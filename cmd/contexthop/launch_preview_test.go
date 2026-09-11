package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
	"reflect"
	"strings"
	"testing"
)

func previewTestConfig() config.Config {
	cfg := config.New()
	cfg.Identities["alice"] = config.Identity{Provider: "gcp", Account: "alice@example.com"}
	cfg.Identities["bob"] = config.Identity{Provider: "gcp", Account: "bob@example.com"}
	cfg.Projects["dev"] = config.Project{Provider: "gcp", ProjectID: "dev-project", Identities: []string{"alice", "bob"}}
	cfg.Projects["prod"] = config.Project{Provider: "gcp", ProjectID: "prod-project", Identities: []string{"bob"}}
	cfg.Kubernetes["dev-cluster"] = config.Kubernetes{Type: "gke", Project: "dev", Cluster: "dev-cluster", Location: "us-east1", Namespace: "payments"}
	cfg.Kubernetes["prod-cluster"] = config.Kubernetes{Type: "gke", Project: "prod", Cluster: "prod-cluster", Location: "us-east1"}
	cfg.Docker["local"] = config.Docker{Context: "desktop"}
	cfg.Docker["remote"] = config.Docker{Context: "remote"}
	cfg.Destinations["saved"] = config.Destination{Identity: "alice", Project: "dev", Kubernetes: "dev-cluster", ADC: "identity"}
	return cfg
}

func TestNextShellPreviewComposition(t *testing.T) {
	cfg := previewTestConfig()
	staged := ui.Draft{ui.ScreenIdentity: "alice", ui.ScreenProject: "dev", ui.ScreenKubernetes: "dev-cluster", ui.ScreenDocker: "local"}
	for _, tc := range []struct {
		screen                                              ui.Screen
		name, identity, project, cluster, docker, workspace string
	}{
		{ui.ScreenProject, "prod", "bob", "prod", "", "local", ""},
		{ui.ScreenKubernetes, "prod-cluster", "bob", "prod", "prod-cluster", "local", ""},
		{ui.ScreenDocker, "remote", "alice", "dev", "dev-cluster", "remote", ""},
		{ui.ScreenWorkspace, "saved", "alice", "dev", "dev-cluster", "", "saved"},
		{ui.ScreenIdentity, "bob", "bob", "dev", "dev-cluster", "local", ""},
	} {
		t.Run(string(tc.screen)+tc.name, func(t *testing.T) {
			before := cloneApplicationDraft(staged)
			p := nextShellPreview(cfg, tc.screen, ui.Option{Name: tc.name}, staged)
			if p.Error != "" || p.NeedsIdentity {
				t.Fatal(p)
			}
			want := ui.Draft{ui.ScreenIdentity: tc.identity, ui.ScreenProject: tc.project, ui.ScreenKubernetes: tc.cluster, ui.ScreenDocker: tc.docker, ui.ScreenWorkspace: tc.workspace}
			for k, v := range want {
				if p.Draft[k] != v {
					t.Fatalf("%s=%q, want %q", k, p.Draft[k], v)
				}
			}
			if !reflect.DeepEqual(staged, before) {
				t.Fatal("preview mutated staging")
			}
			if tc.workspace != "" && p.Fields[len(p.Fields)-1].Value != "identity" {
				t.Fatal("workspace ADC lost")
			}
		})
	}
}

func TestSharedLaunchKeysPreviewAndIdentityContinuation(t *testing.T) {
	cfg := previewTestConfig()
	for _, screen := range []ui.Screen{ui.ScreenWorkspace, ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker} {
		for _, searching := range []bool{false, true} {
			for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: tea.KeyEnter, Mod: tea.ModShift}, {Code: 'a', Mod: tea.ModCtrl}} {
				name := map[ui.Screen]string{ui.ScreenWorkspace: "saved", ui.ScreenIdentity: "alice", ui.ScreenProject: "dev", ui.ScreenKubernetes: "dev-cluster", ui.ScreenDocker: "local"}[screen]
				var request ui.Draft
				var action string
				calls := 0
				picker := ui.Picker{Screen: screen, ResourceBrowser: true, ModalActions: true, Options: []ui.Option{{Name: name}}}
				m := ui.NewAppModel(ui.AppOptions{StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, CanApplyShell: true, Pickers: map[ui.Screen]ui.Picker{screen: picker}, LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview { return nextShellPreview(cfg, s, o, d) }, Flow: func(c ui.Choice, d ui.Draft) ui.Transition {
					calls++
					request = d
					action = c.Action
					return ui.Transition{Complete: true}
				}})
				update := func(msg tea.Msg) tea.Cmd { updated, cmd := m.Update(msg); m = updated.(ui.AppModel); return cmd }
				update(tea.WindowSizeMsg{Width: 200, Height: 24})
				preview := nextShellPreview(cfg, screen, ui.Option{Name: name}, ui.Draft{})
				view := ansi.Strip(m.View().Content)
				if !strings.Contains(view, "Selected") {
					t.Fatal(view)
				}
				for _, field := range preview.Fields {
					if field.Label == "ADC" {
						if strings.Contains(view, "[ADC]") != (field.Value == "identity") {
							t.Fatal("wrong ADC badge", view)
						}
						continue
					}
					value := field.Value
					if value == "none" || value == "" {
						value = "—"
					}
					if !strings.Contains(view, value) {
						t.Fatal("preview field missing", field, view)
					}
				}
				if searching {
					update(tea.KeyPressMsg{Code: '/', Text: "/"})
				}
				cmd := update(key)
				if preview.NeedsIdentity {
					if calls != 0 || cmd != nil {
						t.Fatal("launched before account choice")
					}
					cmd = update(tea.KeyPressMsg{Code: tea.KeyEnter})
					preview.Draft[ui.ScreenIdentity] = "alice"
				}
				wantAction := "default-shell"
				if key.Mod&(tea.ModShift|tea.ModCtrl) != 0 {
					wantAction = "apply-shell"
				}
				if key.Mod&tea.ModAlt != 0 {
					wantAction = "launch-shell"
				}
				if calls != 1 || cmd == nil || action != wantAction {
					t.Fatal("wrong continuation", screen, key, calls, action)
				}
				for _, field := range []ui.Screen{ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker, ui.ScreenWorkspace} {
					if request[field] != preview.Draft[field] {
						t.Fatal("launched draft differs from preview", screen, field, request, preview.Draft)
					}
				}
			}
		}
	}
}

func TestSpaceStagesProjectWithoutLaunchingAndCancelPreservesChoices(t *testing.T) {
	cfg := previewTestConfig()
	initial := ui.Draft{ui.ScreenDocker: "local", ui.ScreenShellADCOverride: "off"}
	var observed ui.Draft
	preview := func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview {
		observed = cloneApplicationDraft(d)
		return nextShellPreview(cfg, s, o, d)
	}
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenProject, ResourceBrowser: true, ComposeSelection: true, InitialDraft: initial, Pickers: map[ui.Screen]ui.Picker{ui.ScreenProject: {Screen: ui.ScreenProject, ResourceBrowser: true, Options: []ui.Option{{Name: "dev"}, {Name: "prod"}}}}, LaunchPreview: preview, Flow: func(ui.Choice, ui.Draft) ui.Transition { t.Fatal("Space launched"); return ui.Transition{} }})
	update := func(msg tea.Msg) {
		updated, cmd := m.Update(msg)
		m = updated.(ui.AppModel)
		if cmd != nil {
			t.Fatal("unexpected launch")
		}
	}
	update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if m.CurrentScreen() == ui.ScreenProject {
		t.Fatal("ambiguous stage needs an account")
	}
	update(tea.KeyPressMsg{Code: tea.KeyEscape})
	// The callback sees the unchanged staging after cancellation and cursor motion.
	_ = m.View()
	if !reflect.DeepEqual(observed, initial) {
		t.Fatal("cancel changed staged choices", observed)
	}
	update(tea.KeyPressMsg{Code: ' ', Text: " "})
	update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.CurrentScreen() != ui.ScreenProject {
		t.Fatal("staging advanced tabs")
	}
	_ = m.View()
	if observed[ui.ScreenIdentity] != "alice" || observed[ui.ScreenProject] != "dev" || observed[ui.ScreenDocker] != "local" {
		t.Fatal("incorrect staged combination", observed)
	}
	staged := cloneApplicationDraft(observed)
	update(tea.KeyPressMsg{Code: tea.KeyDown})
	_ = m.View()
	if !reflect.DeepEqual(observed, staged) {
		t.Fatal("cursor movement changed staged choices", observed)
	}
	update(tea.KeyPressMsg{Code: ' ', Text: " "})
	_ = m.View()
	if observed[ui.ScreenDocker] != "local" || observed[ui.ScreenProject] != "prod" {
		t.Fatal("Project stage did not preserve compatible resources", observed)
	}
}

func TestADCToggleUpdatesPreviewWithoutStagingCursor(t *testing.T) {
	cfg := previewTestConfig()
	var launched ui.Draft
	picker := ui.Picker{Screen: ui.ScreenDocker, ResourceBrowser: true, Options: []ui.Option{{Name: "local"}}}
	m := ui.NewAppModel(ui.AppOptions{CanApplyShell: true, StartScreen: ui.ScreenDocker, ResourceBrowser: true, ComposeSelection: true, InitialDraft: ui.Draft{ui.ScreenIdentity: "alice"}, Pickers: map[ui.Screen]ui.Picker{ui.ScreenDocker: picker}, LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview { return nextShellPreview(cfg, s, o, d) }, Flow: func(c ui.Choice, d ui.Draft) ui.Transition {
		if c.Action == "default-shell" {
			launched = d
			return ui.Transition{Complete: true}
		}
		transition, ok := shellMenuFlow(cfg, c, d)
		if !ok {
			t.Fatal("unexpected settings action", c)
		}
		return transition
	}})
	update := func(msg tea.Msg) { updated, _ := m.Update(msg); m = updated.(ui.AppModel) }
	update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	if m.CurrentScreen() != ui.ScreenDocker {
		t.Fatal("ADC toggle opened a dialog")
	}
	update(tea.WindowSizeMsg{Width: 180, Height: 24})
	if !strings.Contains(ansi.Strip(m.View().Content), "[ADC]") || !strings.Contains(ansi.Strip(m.View().Content), "› desktop") {
		t.Fatal("closing settings lost ADC", m.View().Content)
	}
	update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if launched[ui.ScreenShellADCOverride] != "identity" {
		t.Fatal("launch ignored previewed setting", launched)
	}
}
