package main

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/ui"
)

func TestStagedValuesWinOverCursorForLaunchAndSave(t *testing.T) {
	cfg := previewTestConfig()
	staged := ui.Draft{ui.ScreenIdentity: "alice", ui.ScreenProject: "dev", ui.ScreenKubernetes: "dev-cluster", ui.ScreenDocker: "local"}
	for screen, name := range map[ui.Screen]string{ui.ScreenIdentity: "bob", ui.ScreenProject: "prod", ui.ScreenKubernetes: "prod-cluster", ui.ScreenDocker: "remote", ui.ScreenWorkspace: "saved"} {
		for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: tea.KeyEnter, Mod: tea.ModShift}, {Code: 'w', Mod: tea.ModCtrl}} {
			t.Run(string(screen)+key.String(), func(t *testing.T) {
				var request ui.Draft
				m := ui.NewAppModel(ui.AppOptions{StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, CanApplyShell: true, InitialDraft: staged, Pickers: map[ui.Screen]ui.Picker{screen: {Screen: screen, Dimension: string(screen), ResourceBrowser: true, Options: []ui.Option{{Name: name, Identity: true, Project: true, Kubernetes: true, Docker: true}}}}, LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview { return nextShellPreview(cfg, s, o, d) }, Flow: func(_ ui.Choice, d ui.Draft) ui.Transition { request = d; return ui.Transition{Complete: true} }})
				updated, _ := m.Update(tea.WindowSizeMsg{Width: 220, Height: 24})
				m = updated.(ui.AppModel)
				selected := strings.Join(strings.Fields(strings.Join(strings.Split(ansi.Strip(m.View().Content), "\n")[:5], " ")), " ")
				for _, want := range []string{"Selected", "✓ alice@example.com", "✓ dev-project", "✓ gke_dev-project_us-east1_dev-cluster/payments", "✓ desktop"} {
					if !strings.Contains(selected, want) {
						t.Fatal("staged header changed", selected)
					}
				}
				m.Update(key)
				for k, v := range staged {
					if request[k] != v {
						t.Fatalf("%s replaced %s with %q", key.String(), k, request[k])
					}
				}
			})
		}
	}
}

func TestCursorFillsOnlyCompatibleUnstagedComponents(t *testing.T) {
	cfg := previewTestConfig()
	staged := ui.Draft{ui.ScreenIdentity: "alice", ui.ScreenProject: "dev"}
	var observed ui.Draft
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenKubernetes, ResourceBrowser: true, ComposeSelection: true, InitialDraft: staged, Pickers: map[ui.Screen]ui.Picker{ui.ScreenKubernetes: {Screen: ui.ScreenKubernetes, ResourceBrowser: true, Options: []ui.Option{{Name: "dev-cluster"}, {Name: "prod-cluster"}}}}, LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview {
		observed = d
		return nextShellPreview(cfg, s, o, d)
	}})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 220, Height: 24})
	m = next.(ui.AppModel)
	line := strings.Join(strings.Fields(strings.Join(strings.Split(ansi.Strip(m.View().Content), "\n")[:5], " ")), " ")
	for _, want := range []string{"✓ alice@example.com", "✓ dev-project", "› gke_dev-project_us-east1_dev-cluster/payments", "Docker — —"} {
		if !strings.Contains(line, want) {
			t.Fatal(line)
		}
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(ui.AppModel)
	line = strings.Join(strings.Fields(strings.Join(strings.Split(ansi.Strip(m.View().Content), "\n")[:5], " ")), " ")
	if strings.Contains(line, "prod") || !strings.Contains(line, "Kubernetes — —") || !reflect.DeepEqual(observed, staged) {
		t.Fatal("incompatible highlight replaced staging", line, observed)
	}
	// An explicit Space can replace the dependencies; cursor movement cannot.
	next, _ = m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = next.(ui.AppModel)
	line = strings.Join(strings.Fields(strings.Join(strings.Split(ansi.Strip(m.View().Content), "\n")[:5], " ")), " ")
	if !strings.Contains(line, "✓ prod-project") || !strings.Contains(line, "✓ bob@example.com") {
		t.Fatal("Space did not replace dependencies", line)
	}
}

func TestUnstageClearsDependentStaging(t *testing.T) {
	for _, screen := range []ui.Screen{ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker, ui.ScreenWorkspace} {
		cfg := previewTestConfig()
		staged := ui.Draft{ui.ScreenIdentity: "alice", ui.ScreenProject: "dev", ui.ScreenKubernetes: "dev-cluster", ui.ScreenDocker: "local"}
		if screen == ui.ScreenWorkspace {
			staged[screen] = "saved"
		}
		var observed ui.Draft
		m := ui.NewAppModel(ui.AppOptions{StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, InitialDraft: staged, Pickers: map[ui.Screen]ui.Picker{screen: {Screen: screen, ResourceBrowser: true, Options: []ui.Option{{Name: staged[screen]}}}}, LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview {
			observed = d
			return nextShellPreview(cfg, s, o, d)
		}})
		next, _ := m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
		m = next.(ui.AppModel)
		_ = m.View()
		cleared := []ui.Screen{screen}
		switch screen {
		case ui.ScreenIdentity:
			cleared = append(cleared, ui.ScreenProject, ui.ScreenKubernetes)
		case ui.ScreenProject:
			cleared = append(cleared, ui.ScreenKubernetes)
		case ui.ScreenWorkspace:
			cleared = append(cleared, ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker)
		}
		for _, key := range cleared {
			if observed[key] != "" {
				t.Fatalf("unstage %s retained %s: %v", screen, key, observed)
			}
		}
		if screen != ui.ScreenDocker && screen != ui.ScreenWorkspace && observed[ui.ScreenDocker] != "local" {
			t.Fatal("unrelated Docker lost", observed)
		}
	}
}

func TestSpaceStagesWholeWorkspaceAndCursorCannotReplaceIt(t *testing.T) {
	cfg := previewTestConfig()
	var request ui.Draft
	picker := ui.Picker{Screen: ui.ScreenWorkspace, ResourceBrowser: true, Options: []ui.Option{{Name: "saved"}, {Name: "other"}}}
	cfg.Destinations["other"] = cfg.Destinations["saved"]
	other := cfg.Destinations["other"]
	other.Docker = "remote"
	cfg.Destinations["other"] = other
	m := ui.NewAppModel(ui.AppOptions{CanApplyShell: true, StartScreen: ui.ScreenWorkspace, ResourceBrowser: true, ComposeSelection: true, Pickers: map[ui.Screen]ui.Picker{ui.ScreenWorkspace: picker}, LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview { return nextShellPreview(cfg, s, o, d) }, Flow: func(_ ui.Choice, d ui.Draft) ui.Transition { request = d; return ui.Transition{Complete: true} }})
	for _, key := range []tea.KeyPressMsg{{Code: ' ', Text: " "}, {Code: tea.KeyDown}, {Code: tea.KeyEnter}} {
		next, _ := m.Update(key)
		m = next.(ui.AppModel)
	}
	if request[ui.ScreenWorkspace] != "saved" || request[ui.ScreenIdentity] != "alice" || request[ui.ScreenProject] != "dev" || request[ui.ScreenKubernetes] != "dev-cluster" || request[ui.ScreenDocker] != "" {
		t.Fatal("workspace staging replaced by cursor", request)
	}
}

func TestStagedContextCanLaunchWithNoSearchResults(t *testing.T) {
	cfg := previewTestConfig()
	var request ui.Draft
	m := ui.NewAppModel(ui.AppOptions{CanApplyShell: true, StartScreen: ui.ScreenDocker, ResourceBrowser: true, ComposeSelection: true, InitialDraft: ui.Draft{ui.ScreenDocker: "local"}, Pickers: map[ui.Screen]ui.Picker{ui.ScreenDocker: {Screen: ui.ScreenDocker, ResourceBrowser: true, Options: []ui.Option{{Name: "local"}}}}, LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview { return nextShellPreview(cfg, s, o, d) }, Flow: func(_ ui.Choice, d ui.Draft) ui.Transition { request = d; return ui.Transition{Complete: true} }})
	for _, msg := range []tea.Msg{tea.KeyPressMsg{Code: '/', Text: "/"}, tea.PasteMsg{Content: "no-match"}, tea.KeyPressMsg{Code: tea.KeyEnter}} {
		next, _ := m.Update(msg)
		m = next.(ui.AppModel)
	}
	if request[ui.ScreenDocker] != "local" {
		t.Fatal("search hid staged launch", request)
	}
}
