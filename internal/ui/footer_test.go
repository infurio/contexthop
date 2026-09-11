package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestBrowserFooterKeepsEssentialControls(t *testing.T) {
	for _, width := range []int{35, 48, 60, 80, 120} {
		for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker, ScreenWorkspace} {
			picker := Picker{Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "chosen", Identity: true, Project: true, Kubernetes: true, Docker: true, Actions: []KeyAction{{Key: KeyConsole, Label: "Open web console", Action: "open-console"}}}}}
			m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: screen, Pickers: map[Screen]Picker{screen: picker}})
			m.width, m.height = width, 24
			view := ansi.Strip(m.View().Content)
			for _, want := range []string{"Subshell", "Update shared", "Stage", "Options", "[?]"} {
				if !strings.Contains(strings.ToLower(view), strings.ToLower(want)) {
					t.Fatalf("%s at %d missing %s: %s", screen, width, want, view)
				}
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatal("footer overflow", width, line)
				}
			}
			if width >= 120 && !strings.Contains(view, "[ctrl+o] Browser") {
				t.Fatal("wide footer missing Browser", view)
			}
			if width >= 80 {
				for _, want := range []string{"Save workspace"} {
					if !strings.Contains(view, want) {
						t.Fatalf("missing %s: %s", want, view)
					}
				}
			}
		}
	}
}

func TestFooterLabelsMatchSearchAndInput(t *testing.T) {
	picker := Picker{Screen: ScreenProject, ModalActions: true, Options: []Option{{Name: "item"}}}
	m := NewAppModel(AppOptions{StartScreen: ScreenProject, Pickers: map[Screen]Picker{ScreenProject: picker}})
	m, _ = updateApp(m, textKey("/"))
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "[enter] Done") || strings.Contains(view, "[enter] Apply") {
		t.Fatal(view)
	}
	m = NewAppModel(AppOptions{StartScreen: ScreenInput, Pickers: map[Screen]Picker{ScreenInput: {Screen: ScreenInput, Input: &Input{}}}})
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "[F1] Help") {
		t.Fatal(view)
	}
}
