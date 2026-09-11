package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestDetailsAreReadOnlyScrollableAndPreserveSelection(t *testing.T) {
	for _, width := range []int{48, 120} {
		picker := Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "project", Project: true, Summary: "source:GCP\n" + strings.Repeat("A long mapping description with multiple words to wrap.\n", 40) + "FINAL DETAIL"}}}
		calls := 0
		model := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject, InitialDraft: Draft{ScreenProject: "project"}, Pickers: map[Screen]Picker{ScreenProject: picker}, Flow: func(Choice, Draft) Transition { calls++; return Transition{} }})
		next, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 22})
		model = next.(AppModel)
		model, _ = updateApp(model, textKey("i"))
		if len(model.current().picker.Options) != 0 || model.current().picker.ReadOnlyText == "" {
			t.Fatal("details still encoded as options")
		}
		page := ansi.Strip(model.readOnlyPage(model.current(), width-4, 20))
		for _, unwanted := range []string{"> ", "[enter]", "Select", "No options"} {
			if strings.Contains(page, unwanted) {
				t.Fatalf("details include picker control %q", unwanted)
			}
		}
		for _, key := range []string{"enter", "space", "/", "d", "ctrl+d"} {
			model, _ = updateApp(model, textKey(key))
			if model.CurrentScreen() != "resource-details" || model.current().textOffset != 0 || calls != 0 {
				t.Fatalf("%s acted on read-only details", key)
			}
		}
		model, _ = updateApp(model, tea.KeyPressMsg{Code: tea.KeyPgDown})
		if model.current().textOffset <= 0 {
			t.Fatal("Page Down did not scroll")
		}
		model, _ = updateApp(model, tea.KeyPressMsg{Code: tea.KeyEnd})
		if !strings.Contains(ansi.Strip(model.View().Content), "FINAL DETAIL") {
			t.Fatal("End did not reveal final detail")
		}
		next, _ = model.Update(tea.WindowSizeMsg{Width: width / 2, Height: 16})
		model = next.(AppModel)
		model, _ = updateApp(model, tea.KeyPressMsg{Code: tea.KeyEnd})
		if !strings.Contains(ansi.Strip(model.View().Content), "FINAL DETAIL") {
			t.Fatal("resized details lost final text")
		}
		model, _ = updateApp(model, tea.KeyPressMsg{Code: tea.KeyHome})
		if model.current().textOffset != 0 {
			t.Fatal("Home did not reset scrolling")
		}
		model, _ = updateApp(model, tea.KeyPressMsg{Code: tea.KeyEscape})
		if model.CurrentScreen() != ScreenProject || model.draft[ScreenProject] != "project" || model.StackDepth() != 1 {
			t.Fatal("closing details changed browser selection")
		}
	}
}
