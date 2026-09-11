package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/recency"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func discoveryDialogFixture() config.Config {
	cfg := config.New()
	for _, name := range []string{"a", "b"} {
		cfg.Identities[name] = config.Identity{Provider: "gcp", Account: name + "@example.com"}
	}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "project-id", Identities: []string{"a", "b"}}
	cfg.Kubernetes["k"] = config.Kubernetes{Type: "gke", Project: "p", Cluster: "cluster", Location: "region"}
	cfg.Kubernetes["local"] = config.Kubernetes{Type: "kubeconfig", Context: "k3s"}
	return cfg
}

func TestDiscoveryDialogDefaultsAndSelectionIsolation(t *testing.T) {
	cfg := discoveryDialogFixture()
	for _, tc := range []struct {
		screen                         ui.Screen
		name, scope, project, identity string
	}{
		{ui.ScreenIdentity, "b", "projects", "", "b"},
		{ui.ScreenProject, "p", "clusters", "p", "a"},
		{ui.ScreenKubernetes, "k", "clusters", "p", "a"},
		{ui.ScreenKubernetes, "local", "projects", "", "a"},
		{ui.ScreenDocker, "", "projects", "", "a"},
		{ui.ScreenWorkspace, "", "projects", "", "a"},
	} {
		t.Run(string(tc.screen)+tc.name, func(t *testing.T) {
			draft := ui.Draft{ui.ScreenIdentity: "a", ui.ScreenProject: "p", ui.ScreenKubernetes: "k", ui.ScreenDocker: "docker"}
			before := cloneApplicationDraft(draft)
			transition, handled := discoveryDialogFlow(cfg, cfg, ui.Choice{Screen: tc.screen, Option: ui.Option{Name: tc.name}, Action: "discover-resources"}, draft)
			if !handled || transition.PersistDiscovery || transition.Picker.Screen != screenDiscover {
				t.Fatal("discovery did not open local dialog")
			}
			if transition.DraftUpdates[discoverScope] != tc.scope || transition.DraftUpdates[discoverProject] != tc.project || transition.DraftUpdates[discoverIdentity] != tc.identity {
				t.Fatalf("wrong defaults: %v", transition.DraftUpdates)
			}
			for key, value := range before {
				if draft[key] != value || transition.DraftUpdates[key] != "" {
					t.Fatal("opening discovery changed staged selection")
				}
			}
			cancelled, _ := discoveryDialogFlow(cfg, cfg, ui.Choice{Screen: screenDiscover, Option: ui.Option{Name: "cancel"}}, draft)
			if !cancelled.Dismiss {
				t.Fatal("cancel lost originating tab")
			}
		})
	}
}

func TestDiscoveryDialogRestrictsProjectIdentitiesAndValidatesStart(t *testing.T) {
	cfg := discoveryDialogFixture()
	cfg.Identities["unrelated"] = config.Identity{Provider: "gcp", Account: "unrelated"}
	draft := ui.Draft{discoverScope: "clusters", discoverProject: "p", discoverIdentity: "unrelated"}
	normalizeDiscoveryIdentity(cfg, draft)
	if draft[discoverIdentity] != "" {
		t.Fatal("kept unrelated identity")
	}
	picker, _ := discoveryDialogFlow(cfg, cfg, ui.Choice{Screen: screenDiscover, Option: ui.Option{Name: "identity"}}, draft)
	if len(picker.Picker.Options) != 2 {
		t.Fatal("identity chooser not restricted")
	}
	transition, _ := discoveryDialogFlow(cfg, cfg, ui.Choice{Screen: screenDiscover, Option: ui.Option{Name: "start"}}, draft)
	if transition.Picker.Screen != screenDiscover || !strings.Contains(transition.Picker.Description, "Choose an identity before") {
		t.Fatal("invalid start was not blocked")
	}
	draft[discoverIdentity] = "a"
	scope, _ := discoveryDialogFlow(cfg, cfg, ui.Choice{Screen: screenDiscoverScope, Option: ui.Option{Name: "all"}}, draft)
	if scope.DraftUpdates[discoverScope] != "all" || !strings.Contains(scope.Picker.Description, "every project") {
		t.Fatal("full scan not explicit")
	}
}

func TestDiscoveryAndImportShortcutsOpenDirectDialogs(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("KUBECONFIG", t.TempDir()+"/missing")
	cfg := discoveryDialogFixture()
	local := cfg.Kubernetes["local"]
	local.Kubeconfig = t.TempDir() + "/kubeconfig"
	cfg.Kubernetes["local"] = local
	for _, screen := range []ui.Screen{ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker, ui.ScreenWorkspace} {
		model := ui.NewAppModel(ui.AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: screen, Pickers: interactivePickers(cfg, cfg, nil), Flow: interactiveFlow(cfg, cfg, &catalogEditorState{})})
		key := 'd'
		if screen == ui.ScreenDocker {
			key = 'l'
		}
		updated, _ := model.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		model = updated.(ui.AppModel)
		want := screenDiscover
		if screen == ui.ScreenDocker {
			want = "local-import-results"
		}
		if model.CurrentScreen() != want || model.StackDepth() != 2 {
			t.Fatalf("discovery/import failed on %s: screen=%s depth=%d view=%s", screen, model.CurrentScreen(), model.StackDepth(), model.View().Content)
		}
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if updated.(ui.AppModel).CurrentScreen() != screen {
			t.Fatalf("cancel failed on %s", screen)
		}
	}
}

type scopedDiscoveryFake struct {
	mu          sync.Mutex
	projects    int
	calls       []string
	active, max int
	started     chan struct{}
	release     chan struct{}
}

func (f *scopedDiscoveryFake) RefreshProjects(_ context.Context, identity config.Identity) (catalog.ProjectResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "projects:"+identity.Account)
	result := catalog.ProjectResult{Freshness: catalog.FreshnessLive, LastSuccess: time.Now()}
	for i := range f.projects {
		result.Projects = append(result.Projects, catalog.Project{ProjectID: fmt.Sprintf("p%d", i)})
	}
	return result, nil
}
func (f *scopedDiscoveryFake) RefreshClusters(_ context.Context, identity config.Identity, project string) (catalog.ClusterResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, project+":"+identity.Account)
	f.active++
	f.max = max(f.max, f.active)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- struct{}{}
		<-f.release
	}
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	if project == "p1" {
		return catalog.ClusterResult{}, errors.New("permission denied")
	}
	return catalog.ClusterResult{Freshness: catalog.FreshnessLive, LastSuccess: time.Now(), Clusters: []catalog.Cluster{{Name: "cluster", Location: "region"}}}, nil
}

func TestScopedDiscoveryLimitsScopeAndIdentity(t *testing.T) {
	for _, scope := range []string{"projects", "clusters", "all"} {
		t.Run(scope, func(t *testing.T) {
			cfg := discoveryDialogFixture()
			cfg.Projects["p1"] = config.Project{Provider: "gcp", ProjectID: "p1", Identities: []string{"a"}}
			catalog.RecordDiscovery(cfg, "a", "p1", []string{"old/cluster"}, time.Now(), true)
			saved := cfg.Clone()
			fake := &scopedDiscoveryFake{projects: 4}
			draft := ui.Draft{discoverIdentity: "a", discoverProject: "p", discoverScope: scope, discoverOrigin: string(ui.ScreenProject), ui.ScreenIdentity: "b"}
			messages := []string{}
			result := runScopedDiscovery(cfg, saved, draft, func(s string) { messages = append(messages, s) }, fake)
			wantCalls := 1
			if scope == "all" {
				wantCalls = 5
			}
			if len(fake.calls) != wantCalls {
				t.Fatalf("unexpected cloud calls: %v", fake.calls)
			}
			for _, call := range fake.calls {
				if !strings.HasSuffix(call, ":a@example.com") {
					t.Fatal("crossed identity boundary")
				}
			}
			if scope == "clusters" && fake.calls[0] != "project-id:a@example.com" {
				t.Fatal("scanned unrelated project")
			}
			if len(messages) == 0 || !strings.Contains(result.Picker.Description, "a@example.com [a]") || !result.PersistDiscovery || hasResourceUpdates(result.DraftUpdates) {
				t.Fatal("missing progress, identity, or changed staged selection")
			}
			if scope == "all" {
				evidence := cfg.DiscoveryFor("a").Clusters["p1"]
				if !evidence.Stale || !reflect.DeepEqual(evidence.Resources, []string{"old/cluster"}) {
					t.Fatal("failure erased cached membership")
				}
				if !strings.Contains(result.Picker.OperationDetails, "p1: permission denied") {
					t.Fatal("missing per-project failure")
				}
			}
		})
	}
}

func TestFullDiscoveryUsesThreeWorkers(t *testing.T) {
	cfg := discoveryDialogFixture()
	fake := &scopedDiscoveryFake{projects: 6, started: make(chan struct{}, 6), release: make(chan struct{})}
	done := make(chan ui.Transition, 1)
	go func() {
		done <- runScopedDiscovery(cfg, cfg.Clone(), ui.Draft{discoverIdentity: "a", discoverScope: "all", discoverOrigin: string(ui.ScreenProject)}, nil, fake)
	}()
	for range 3 {
		select {
		case <-fake.started:
		case <-time.After(5 * time.Second):
			close(fake.release)
			t.Fatal("cluster requests did not run concurrently")
		}
	}
	close(fake.release)
	<-done
	if fake.max != 3 {
		t.Fatalf("concurrency=%d", fake.max)
	}
}

func TestMissingProjectKeepsIdentityAndGuidesProjectFirst(t *testing.T) {
	cfg := discoveryDialogFixture()
	draft := ui.Draft{discoverIdentity: "a", discoverScope: "projects"}
	changed, _ := discoveryDialogFlow(cfg, cfg, ui.Choice{Screen: screenDiscoverScope, Option: ui.Option{Name: "clusters"}}, draft)
	if changed.DraftUpdates[discoverIdentity] != "a" || changed.Picker.Focus != "project" {
		t.Fatal("scope change lost identity or focused wrong field")
	}
	if !strings.Contains(discoveryValidation(cfg, draft), "Choose a project first") {
		t.Fatal("wrong validation guidance")
	}
	choices, _ := discoveryDialogFlow(cfg, cfg, ui.Choice{Screen: screenDiscover, Option: ui.Option{Name: "identity"}}, draft)
	if len(choices.Picker.Options) == 0 {
		t.Fatal("identity chooser is empty before project selection")
	}
}

type failedDiscoveryFake struct{}

func (failedDiscoveryFake) RefreshProjects(context.Context, config.Identity) (catalog.ProjectResult, error) {
	return catalog.ProjectResult{Projects: []catalog.Project{{ProjectID: "cached-project"}}, Freshness: catalog.FreshnessStale, LastSuccess: time.Now().Add(-48 * time.Hour)}, errors.New("Reauthentication failed")
}
func (failedDiscoveryFake) RefreshClusters(context.Context, config.Identity, string) (catalog.ClusterResult, error) {
	return catalog.ClusterResult{}, errors.New("Reauthentication failed")
}

func TestDiscoveryFailuresReturnToBrowserWithDetails(t *testing.T) {
	cfg := discoveryDialogFixture()
	identity := cfg.Identities["a"]
	identity.CloudSDKConfig = t.TempDir()
	cfg.Identities["a"] = identity
	for _, scope := range []string{"clusters", "all"} {
		cloudauth.RecordStatus(identity, "Authenticated")
		draft := ui.Draft{discoverScope: scope, discoverIdentity: "a", discoverProject: "p", discoverOrigin: string(ui.ScreenProject), ui.ScreenIdentity: "b"}
		result := runScopedDiscovery(cfg, cfg.Clone(), draft, nil, failedDiscoveryFake{})
		if cloudauth.Status(identity) != "Sign-in required" || !result.Picker.ResourceBrowser || !strings.Contains(result.Picker.Description, "press o for results") {
			t.Fatal("failure did not return to browser with recovery details")
		}
		if scope == "all" && (!strings.Contains(result.Picker.OperationDetails, "0 fresh · 1 cached") || !strings.Contains(result.Picker.OperationDetails, "Cluster scan not started")) {
			t.Fatal("cached results or failed scope details lost")
		}
		if hasResourceUpdates(result.DraftUpdates) || draft[ui.ScreenIdentity] != "b" {
			t.Fatal("discovery changed shell selection")
		}
	}
}

func TestDiscoveryCompletesWithoutResultDialog(t *testing.T) {
	for _, origin := range []ui.Screen{ui.ScreenWorkspace, ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker} {
		cfg := discoveryDialogFixture()
		draft := ui.Draft{discoverScope: "projects", discoverIdentity: "a", discoverOrigin: string(origin)}
		result := runScopedDiscovery(cfg, cfg.Clone(), draft, nil, &scopedDiscoveryFake{projects: 1})
		if result.Picker.Screen != origin || !result.Picker.ResourceBrowser || !result.ReturnToPrevious || !result.PreservePosition || len(result.Pickers) != 0 {
			t.Fatal("discovery did not return directly to original table")
		}
		if !strings.Contains(result.Picker.Description, "Discovered 1 project as") {
			t.Fatalf("missing concise result: %s", result.Picker.Description)
		}
		if _, ok := cfg.Projects["p0"]; !ok {
			t.Fatal("discovery results lost")
		}
		if hasResourceUpdates(result.DraftUpdates) {
			t.Fatal("discovery changed selection")
		}
	}
}

type cancellableDiscoveryFake struct{ scopedDiscoveryFake }

func (f *cancellableDiscoveryFake) RefreshClusters(ctx context.Context, _ config.Identity, project string) (catalog.ClusterResult, error) {
	if project == "p0" {
		return catalog.ClusterResult{Freshness: catalog.FreshnessLive, LastSuccess: time.Now(), Clusters: []catalog.Cluster{{Name: "completed", Location: "region"}}}, nil
	}
	<-ctx.Done()
	return catalog.ClusterResult{}, ctx.Err()
}
func TestStoppingDiscoverySavesCompletedResults(t *testing.T) {
	cfg := discoveryDialogFixture()
	delete(cfg.Kubernetes, "local")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	c := newApplicationController(path, cfg, recency.History{}, nil)
	c.autoSaveDiscovery = true
	resources := c.resources.Clone()
	view := resources.Selection()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	draft := ui.Draft{discoverScope: "all", discoverIdentity: "a", discoverOrigin: string(ui.ScreenProject)}
	result := runScopedDiscovery(view, cfg, draft, func(label string) {
		if strings.Contains(label, "1/3 projects checked") {
			cancel()
		}
	}, &cancellableDiscoveryFake{scopedDiscoveryFake{projects: 3}}, ctx)
	if !strings.Contains(result.Picker.Description, "Discovery stopped") || !strings.Contains(result.Picker.OperationDetails, "2 not scanned") || !strings.Contains(result.Picker.OperationDetails, "Failures: 0") {
		t.Fatalf("wrong stopped result: %s", result.Picker.Description)
	}
	accepted := c.finish(resources, view, &catalogEditorState{}, result, ui.Choice{}, draft)
	if accepted.Result == nil {
		t.Fatal("completed results were not accepted")
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range saved.Kubernetes {
		found = found || target.Cluster == "completed"
	}
	if !found {
		t.Fatal("stopping lost completed cluster")
	}
}

func TestDiscoveryWindowRendering(t *testing.T) {
	cfg := discoveryDialogFixture()
	for _, scope := range []string{"projects", "clusters", "all"} {
		draft := ui.Draft{discoverScope: scope, discoverProject: "p", discoverIdentity: "a"}
		picker := discoveryDialog(cfg, draft, "")
		model := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenIdentity, ResourceBrowser: true, Pickers: interactivePickers(cfg, cfg, nil), Flow: func(ui.Choice, ui.Draft) ui.Transition { return ui.Transition{Picker: picker} }})
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		model = updated.(ui.AppModel)
		updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
		model = updated.(ui.AppModel)
		view := ansi.Strip(model.View().Content)
		if strings.Contains(view, "ctrl+c") || !strings.Contains(view, "then start discovery.") {
			t.Fatalf("bad dialog: %s", view)
		}
		if scope == "all" && !strings.Contains(view, "considerably longer.") {
			t.Fatal("full scan explanation clipped")
		}
		if scope == "projects" {
			t.Log("\n" + view)
		}
	}
}

func hasResourceUpdates(draft ui.Draft) bool {
	for _, key := range []ui.Screen{ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker, ui.ScreenWorkspace, ui.ScreenWorkspaceSource} {
		if _, ok := draft[key]; ok {
			return true
		}
	}
	return false
}
