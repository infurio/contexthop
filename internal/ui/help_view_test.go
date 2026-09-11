package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpLayoutFitsTerminalAndScrollsToLastShortcut(t *testing.T) {
	for _, size := range [][2]int{{140, 40}, {80, 24}, {36, 20}} {
		m := NewAppModel(AppOptions{ComposeSelection: true, StartScreen: ScreenProject, Pickers: map[Screen]Picker{
			ScreenProject: {Screen: ScreenProject, Options: []Option{{Name: "project", Actions: []KeyAction{{Key: "ctrl+d", Label: "Delete project"}}}}},
		}})
		resized, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = resized.(AppModel)
		m.helpVisible = true
		view := ansi.Strip(m.helpPanel(""))
		if !strings.Contains(view, "Chop  /  Keyboard shortcuts") || !strings.Contains(view, "cloud identities") {
			t.Fatalf("missing introduction at %v", size)
		}
		lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
		if len(lines) > size[1] {
			t.Fatalf("help exceeds terminal height at %v", size)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("help exceeds terminal width at %v: %q", size, line)
			}
		}
		content, height := m.helpViewport()
		m, _ = updateApp(m, specialKey(tea.KeyEnd))
		if m.helpOffset != max(0, len(content)-height) {
			t.Fatal("End does not reach final help row")
		}
		end := ansi.Strip(m.helpPanel(""))
		if !strings.Contains(end, "Quit the application") {
			t.Fatalf("last shortcut inaccessible at %v: %s", size, end)
		}
		m, _ = updateApp(m, specialKey(tea.KeyEscape))
		if m.helpVisible {
			t.Fatal("Escape did not close help")
		}
	}
}

func TestWideBrowserHelpUsesBothColumns(t *testing.T) {
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, Pickers: map[Screen]Picker{ScreenWorkspace: {ResourceBrowser: true, Options: []Option{{Name: "workspace"}}}}})
	m.width, m.height = 180, 28
	m.helpVisible = true
	view := ansi.Strip(m.helpPanel(""))
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "AVAILABLE ACTIONS") && ansi.StringWidth(strings.Split(line, "AVAILABLE ACTIONS")[0]) > m.helpWidth()/2 {
			return
		}
	}
	t.Fatal("available actions absent from right column", view)
}
