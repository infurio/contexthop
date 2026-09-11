package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRefreshReuseAndRemovedLaunchAliasAcrossTabs(t *testing.T) {
	for _, tab := range applicationTabs {
		newModel := func() AppModel {
			return NewAppModel(AppOptions{StartScreen: tab.screen, ComposeSelection: true, Pickers: map[Screen]Picker{
				tab.screen:  {Screen: tab.screen, ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "resource"}}},
				ScreenReuse: {Screen: ScreenReuse, Options: []Option{{Name: "session"}}},
			}})
		}
		for _, inMenu := range []bool{false, true} {
			m := newModel()
			if inMenu {
				m, _ = updateApp(m, textKey("o"))
			}
			m, cmd := updateApp(m, textKey("r"))
			if cmd == nil || !m.probing || m.CurrentScreen() != tab.screen {
				t.Fatal("r failed to refresh", tab.screen, inMenu)
			}
			m = newModel()
			if inMenu {
				m, _ = updateApp(m, textKey("o"))
			}
			m, _ = updateApp(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
			if m.CurrentScreen() != ScreenReuse {
				t.Fatal("Ctrl+R failed to open sessions", tab.screen, inMenu)
			}
		}
		for _, searching := range []bool{false, true} {
			m := newModel()
			m.stack[0].searching = searching
			m, cmd := updateApp(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
			if cmd != nil || m.outcome.Complete || m.CurrentScreen() != tab.screen {
				t.Fatal("Ctrl+S still launches", tab.screen, searching)
			}
		}
		m := newModel()
		m, _ = updateApp(m, textKey("/"))
		m, cmd := updateApp(m, textKey("r"))
		if cmd != nil || m.current().filter != "r" {
			t.Fatal("r stopped being search text", tab.screen)
		}
	}
}

func TestDiscoveryAndImportShortcuts(t *testing.T) {
	for _, tab := range applicationTabs {
		for _, key := range []string{"d", "l"} {
			m := NewAppModel(AppOptions{StartScreen: tab.screen, ComposeSelection: true, Pickers: map[Screen]Picker{
				tab.screen: {Screen: tab.screen, ResourceBrowser: true, ModalActions: true},
			}})
			m, _ = updateApp(m, textKey(key))
			want := ""
			if key == "d" && tab.screen != ScreenDocker {
				want = "discover-resources"
			}
			if key == "l" && (tab.screen == ScreenDocker || tab.screen == ScreenIdentity || tab.screen == ScreenKubernetes) {
				want = "import-local-contexts"
			}
			if m.outcome.Choice.Action != want {
				t.Fatal("incorrect discovery/import action", tab.screen, key, m.outcome)
			}
		}
	}
}

func TestOptionsRunsActionsWithoutShortcuts(t *testing.T) {
	for _, action := range []KeyAction{{Label: "Browser profile", Action: "configure-browser"}, {Label: "Unmap workspace", Action: "docker-unmap-workspace"}} {
		m := NewAppModel(AppOptions{StartScreen: ScreenDocker, ComposeSelection: true, Pickers: map[Screen]Picker{
			ScreenDocker: {Screen: ScreenDocker, ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "target", Actions: []KeyAction{action}}}},
		}})
		m, _ = updateApp(m, textKey("o"))
		m = chooseMenuAction(t, m, action.Action)
		if m.outcome.Choice.Action != action.Action || m.outcome.Choice.Option.Name != "target" {
			t.Fatal("menu-only action unavailable", action, m.outcome)
		}
	}
}
