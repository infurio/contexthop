package main

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

func TestDiscoveryFiltersExactIdentityAndRetainsSelection(t *testing.T) {
	cfg := config.New()
	for _, name := range []string{"a", "b"} {
		cfg.Identities[name] = config.Identity{Provider: "gcp", Account: "same@example.com", CloudSDKConfig: "/profiles/" + name}
	}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Identities: []string{"a", "b"}}
	cfg.Kubernetes["k"] = config.Kubernetes{Type: "gke", Project: "p", Location: "region", Cluster: "cluster"}
	cfg.Kubernetes["local"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/kube", Context: "local"}
	for _, name := range []string{"a", "b"} {
		catalog.RecordDiscovery(cfg, name, "", []string{"p"}, time.Now(), true)
	}
	catalog.RecordDiscovery(cfg, "a", "p", []string{"region/cluster"}, time.Now(), true)
	catalog.RecordDiscovery(cfg, "b", "p", nil, time.Now(), true)
	for _, option := range interactiveBrowserPicker(cfg, cfg, ui.ScreenKubernetes, ui.Draft{ui.ScreenIdentity: "b", ui.ScreenProject: "p"}).Options {
		if option.Name == "k" {
			t.Fatal("cluster leaked across identity profiles")
		}
	}
	if got := kubernetesOptionsForIdentity(cfg, "b"); len(got) != 1 || got[0].Name != "local" {
		t.Fatalf("standalone not available: %v", got)
	}
	// Choosing a compatible identity after a cluster retains the same target.
	var selected ui.Draft
	model := ui.NewAppModel(ui.AppOptions{CanApplyShell: true, ComposeSelection: true, ResourceBrowser: true, StartScreen: ui.ScreenIdentity, InitialDraft: ui.Draft{ui.ScreenProject: "p", ui.ScreenKubernetes: "k"}, Pickers: interactivePickers(cfg, cfg, nil), BrowserPicker: func(screen ui.Screen, draft ui.Draft) ui.Picker {
		return interactiveBrowserPicker(cfg, cfg, screen, draft)
	}, Flow: func(_ ui.Choice, draft ui.Draft) ui.Transition {
		selected = draft
		return ui.Transition{Complete: true}
	}})
	updated, _ := model.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	model = updated.(ui.AppModel)
	model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if selected[ui.ScreenIdentity] != "a" || selected[ui.ScreenProject] != "p" || selected[ui.ScreenKubernetes] != "k" {
		t.Fatalf("lost staged target: %v", selected)
	}
	if _, err := resolver.Components(cfg, resolver.Selection{Identity: "b", Kubernetes: "k"}); err == nil {
		t.Fatal("activation accepted unseen cluster")
	}
}

func TestAmbiguousLaunchOffersIdentityWithoutLosingTarget(t *testing.T) {
	cfg := config.New()
	for _, name := range []string{"a", "b"} {
		cfg.Identities[name] = config.Identity{Provider: "gcp", Account: name}
	}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Identities: []string{"a", "b"}}
	cfg.Kubernetes["k"] = config.Kubernetes{Type: "gke", Project: "p", Cluster: "cluster", Location: "region"}
	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	draft := ui.Draft{ui.ScreenProject: "p", ui.ScreenKubernetes: "k"}
	transition := flow(ui.Choice{Action: "launch-shell"}, draft)
	if transition.Picker.Screen != screenResolveIdentity || len(transition.Picker.Options) != 2 {
		t.Fatalf("missing identity chooser: %+v", transition)
	}
	transition = flow(ui.Choice{Screen: screenResolveIdentity, Option: ui.Option{Name: "b"}}, draft)
	if !transition.ResetNavigation || transition.DraftUpdates[ui.ScreenIdentity] != "b" || draft[ui.ScreenKubernetes] != "k" {
		t.Fatal("identity choice lost target")
	}
}

func TestRelationshipActionsSeparatePreferenceFromDiscovery(t *testing.T) {
	cfg := config.New()
	cfg.Identities["a"] = config.Identity{Provider: "gcp", Account: "a"}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Identities: []string{"a"}}
	for _, ref := range []catalog.Ref{{Kind: catalog.KindIdentity, Name: "a"}, {Kind: catalog.KindProject, Name: "p"}} {
		for _, action := range catalogRowActions(cfg, ref) {
			if strings.Contains(action.Action, "map-") || strings.Contains(action.Action, "unmap-") {
				t.Fatalf("discovered resource exposes manual mapping: %v", action)
			}
		}
	}
	editor := catalogEditorState{}
	flow := interactiveFlow(cfg, cfg, &editor)
	transition := flow(ui.Choice{Screen: ui.ScreenProject, Option: ui.Option{Name: "p"}, Action: "preferred-identity"}, ui.Draft{})
	if transition.Picker.Screen != screenPreferredIdentity || len(transition.Picker.Options) != 2 {
		t.Fatal("preference should include automatic and existing identity")
	}
	flow(ui.Choice{Screen: screenPreferredIdentity, Option: ui.Option{Name: "a"}}, transition.DraftUpdates)
	if editor.plan == nil || !editor.plan.Valid() || editor.plan.Config.Projects["p"].PreferredIdentity != "a" {
		t.Fatal("preference plan missing")
	}
}

func TestCLIDiscoveryPersistsIdentityScopedRelationships(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	t.Setenv("CONTEXTHOP_CONFIG", path)
	t.Setenv("CONTEXTHOP_CACHE_DIR", dir)
	t.Setenv("PATH", dir)
	cfg := config.New()
	cfg.Identities["a"] = config.Identity{Provider: "gcp", Account: "a@example.com", CloudSDKConfig: filepath.Join(dir, "profile")}
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	script := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte("#!/bin/sh\n"+body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	script("printf '%s' '[{\"projectId\":\"project-id\"}]'\n")
	if err := runCatalogDiscovery([]string{"a"}); err != nil {
		t.Fatal(err)
	}
	script("printf '%s' '[{\"name\":\"cluster\",\"location\":\"region\"}]'\n")
	if err := runCatalogDiscovery([]string{"a", "project-id"}); err != nil {
		t.Fatal(err)
	}
	fresh, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	name := ""
	for key := range fresh.Kubernetes {
		name = key
	}
	if name == "" || !resolver.KubernetesIdentityAvailable(fresh, name, "a") {
		t.Fatal("CLI did not persist accessible cluster")
	}
	script("[ \"$1 $2\" = 'auth print-access-token' ] && exit 0\nprintf '%s' 'network unavailable' >&2\nexit 1\n")
	if err := runCatalogDiscovery([]string{"a", "project-id"}); err == nil {
		t.Fatal("refresh failure hidden")
	}
	fresh, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.DiscoveryFor("a").Clusters["project-id"].Stale || !resolver.KubernetesIdentityAvailable(fresh, name, "a") {
		t.Fatal("failure lost previous evidence or freshness")
	}
	script("printf '%s' '[]'\n")
	if err := runCatalogDiscovery([]string{"a", "project-id"}); err != nil {
		t.Fatal(err)
	}
	fresh, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.KubernetesIdentityAvailable(fresh, name, "a") || len(fresh.Kubernetes) != 1 {
		t.Fatal("empty refresh must remove the relationship, keeping the resource")
	}
}

func TestLegacyLinkOnlyChangesManualProjectAssociation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	t.Setenv("CONTEXTHOP_CONFIG", path)
	cfg := config.New()
	for _, name := range []string{"a", "b"} {
		cfg.Identities[name] = config.Identity{Provider: "gcp", Account: name}
	}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Identities: []string{"a"}}
	cfg.Kubernetes["k"] = config.Kubernetes{Type: "gke", Project: "p", Cluster: "k", Location: "region"}
	cfg.Docker["d"] = config.Docker{Context: "local"}
	cfg.Destinations["k"] = config.Destination{Docker: "d"}
	cfg.Destinations["existing"] = config.Destination{Identity: "a", Project: "p", Kubernetes: "k"}
	catalog.RecordDiscovery(cfg, "b", "", nil, time.Now(), true)
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := runLink("p", "b"); err != nil {
		t.Fatal(err)
	}
	fresh, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh.Destinations, cfg.Destinations) || !resolver.ProjectIdentityAvailable(fresh, "p", "b") {
		t.Fatal("manual fallback changed workspaces or failed to associate identity")
	}
}

func TestFreshnessCheckRejectsChangedRelationshipScope(t *testing.T) {
	cfg := config.New()
	cfg.Identities["a"] = config.Identity{Provider: "gcp", Account: "a"}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Identities: []string{"a"}}
	catalog.RecordDiscovery(cfg, "a", "", []string{"p"}, time.Now(), true)
	fresh := cfg.Clone()
	catalog.RecordDiscovery(fresh, "a", "", nil, time.Now(), true)
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("CONTEXTHOP_CONFIG", path)
	if err := config.Write(path, fresh); err != nil {
		t.Fatal(err)
	}
	if err := validateSelectionFresh(cfg, resolver.Selection{Identity: "a", Project: "p"}); err == nil {
		t.Fatal("accepted externally removed discovery relationship")
	}
}

func TestDiscoveryShortcutOpensSharedDialog(t *testing.T) {
	cfg := config.New()
	cfg.Identities["a"] = config.Identity{Provider: "gcp", Account: "a"}
	action := ""
	model := ui.NewAppModel(ui.AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ui.ScreenIdentity, Pickers: interactivePickers(cfg, cfg, nil), Flow: func(choice ui.Choice, _ ui.Draft) ui.Transition { action = choice.Action; return ui.Transition{} }})
	model.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if action != "discover-resources" {
		t.Fatalf("discovery shortcut intercepted: %q", action)
	}
}

func TestProjectSelectionAsksForIdentity(t *testing.T) {
	for _, enter := range []bool{false, true} {
		for _, staged := range []string{"", "a"} {
			t.Run(fmt.Sprintf("enter=%t/staged=%s", enter, staged), func(t *testing.T) {
				cfg := config.New()
				for _, name := range []string{"a", "b", "unrelated"} {
					cfg.Identities[name] = config.Identity{Provider: "gcp", Account: name + "@example.com"}
				}
				cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "cloud-project", Identities: []string{"a", "b"}}
				var selected ui.Draft
				model := ui.NewAppModel(ui.AppOptions{CanApplyShell: true,
					ComposeSelection: true, ResourceBrowser: true, StartScreen: ui.ScreenProject,
					InitialDraft: ui.Draft{ui.ScreenIdentity: staged, ui.ScreenDocker: "docker"},
					Pickers:      interactivePickers(cfg, cfg, nil),
					BrowserPicker: func(screen ui.Screen, draft ui.Draft) ui.Picker {
						return interactiveBrowserPicker(cfg, cfg, screen, draft)
					},
					Flow: func(choice ui.Choice, draft ui.Draft) ui.Transition {
						if choice.Action == "discover-resources" {
							return ui.Transition{Picker: ui.Picker{Screen: "discovery-required", Title: "Discover access", DisableEnter: true}}
						}
						selected = draft
						return ui.Transition{Complete: true}
					},
				})
				key := tea.KeyPressMsg{Code: ' ', Text: " "}
				if enter {
					key = tea.KeyPressMsg{Code: tea.KeyEnter}
				}
				updated, _ := model.Update(key)
				model = updated.(ui.AppModel)
				if staged != "" {
					want := ui.ScreenProject

					if model.CurrentScreen() != want || model.StackDepth() != 1 {
						t.Fatal("eligible explicit identity prompted again")
					}
					model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					if selected[ui.ScreenIdentity] != staged || selected[ui.ScreenProject] != "p" {
						t.Fatal("explicit identity lost", selected)
					}
					return
				}
				view := model.View().Content
				if model.StackDepth() != 2 || !strings.Contains(view, "Choose identity") || !strings.Contains(view, "b@example.com") || strings.Contains(view, "unrelated@example.com") {
					t.Fatalf("missing eligible identity dialog: %s", view)
				}
				updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
				model = updated.(ui.AppModel)
				if model.CurrentScreen() != ui.ScreenProject || model.StackDepth() != 1 {
					t.Fatal("cancel did not restore projects")
				}
				updated, _ = model.Update(key)
				model = updated.(ui.AppModel)
				if model.StackDepth() != 2 {
					t.Fatal("cancel prematurely staged project")
				}
				updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
				model = updated.(ui.AppModel)
				updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				model = updated.(ui.AppModel)
				want := ui.ScreenProject

				if model.CurrentScreen() != want || model.StackDepth() != 1 {
					t.Fatalf("choice returned to %s", model.CurrentScreen())
				}
				model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				if selected[ui.ScreenIdentity] != "b" || selected[ui.ScreenProject] != "p" || selected[ui.ScreenDocker] != "docker" {
					t.Fatalf("incorrect selection: %v", selected)
				}
			})
		}
	}
}

func TestProjectWithOneIdentitySelectsItWithoutDialog(t *testing.T) {
	cfg := config.New()
	cfg.Identities["a"] = config.Identity{Provider: "gcp", Account: "a@example.com"}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Identities: []string{"a"}}
	for _, option := range sessionProjectOptions(cfg, "") {
		if option.Name == "p" {
			if len(option.IdentityChoices) != 0 || option.Selection[ui.ScreenIdentity] != "a" {
				t.Fatalf("incorrect sole identity selection: %#v", option)
			}
			return
		}
	}
	t.Fatal("project missing")
}

func TestKubernetesSelectionIdentityPrompt(t *testing.T) {
	for _, tc := range []struct {
		name, kind, project, staged string
		eligible                    int
		prompt                      bool
	}{
		{"cloud-multiple", "gke", "p", "", 2, true},
		{"cloud-single", "gke", "p", "", 1, false},
		{"cloud-none", "gke", "p", "", 0, true},
		{"cloud-staged", "gke", "p", "a", 2, false},
		{"cloud-kubeconfig", "kubeconfig", "p", "", 2, true},
		{"k3s", "kubeconfig", "", "", 0, false},
		{"docker-desktop", "kubeconfig", "", "", 0, false},
	} {
		for _, enter := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enter=%t", tc.name, enter), func(t *testing.T) {
				cfg := config.New()
				cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Identities: []string{"a", "b"}}
				cfg.Kubernetes["k"] = config.Kubernetes{Type: tc.kind, Project: tc.project, Cluster: "cluster", Location: "region", Context: tc.name}
				for index, name := range []string{"a", "b"} {
					cfg.Identities[name] = config.Identity{Provider: "gcp", Account: name + "@example.com"}
					catalog.RecordDiscovery(cfg, name, "", []string{"p"}, time.Now(), true)
					clusters := []string{}
					if index < tc.eligible {
						clusters = []string{"region/cluster"}
					}
					catalog.RecordDiscovery(cfg, name, "p", clusters, time.Now(), true)
				}
				var selected ui.Draft
				model := ui.NewAppModel(ui.AppOptions{CanApplyShell: true,
					ComposeSelection: true, ResourceBrowser: true, StartScreen: ui.ScreenKubernetes,
					InitialDraft: ui.Draft{ui.ScreenIdentity: tc.staged, ui.ScreenDocker: "docker"},
					Pickers:      interactivePickers(cfg, cfg, nil),
					BrowserPicker: func(screen ui.Screen, draft ui.Draft) ui.Picker {
						return interactiveBrowserPicker(cfg, cfg, screen, draft)
					},
					Flow: func(choice ui.Choice, draft ui.Draft) ui.Transition {
						if choice.Action == "discover-resources" {
							return ui.Transition{Picker: ui.Picker{Screen: "discovery-required", Title: "Discover access", DisableEnter: true}}
						}
						selected = draft
						return ui.Transition{Complete: true}
					},
				})
				key := tea.KeyPressMsg{Code: ' ', Text: " "}
				if enter {
					key = tea.KeyPressMsg{Code: tea.KeyEnter}
				}
				updated, _ := model.Update(key)
				model = updated.(ui.AppModel)
				if tc.prompt {
					if model.StackDepth() != 2 || !strings.Contains(model.View().Content, "Choose identity") && !strings.Contains(model.View().Content, "Choose an identity") {
						t.Fatalf("missing chooser: %s", model.View().Content)
					}
					if tc.eligible < 2 && strings.Contains(model.View().Content, "b@example.com") {
						t.Fatal("offered identity without cluster access")
					}
					if tc.eligible == 0 {
						if !strings.Contains(model.View().Content, "No eligible identities") {
							t.Fatal("missing recovery explanation")
						}
						updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
						if updated.(ui.AppModel).StackDepth() != 2 {
							t.Fatal("selected target without eligible identity")
						}
						return
					}
					updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
					model = updated.(ui.AppModel)
					updated, _ = model.Update(key)
					model = updated.(ui.AppModel)
					if model.StackDepth() != 2 {
						t.Fatal("cancel prematurely selected target")
					}
					updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					model = updated.(ui.AppModel)
				}
				if model.StackDepth() != 1 || model.CurrentScreen() != ui.ScreenKubernetes {
					t.Fatal("did not return to Kubernetes")
				}
				model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				wantIdentity := tc.staged
				if tc.prompt || tc.eligible == 1 {
					wantIdentity = "a"
				}
				if selected[ui.ScreenIdentity] != wantIdentity || selected[ui.ScreenProject] != tc.project || selected[ui.ScreenKubernetes] != "k" || selected[ui.ScreenDocker] != "docker" {
					t.Fatalf("wrong selected components: %v", selected)
				}
			})
		}
	}
}
