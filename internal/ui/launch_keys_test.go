package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLaunchKeysWithAndWithoutShellIntegration(t *testing.T) {
	for _, tab := range applicationTabs {
		for _, searching := range []bool{false, true} {
			for _, canApply := range []bool{false, true} {
				for _, binding := range []struct {
					key    tea.KeyPressMsg
					action string
				}{
					{tea.KeyPressMsg{Code: tea.KeyEnter}, "default-shell"},
					{tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, "apply-shell"},
					{tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}, "apply-shell"},
					{tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}, "launch-shell"},
				} {
					picker := Picker{Screen: tab.screen, ResourceBrowser: true, Options: []Option{{Name: "target"}}}
					m := NewAppModel(AppOptions{StartScreen: tab.screen, ComposeSelection: true, ResourceBrowser: true, CanApplyShell: canApply, Pickers: map[Screen]Picker{tab.screen: picker}})
					m.width, m.height = 120, 24
					m.stack[0].searching = searching
					before := cloneDraft(m.draft)
					view := ansi.Strip(m.View().Content)
					current := "[shift+enter] Apply here"
					if !canApply {
						current = "[shift+enter] Set up shell"
					}
					if !strings.Contains(view, current) || !strings.Contains(view, "[option+enter] Subshell") {
						t.Fatal("footer mislabels launch bindings", view)
					}
					m, cmd := updateApp(m, binding.key)
					if binding.action == "apply-shell" && !canApply {
						if cmd != nil || m.outcome.Complete || m.CurrentScreen() != "shell-integration" || !reflect.DeepEqual(before, m.draft) {
							t.Fatal("missing integration launched or changed selection", tab.screen, binding.key)
						}
						if !strings.Contains(m.current().picker.Description, "Option+Enter") {
							t.Fatal("setup guidance lacks subshell shortcut")
						}
						m, _ = updateApp(m, specialKey(tea.KeyEscape))
						if m.CurrentScreen() != tab.screen || !reflect.DeepEqual(before, m.draft) {
							t.Fatal("setup guidance lost originating selection")
						}
					} else if cmd == nil || !m.outcome.Complete || m.outcome.Choice.Action != binding.action {
						t.Fatal("wrong launch action", tab.screen, binding.key, m.outcome)
					}
				}
			}
		}
	}
}
