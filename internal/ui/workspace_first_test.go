package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestWorkspaceFirstNavigationAndEmptyGuidance(t *testing.T) {
	screens := []Screen{ScreenWorkspace, ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker}
	pickers := map[Screen]Picker{}
	for _, s := range screens {
		pickers[s] = Picker{Screen: s, Dimension: string(s), ResourceBrowser: true}
	}
	m := NewAppModel(AppOptions{StartScreen: ScreenWorkspace, ResourceBrowser: true, ComposeSelection: true, Pickers: pickers})
	m.width, m.height = 80, 24
	view := ansi.Strip(m.View().Content)
	for _, word := range []string{"Workspaces", "Space", "Ctrl+W"} {
		if !strings.Contains(view, word) {
			t.Fatalf("missing onboarding %q: %s", word, view)
		}
	}
	m.width, m.height = 35, 12
	small := ansi.Strip(m.View().Content)
	for _, word := range []string{"Space", "Ctrl+W", "save workspace"} {
		if !strings.Contains(small, word) {
			t.Fatalf("small terminal loses guidance %q: %s", word, small)
		}
	}
	if strings.Contains(view, "Launcher") {
		t.Fatal("retired tab visible")
	}
	for _, s := range append(screens[1:], ScreenWorkspace) {
		m, _ = updateApp(m, specialKey(tea.KeyTab))
		if m.CurrentScreen() != s {
			t.Fatal("wrong tab order", m.CurrentScreen(), s)
		}
	}
	for _, key := range []tea.KeyPressMsg{textKey("p"), {Code: 'p', Mod: tea.ModCtrl}, textKey("L")} {
		m, _ = updateApp(m, key)
		if m.CurrentScreen() != ScreenWorkspace || m.outcome.Complete {
			t.Fatal("removed action executed")
		}
	}
}

func TestDefaultRootLaunchesFocusedWorkspaceWithoutHome(t *testing.T) {
	m := NewAppModel(AppOptions{CanApplyShell: true, ResourceBrowser: true, ComposeSelection: true, Pickers: map[Screen]Picker{
		ScreenWorkspace: {Screen: ScreenWorkspace, ResourceBrowser: true, ModalActions: true, Focus: "saved", Options: []Option{
			{Name: "first", Selection: Draft{ScreenDocker: "other"}},
			{Name: "saved", Selection: Draft{ScreenDocker: "local"}},
		}},
	}})
	if m.CurrentScreen() != ScreenWorkspace || m.StackDepth() != 1 {
		t.Fatal("default root is not Workspaces")
	}
	m, cmd := updateApp(m, specialKey(tea.KeyEnter))
	if cmd == nil || m.outcome.Draft[ScreenWorkspace] != "saved" || m.outcome.Draft[ScreenDocker] != "local" {
		t.Fatal("default root lost workspace focus or launch", m.outcome)
	}
}
