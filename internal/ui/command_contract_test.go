package ui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
)

func TestEmptyResourceSearchProtectsAllPrintableCommands(t *testing.T) {
	for _, tab := range applicationTabs {
		t.Run(string(tab.screen), func(t *testing.T) {
			called := false
			m := NewAppModel(AppOptions{StartScreen: tab.screen, ResourceBrowser: true, ComposeSelection: true,
				Pickers: map[Screen]Picker{tab.screen: {Screen: tab.screen, Dimension: string(tab.screen), ResourceBrowser: true}},
				Flow:    func(Choice, Draft) Transition { called = true; return Transition{} }})
			m, _ = updateApp(m, textKey("/"))
			query := "?pnNdblhqcsrIWPKDL"
			for _, r := range query {
				m, _ = updateApp(m, textKey(string(r)))
			}
			if called || m.helpVisible || m.current().filter != query {
				t.Fatal("search ran command", m.current())
			}
			m, _ = updateApp(m, specialKey(tea.KeyEscape))
			if m.current().filter != query || m.current().searching {
				t.Fatal("search did not cancel")
			}
			m, _ = updateApp(m, textKey("?"))
			if !m.helpVisible {
				t.Fatal("empty tab cannot open Help")
			}
			m, _ = updateApp(m, specialKey(tea.KeyEscape))
			m, cmd := updateApp(m, textKey("q"))
			if cmd == nil {
				t.Fatal("Quit advertised but not executable")
			}
		})
	}
}

func TestNoMatchResourceFooterDoesNotOfferSelection(t *testing.T) {
	for _, width := range []int{35, 80, 110} {
		m := listModel{dimension: "docker", resourceBrowser: true, modalActions: true, searching: true, width: width, height: 12, filter: "missing"}
		lines := strings.Split(m.resourceBrowserView(), "\n")
		footer := strings.Join(lines[len(lines)-2:], "\n")
		if strings.Contains(footer, "enter") || strings.Contains(footer, "Select") {
			t.Fatal("no result can be selected", footer)
		}
	}
}
