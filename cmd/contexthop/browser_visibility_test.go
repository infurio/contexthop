package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/ui"
	"path/filepath"
	"testing"
)

func visibilityConfig() config.Config {
	cfg := config.New()
	cfg.Identities["identity"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example", Identities: []string{"identity"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/contexthop-test-kubeconfig", Context: "cluster", Project: "project"}
	cfg.Docker["docker"] = config.Docker{Context: "docker"}
	cfg.Destinations["workspace"] = config.Destination{Identity: "identity", Project: "project", Kubernetes: "cluster", Docker: "docker"}
	return cfg
}

func TestBrowserHideUnhidePersistsForEveryEntity(t *testing.T) {
	for _, item := range []struct {
		screen ui.Screen
		name   string
	}{
		{ui.ScreenIdentity, "identity"}, {ui.ScreenProject, "project"}, {ui.ScreenKubernetes, "cluster"}, {ui.ScreenDocker, "docker"},
	} {
		t.Run(string(item.screen), func(t *testing.T) {
			cfg := visibilityConfig()
			path := filepath.Join(t.TempDir(), "config.yaml")
			t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
			if err := config.Write(path, cfg); err != nil {
				t.Fatal(err)
			}
			c := newApplicationController(path, cfg, recency.History{}, nil)
			pickers := interactivePickers(cfg, cfg, nil)
			picker := pickers[item.screen]
			picker.Focus = item.name
			pickers[item.screen] = picker
			model := ui.NewAppModel(ui.AppOptions{StartScreen: item.screen, ResourceBrowser: true, Pickers: pickers, Flow: c.Prepare, AcceptResult: c.Accept, BrowserPicker: c.Browser})
			press := func(key string) {
				next, cmd := model.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
				if cmd != nil {
					t.Fatal("visibility unexpectedly left synchronous flow")
				}
				model = next.(ui.AppModel)
			}
			press("h")
			for _, hidden := range []bool{true, false} {
				press("H")
				saved, err := config.Load(path)
				if err != nil {
					t.Fatal(err)
				}
				if catalogRefHidden(saved, catalog.Ref{Kind: catalog.Kind(item.screen), Name: item.name}) != hidden {
					t.Fatalf("hidden flag not persisted: want %t\n%s", hidden, model.View().Content)
				}
				if model.CurrentScreen() != item.screen || model.StackDepth() != 1 {
					t.Fatal("visibility left resource panel")
				}
			}
		})
	}
}

func TestBrowserMappingKeysAndHiddenScope(t *testing.T) {
	cfg := visibilityConfig()
	cfg.Projects["hidden"] = config.Project{Provider: "gcp", ProjectID: "hidden", Identities: []string{"identity"}, Hidden: true}
	cfg.Projects["unrelated"] = config.Project{Provider: "gcp", ProjectID: "unrelated", Hidden: true}
	picker := interactiveBrowserPicker(cfg, cfg, ui.ScreenProject, ui.Draft{ui.ScreenIdentity: "identity"})
	found := false
	for _, option := range picker.Options {
		if option.Name == "unrelated" {
			t.Fatal("hidden resource escaped scope")
		}
		if option.Name == "hidden" {
			found = option.Hidden
		}
	}
	if !found {
		t.Fatal("browser did not receive scoped hidden resource")
	}
	for _, screen := range []ui.Screen{ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker, ui.ScreenWorkspace} {
		picker := interactiveBrowserPicker(cfg, cfg, screen, ui.Draft{})
		mapped := false
		for _, option := range picker.Options {
			if option.Hidden {
				continue
			}
			used := map[string]bool{}
			for _, action := range option.Actions {
				// Menu-only actions have no keyboard binding to collide with.
				if action.Key == "" {
					continue
				}
				if used[action.Key] {
					t.Fatalf("duplicate key %s on %s", action.Key, screen)
				}
				used[action.Key] = true
				if action.Key == "m" {
					mapped = true
				}
				if action.Key == "h" || action.Key == "i" || action.Key == "p" && action.Action != "toggle-pin" || action.Key == "k" || action.Key == "d" || action.Key == "w" {
					t.Fatalf("reserved key %s on %s", action.Key, screen)
				}
			}
		}
		if !mapped && screen != ui.ScreenWorkspace && screen != ui.ScreenIdentity {
			t.Fatalf("%s lacks m mapping action", screen)
		}
	}
}

func TestKubernetesRelationshipEntryChoosesCorrectFlow(t *testing.T) {
	for _, kind := range []string{"standalone", "mapped", "gke"} {
		t.Run(kind, func(t *testing.T) {
			cfg := visibilityConfig()
			target := cfg.Kubernetes["cluster"]
			if kind == "standalone" {
				target.Project = ""
			}
			if kind == "gke" {
				target.Type = "gke"
				target.Cluster = "cluster"
				target.Location = "region"
			}
			cfg.Kubernetes["cluster"] = target
			action, expected := "preferred-identity", screenPreferredIdentity
			if kind == "standalone" {
				action = "kubernetes-set-project"
				expected = screenCatalogKubernetesProject
			}
			transition := interactiveFlow(cfg, cfg, &catalogEditorState{})(ui.Choice{Screen: ui.ScreenKubernetes, Option: ui.Option{Name: "cluster"}, Action: action}, ui.Draft{})
			if transition.Picker.Screen != expected {
				t.Fatalf("opened %s, want %s", transition.Picker.Screen, expected)
			}
			if kind != "standalone" && (len(transition.Picker.Options) < 2 || transition.Picker.Options[0].Label != "Automatic") {
				t.Fatal("preference missing automatic or eligible identities")
			}
		})
	}
}
