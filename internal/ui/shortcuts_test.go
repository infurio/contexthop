package ui

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

func shortcutTestBrowser() AppModel {
	return NewAppModel(AppOptions{
		StartScreen: ScreenKubernetes, ResourceBrowser: true,
		Pickers: map[Screen]Picker{
			ScreenKubernetes: {Screen: ScreenKubernetes, ResourceBrowser: true, ModalActions: true, Options: []Option{
				{Name: "first", Label: "First"},
				{Name: "second", Label: "Second", Actions: []KeyAction{
					{Key: KeyConsole, Action: "open-console"},
				}},
			}},
			ScreenReuse: {Screen: ScreenReuse},
		},
	})
}

func TestHelpAndRemovedActionsKeys(t *testing.T) {
	for _, key := range []rune{tea.KeyF1, tea.KeyF2} {
		browser := shortcutTestBrowser()
		browser.stack[len(browser.stack)-1].searching, browser.stack[len(browser.stack)-1].filter = true, "second"
		updated, _ := browser.Update(tea.KeyPressMsg{Code: key})
		browser = updated.(AppModel)
		if browser.helpVisible != (key == tea.KeyF1) || browser.CurrentScreen() != ScreenKubernetes {
			t.Fatalf("browser key %v opened wrong screen", key)
		}
	}
}

func TestSharedResourceShortcutsWorkDuringSearch(t *testing.T) {
	for _, searching := range []bool{false, true} {
		for key, action := range map[rune]string{'o': "open-console"} {
			browser := shortcutTestBrowser()
			browser.stack[len(browser.stack)-1].searching, browser.stack[len(browser.stack)-1].filter = searching, "second"
			updated, _ := browser.Update(tea.KeyPressMsg{Code: key, Mod: tea.ModCtrl, Text: string(key)})
			got := updated.(AppModel).outcome
			if got.Choice.Action != action || got.Choice.Option.Name != "second" {
				t.Fatalf("searching=%t key=%c: %#v", searching, key, got)
			}
		}
	}
	browser := shortcutTestBrowser()
	browser.stack[len(browser.stack)-1].searching, browser.stack[len(browser.stack)-1].filter = true, "second"
	updated, _ := browser.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl, Text: "u"})
	browser = updated.(AppModel)
	if browser.current().filter != "" {
		t.Fatal("Ctrl+U did not clear search")
	}
	updated, _ = browser.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl, Text: "r"})
	if updated.(AppModel).CurrentScreen() != ScreenReuse {
		t.Fatal("Ctrl+R did not open sessions")
	}
}

func TestResourceTabsHaveNoActionsMenu(t *testing.T) {
	for _, screen := range []Screen{ScreenWorkspace, ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker} {
		for _, key := range []tea.KeyPressMsg{textKey(":"), {Code: tea.KeyF2}} {
			m := NewAppModel(AppOptions{StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, Pickers: map[Screen]Picker{screen: {Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true}}})
			m, _ = updateApp(m, key)
			if m.CurrentScreen() != screen || m.StackDepth() != 1 {
				t.Fatal("obsolete menu is reachable", screen)
			}
		}
	}
}
