package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/ui"
)

func TestADCToggleAcrossEntityTabs(t *testing.T) {
	cfg := previewTestConfig()
	for screen, name := range map[ui.Screen]string{
		ui.ScreenIdentity: "alice", ui.ScreenProject: "dev", ui.ScreenKubernetes: "dev-cluster", ui.ScreenDocker: "local", ui.ScreenWorkspace: "saved",
	} {
		var launch ui.Draft
		m := ui.NewAppModel(ui.AppOptions{CanApplyShell: true, StartScreen: screen, ComposeSelection: true, ResourceBrowser: true,
			InitialDraft:  ui.Draft{ui.ScreenIdentity: "alice"},
			Pickers:       map[ui.Screen]ui.Picker{screen: {Screen: screen, ResourceBrowser: true, ModalActions: true, Options: []ui.Option{{Name: name}}}},
			LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview { return nextShellPreview(cfg, s, o, d) },
			Flow: func(c ui.Choice, d ui.Draft) ui.Transition {
				if c.Action != "default-shell" {
					t.Fatal(c)
				}
				launch = d
				return ui.Transition{Complete: true}
			}})
		press := func(k tea.KeyPressMsg) { next, _ := m.Update(k); m = next.(ui.AppModel) }
		press(tea.KeyPressMsg{Code: 's', Text: "s"})
		if m.CurrentScreen() != screen {
			t.Fatal("obsolete settings shortcut opened a dialog")
		}
		press(tea.KeyPressMsg{Code: 'o', Text: "o"})
		if !strings.Contains(ansi.Strip(m.View().Content), "ADC") {
			t.Fatal("ADC missing from Options")
		}
		press(tea.KeyPressMsg{Code: 'A', Text: "A"})
		if m.CurrentScreen() != screen {
			t.Fatal("menu toggle did not return to tab")
		}
		press(tea.KeyPressMsg{Code: tea.KeyEnter})
		resolved, err := resolveShellSelection(cfg, launch)
		want := "identity"
		// The saved preset already enables ADC; toggling turns it off.
		if screen == ui.ScreenWorkspace {
			want = ""
		}
		if err != nil || resolved.ADCMode != want {
			t.Fatal(screen, resolved.ADCMode, err)
		}
	}
}

func TestADCOverrideFollowsCursorAndResetsFromOptions(t *testing.T) {
	cfg := previewTestConfig()
	cfg.Destinations["second"] = cfg.Destinations["saved"]
	var captured ui.Draft
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenWorkspace, ComposeSelection: true, ResourceBrowser: true,
		Pickers: map[ui.Screen]ui.Picker{ui.ScreenWorkspace: {Screen: ui.ScreenWorkspace, ResourceBrowser: true, ModalActions: true, Options: []ui.Option{{Name: "saved"}, {Name: "second"}}}},
		LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview {
			captured = cloneApplicationDraft(d)
			return nextShellPreview(cfg, s, o, d)
		}})
	press := func(k tea.KeyPressMsg) { next, _ := m.Update(k); m = next.(ui.AppModel) }
	next, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m = next.(ui.AppModel)
	press(tea.KeyPressMsg{Code: 'A', Text: "A"})
	press(tea.KeyPressMsg{Code: tea.KeyDown})
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "[ADC]") || captured[ui.ScreenWorkspace] != "" || captured[ui.ScreenIdentity] != "" {
		t.Fatal("toggle staged the cursor or lost override", captured, view)
	}
	press(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = chooseVisibleOption(t, m, "Use workspace ADC setting")
	view = ansi.Strip(m.View().Content)
	if m.CurrentScreen() != ui.ScreenWorkspace || !strings.Contains(view, "[ADC]") || captured[ui.ScreenShellADCOverride] != "" {
		t.Fatal("reset failed", captured, view)
	}
}

func TestADCWithoutIdentityAndDuringSearch(t *testing.T) {
	cfg := previewTestConfig()
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenDocker, ComposeSelection: true, ResourceBrowser: true,
		Pickers:       map[ui.Screen]ui.Picker{ui.ScreenDocker: {Screen: ui.ScreenDocker, ResourceBrowser: true, ModalActions: true, Options: []ui.Option{{Name: "local"}}}},
		LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview { return nextShellPreview(cfg, s, o, d) }})
	press := func(k tea.KeyPressMsg) { next, _ := m.Update(k); m = next.(ui.AppModel) }
	press(tea.KeyPressMsg{Code: 'A', Text: "A"})
	if !strings.Contains(m.View().Content, "Select an identity") {
		t.Fatal("missing ADC prerequisite guidance")
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	press(tea.KeyPressMsg{Code: '/', Text: "/"})
	press(tea.KeyPressMsg{Code: 'A', Text: "A"})
	if m.CurrentScreen() != ui.ScreenDocker {
		t.Fatal("search executed ADC shortcut")
	}
}
