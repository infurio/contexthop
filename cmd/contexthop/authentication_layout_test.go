package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/ui"
)

func TestAuthenticationDescriptionsRemainVisible(t *testing.T) {
	cfg := visibilityConfig()
	auth := providerAuthPicker(cfg, "identity", "identity", "")
	for _, size := range [][2]int{{60, 24}, {80, 24}, {100, 30}} {
		picker := interactiveBrowserPicker(cfg, cfg, ui.ScreenIdentity, ui.Draft{})
		model := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenIdentity, ResourceBrowser: true,
			Pickers: map[ui.Screen]ui.Picker{ui.ScreenIdentity: picker},
			Flow:    func(ui.Choice, ui.Draft) ui.Transition { return ui.Transition{Picker: auth} },
		})
		update := func(msg tea.Msg) { next, _ := model.Update(msg); model = next.(ui.AppModel) }
		update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		if model.CurrentScreen() != screenProviderAuth {
			t.Fatal("authentication did not open")
		}
		for index, option := range auth.Options {
			if index > 0 {
				update(tea.KeyPressMsg{Code: tea.KeyDown})
			}
			view := ansi.Strip(model.View().Content)
			if !strings.Contains(view, "> "+option.Label) {
				t.Fatalf("selected label cropped at %v:\n%s", size, view)
			}
			// Every word of the selected description must survive wrapping/scrolling.
			for _, word := range strings.Fields(option.Detail) {
				if !strings.Contains(view, word) {
					t.Fatalf("description cropped (%q) at %v:\n%s", word, size, view)
				}
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("dialog exceeds terminal width at %v", size)
				}
			}
			if len(strings.Split(strings.TrimSuffix(view, "\n"), "\n")) > size[1] {
				t.Fatalf("dialog exceeds terminal height at %v", size)
			}
			if size == [2]int{80, 24} && index == 2 {
				t.Log("ADC selection at 80×24:\n" + view)
			}
		}
	}
}
