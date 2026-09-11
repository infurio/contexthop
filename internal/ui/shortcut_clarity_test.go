package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestShortcutLabelsDistinguishShiftAndTagFilter(t *testing.T) {
	for _, test := range []struct{ key, want string }{{"N", "shift+n"}, {"H", "shift+h"}, {"n", "n"}, {"F1", "F1"}} {
		if got := ansi.Strip(shortcut(test.key, "Action")); got != "["+test.want+"] Action" {
			t.Fatal(got)
		}
	}
	m := shortcutTestBrowser()
	m.width, m.height = 118, 28

	m, _ = updateApp(m, textKey("/"))
	for _, key := range "tNH" {
		m, _ = updateApp(m, textKey(string(key)))
	}
	if m.current().filter != "tNH" || m.CurrentScreen() != ScreenKubernetes {
		t.Fatal("shifted keys did not remain text")
	}
}

func TestHiddenVisibilityAndItemActionsAreDistinct(t *testing.T) {
	picker := Picker{Screen: ScreenDocker, Dimension: "docker", ResourceBrowser: true, Options: []Option{{Name: "visible", DockerContext: "visible", Docker: true, Actions: []KeyAction{{Key: "H", Label: "Hide item", Action: "hide-entity"}}}, {Name: "hidden", DockerContext: "hidden", Docker: true, Hidden: true, Actions: []KeyAction{{Key: "H", Label: "Unhide item", Action: "unhide-entity"}}}}}
	m := NewAppModel(AppOptions{StartScreen: ScreenDocker, ResourceBrowser: true, ComposeSelection: true, Pickers: map[Screen]Picker{ScreenDocker: picker}})
	m.width, m.height = 118, 28
	m, _ = updateApp(m, textKey("o"))
	view := m.helpText()
	for _, hint := range []string{"Show hidden", "Hide item"} {
		if !strings.Contains(view, hint) {
			t.Fatal("missing hint", hint, view)
		}
	}
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	m, _ = updateApp(m, textKey("h"))
	if !strings.Contains(m.helpText(), "Hide hidden") {
		t.Fatal("visibility toggle label not updated")
	}
	m, _ = updateApp(m, specialKey(tea.KeyDown))
	m, _ = updateApp(m, textKey("H"))
	if m.outcome.Choice.Action != "unhide-entity" {
		t.Fatal("Shift+H did not target highlighted item", m.outcome)
	}
}
