package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/ui"
)

func TestEntityBrowsersIgnoreLegacyPinsAndRecency(t *testing.T) {
	cfg := config.New()
	for _, name := range []string{"zebra", "alpha"} {
		pinned := name == "zebra"
		cfg.Identities[name] = config.Identity{Provider: "gcp", Account: name + "@example.com", Pinned: pinned}
		cfg.Projects[name] = config.Project{Provider: "gcp", ProjectID: name, Identities: []string{name}, Pinned: pinned}
		cfg.Kubernetes[name] = config.Kubernetes{Type: "gke", Project: name, Cluster: name, Location: "us-east1", Pinned: pinned}
		cfg.Docker[name] = config.Docker{Context: name, Pinned: pinned}
		cfg.Destinations[name] = config.Destination{Docker: name, Pinned: pinned}
	}
	history := recency.History{LastUsed: map[string]map[string]time.Time{}}
	for _, kind := range []string{"workspace", "identity", "project", "kubernetes", "docker"} {
		history.LastUsed[kind] = map[string]time.Time{"zebra": time.Now()}
	}
	c := newApplicationController("", cfg, history, nil)
	if len(c.Pickers()) != 7 {
		t.Fatalf("unexpected application pickers: %v", c.Pickers())
	} // Five tabs, reuse and new-resource dialog.
	for _, screen := range []ui.Screen{ui.ScreenWorkspace, ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker} {
		picker := c.Browser(screen, ui.Draft{})
		names := []string{}
		for _, row := range picker.Options {
			if strings.HasPrefix(row.Name, "\x00__") {
				continue
			}
			names = append(names, row.Name)
			for _, action := range row.Actions {
				if action.Action == "toggle-pin" || action.Key == "p" || action.Key == "ctrl+p" {
					t.Fatalf("%s still exposes pin: %v", screen, action)
				}
			}
		}
		if strings.Join(names, ",") != "alpha,zebra" || picker.Focus != "" {
			t.Fatalf("%s ordering/focus: %v %q", screen, names, picker.Focus)
		}
	}
}

func TestSaveNextShellIncludesUnstagedHighlightAndContinuesIdentityChoice(t *testing.T) {
	for _, searching := range []bool{false, true} {
		t.Run(map[bool]string{false: "browse", true: "search"}[searching], func(t *testing.T) {
			cfg := previewTestConfig()
			cfg.Destinations = map[string]config.Destination{}
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := config.Write(path, cfg); err != nil {
				t.Fatal(err)
			}
			c := newApplicationController(path, cfg, recency.History{}, nil)
			m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenKubernetes, ResourceBrowser: true, ComposeSelection: true, InitialDraft: ui.Draft{ui.ScreenDocker: "local"}, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept, LaunchPreview: c.LaunchPreview})
			press := func(msg tea.Msg) {
				next, cmd := m.Update(msg)
				m = next.(ui.AppModel)
				if cmd != nil {
					t.Fatal("save attempted activation")
				}
			}
			if searching {
				press(tea.KeyPressMsg{Code: '/', Text: "/"})
				for _, r := range "dev-cluster" {
					press(tea.KeyPressMsg{Code: r, Text: string(r)})
				}
			}
			press(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
			if m.CurrentScreen() != "next-shell-identity" {
				t.Fatal("save skipped identity choice", m.CurrentScreen())
			}
			press(tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.CurrentScreen() != screenSaveWorkspaceName {
				t.Fatal("save did not continue", m.CurrentScreen())
			}
			press(tea.PasteMsg{Content: "Daily dev"})
			press(tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.CurrentScreen() != screenAddWorkspaceBuilder {
				t.Fatal("missing review", m.CurrentScreen())
			}
			value := c.editor.workspace.Value
			if value.Identity != "alice" || value.Project != "dev" || value.Kubernetes != "dev-cluster" || value.Docker != "local" {
				t.Fatalf("saved preview lost components: %+v", value)
			}
			// The editor focuses Create workspace; saving still requires its normal review.
			press(tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.CurrentScreen() != ui.ScreenConfirm {
				t.Fatal("missing save confirmation", m.CurrentScreen())
			}
			press(tea.KeyPressMsg{Code: tea.KeyEnter})
			saved, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			workspace, ok := saved.Destinations["Daily dev"]
			if !ok || workspace.Docker != "local" || workspace.Kubernetes != "dev-cluster" {
				t.Fatalf("workspace not saved: %v", saved.Destinations)
			}
		})
	}
}

func TestSaveNextShellCarriesADCSettingIntoReview(t *testing.T) {
	for _, override := range []string{"identity", "off"} {
		cfg := previewTestConfig()
		editor := &catalogEditorState{}
		_, ok := selectionWorkspaceFlow(cfg, cfg, ui.Choice{Action: "save-selection-workspace"}, ui.Draft{ui.ScreenWorkspace: "saved", ui.ScreenWorkspaceSource: "saved", ui.ScreenIdentity: "alice", ui.ScreenProject: "dev", ui.ScreenKubernetes: "dev-cluster", ui.ScreenShellADCOverride: override}, editor)
		want, _ := shellADCMode(override)
		if !ok || editor.workspace == nil || editor.workspace.Value.ADC != want {
			t.Fatalf("ADC %s not captured: %+v", override, editor.workspace)
		}
	}
}
