package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestHiddenVisibilityStaysInEntityTabAndSurvivesRefresh(t *testing.T) {
	pickers := map[Screen]Picker{}
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker} {
		pickers[screen] = Picker{Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true,
			Options: []Option{
				{Name: "visible", Identity: true, Project: true, Kubernetes: true, Docker: true},
				{Name: "suppressed", Hidden: true, Identity: true, Project: true, Kubernetes: true, Docker: true, Actions: []KeyAction{{Key: "H", Label: "Unhide", Action: "unhide-entity"}}},
			}}
	}
	calls := 0
	model := NewAppModel(AppOptions{StartScreen: ScreenIdentity, ResourceBrowser: true, Pickers: pickers, Flow: func(choice Choice, _ Draft) Transition {
		calls++
		if choice.Option.Name != "suppressed" || choice.Action != "unhide-entity" {
			t.Fatalf("wrong action: %#v", choice)
		}
		picker := pickers[choice.Screen]
		picker.Options = append([]Option(nil), picker.Options...)
		picker.Options[1].Hidden = false
		picker.Focus = "suppressed"
		return Transition{ResetNavigation: true, Picker: picker}
	}})
	press := func(key string) { model, _ = updateApp(model, textKey(key)) }
	for _, tab := range []string{"I", "P", "K", "D"} {
		if tab == "I" {
			model.switchResourceTab(ScreenIdentity)
		} else {
			press(tab)
		}
		screen := model.CurrentScreen()
		if len(filteredPickerOptions(model.current().picker, "")) != 1 {
			t.Fatal("hidden row visible by default")
		}
		press("h")
		if model.CurrentScreen() != screen || model.StackDepth() != 1 || len(filteredPickerOptions(model.current().picker, "")) != 2 {
			t.Fatal("toggle left entity tab or lost rows")
		}
	}
	model.switchResourceTab(ScreenIdentity)
	if !model.current().picker.ShowHidden {
		t.Fatal("tab forgot visibility")
	}
	model, _ = updateApp(model, specialKey(tea.KeyDown))
	press("H")
	if calls != 1 || !model.current().picker.ShowHidden || model.current().cursor != 1 || model.StackDepth() != 1 {
		t.Fatal("refresh lost visibility, focus, or navigation")
	}
	press("h")
	if len(filteredPickerOptions(model.current().picker, "")) != 2 {
		t.Fatal("unhidden row disappeared")
	}
	press("P")
	press("/")
	press("h")
	if !model.current().picker.ShowHidden || model.current().filter != "h" {
		t.Fatal("search key toggled visibility")
	}
}

func TestHiddenRowsAreMutedAndCurrentSkinSetsBlackBackground(t *testing.T) {
	picker := Picker{Screen: ScreenDocker, Dimension: "docker", ResourceBrowser: true, ModalActions: true, Options: []Option{
		{Name: "suppressed", Docker: true, DockerContext: "suppressed", Hidden: true},
	}}
	model := NewAppModel(AppOptions{StartScreen: ScreenDocker, ResourceBrowser: true, Pickers: map[Screen]Picker{ScreenDocker: picker}})
	model, _ = updateApp(model, textKey("h"))
	view := model.View()
	if view.BackgroundColor != backgroundColor || view.ForegroundColor != bodyColor {
		t.Fatal("skin did not set terminal colors")
	}
	if !strings.Contains(view.Content, strings.TrimSuffix(hiddenSelectedStyle.Render("> suppressed"), "\x1b[m")) {
		t.Fatalf("selected hidden row is not muted: %q", view.Content)
	}
	if !strings.Contains(ansi.Strip(view.Content), "1 hidden shown") {
		t.Fatal("hidden state is not visible")
	}
}
