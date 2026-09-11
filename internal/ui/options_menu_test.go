package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"reflect"
	"strings"
	"testing"
)

func chooseMenuAction(t *testing.T, m AppModel, name string) AppModel {
	t.Helper()
	if m.CurrentScreen() != screenOptions {
		t.Fatal("Options not open", m.CurrentScreen())
	}
	found := false
	for _, o := range m.current().picker.Options {
		if o.Name == name {
			found = true
		}
	}
	if !found {
		t.Fatal("missing option", name)
	}
	m.stack[len(m.stack)-1].focusOption(name)
	m, _ = updateApp(m, specialKey(tea.KeyEnter))
	return m
}

func TestOptionsDispatchAndCancellationAcrossTabs(t *testing.T) {
	for _, tab := range applicationTabs {
		for _, viaShortcut := range []bool{false, true} {
			var called Choice
			before := Draft{ScreenDocker: "staged"}
			picker := Picker{Screen: tab.screen, Dimension: string(tab.screen), ResourceBrowser: true, Options: []Option{{Name: "first", Label: "First", Identity: true, Project: true, Kubernetes: true, Docker: true}, {Name: "target", Label: "Target", Identity: true, Project: true, Kubernetes: true, Docker: true, Actions: []KeyAction{{Key: "t", Label: "Edit tags", Action: "entity-labels"}, {Key: "ctrl+d", Label: "Delete", Action: "remove-entity"}}}}}
			m := NewAppModel(AppOptions{StartScreen: tab.screen, ResourceBrowser: true, ComposeSelection: true, InitialDraft: before, Pickers: map[Screen]Picker{tab.screen: picker}, Flow: func(c Choice, d Draft) Transition {
				called = c
				if !reflect.DeepEqual(d, before) {
					t.Fatal("menu changed staged context", d)
				}
				return Transition{Picker: Picker{Screen: "editor", HideSearch: true}}
			}})
			m.width, m.height = 110, 28
			m.stack[0].filter = "target"
			root := m.current()
			m, _ = updateApp(m, textKey("o"))
			if m.CurrentScreen() != screenOptions {
				t.Fatal("menu unavailable", tab.screen)
			}
			m.stack[len(m.stack)-1].focusOption("entity-labels")
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "Highlighted item") || !strings.Contains(view, "Edit tags") || !strings.Contains(view, "ctrl+d") {
				t.Fatal(view)
			}
			m, _ = updateApp(m, specialKey(tea.KeyEscape))
			if !reflect.DeepEqual(m.draft, before) || m.current().filter != root.filter || m.current().cursor != root.cursor {
				t.Fatal("cancel changed browsing state")
			}
			m, _ = updateApp(m, textKey("o"))
			if viaShortcut {
				m, _ = updateApp(m, textKey("t"))
			} else {
				m = chooseMenuAction(t, m, "entity-labels")
			}
			if called.Option.Name != "target" || called.Action != "entity-labels" || called.Screen != tab.screen || m.StackDepth() != 2 || m.CurrentScreen() != "editor" {
				t.Fatal("wrong menu dispatch", called, m.CurrentScreen())
			}
			m, _ = updateApp(m, specialKey(tea.KeyEscape))
			if m.CurrentScreen() != tab.screen || m.current().filter != "target" {
				t.Fatal("editor returned to wrong place")
			}
		}
	}
}

func TestOptionsEmptyTabsAndSearch(t *testing.T) {
	for _, tab := range applicationTabs {
		m := NewAppModel(AppOptions{StartScreen: tab.screen, ResourceBrowser: true, ComposeSelection: true, Pickers: map[Screen]Picker{tab.screen: {Screen: tab.screen, Dimension: string(tab.screen), ResourceBrowser: true}}})
		m, _ = updateApp(m, textKey("/"))
		m, _ = updateApp(m, textKey("o"))
		if m.current().filter != "o" || m.CurrentScreen() != tab.screen {
			t.Fatal("o stopped being search text")
		}
		m, _ = updateApp(m, specialKey(tea.KeyEscape))
		m, _ = updateApp(m, textKey("o"))
		for _, o := range m.current().picker.Options {
			if o.Name == "ui:info" || o.Name == "shell-menu" || o.Name == "remove-entity" {
				t.Fatal("unavailable row action shown", o)
			}
		}
		if len(m.current().picker.Options) == 0 {
			t.Fatal("empty tab lost management actions")
		}
	}
}

func TestOptionsMenuScrollsAndFitsSmallTerminals(t *testing.T) {
	for _, size := range [][2]int{{35, 12}, {80, 24}, {118, 32}} {
		picker := Picker{Screen: ScreenKubernetes, Dimension: "kubernetes", ResourceBrowser: true, OperationDetails: "Full results", Options: []Option{{Name: "cluster", Label: "Cluster", Kubernetes: true, Actions: []KeyAction{{Key: "m", Label: "Edit mappings", Action: "map"}, {Key: "t", Label: "Edit tags", Action: "tags"}, {Key: "H", Label: "Hide item", Action: "hide"}, {Key: "ctrl+d", Label: "Delete", Action: "delete"}}}}}
		m := NewAppModel(AppOptions{StartScreen: ScreenKubernetes, ResourceBrowser: true, ComposeSelection: true, InitialDraft: Draft{ScreenKubernetes: "cluster"}, Pickers: map[Screen]Picker{ScreenKubernetes: picker}})
		m.width, m.height = size[0], size[1]
		m, _ = updateApp(m, textKey("o"))
		for _, key := range []tea.KeyPressMsg{specialKey(tea.KeyHome), specialKey(tea.KeyEnd)} {
			m, _ = updateApp(m, key)
			view := ansi.Strip(m.View().Content)
			lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
			if !strings.Contains(view, "[esc] Close") {
				t.Fatal("menu close hint clipped", size, view)
			}
			if len(lines) > size[1] {
				t.Fatal("menu exceeds height", size, view)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("menu exceeds width", size, line)
				}
			}
			if key.Code == tea.KeyEnd && !strings.Contains(view, m.current().picker.Options[len(m.current().picker.Options)-1].Label) {
				t.Fatal("last menu item inaccessible", size, view)
			}
		}
	}
}

func TestPrimaryActionsRunFromOptions(t *testing.T) {
	for _, tab := range applicationTabs {
		for _, action := range []string{"next-apply", "next-launch", "stage", "save-selection-workspace"} {
			m := NewAppModel(AppOptions{StartScreen: tab.screen, ResourceBrowser: true, ComposeSelection: true, CanApplyShell: true, Pickers: map[Screen]Picker{tab.screen: {ResourceBrowser: true, Options: []Option{{Name: "target"}}}}})
			m, _ = updateApp(m, textKey("o"))
			m = chooseMenuAction(t, m, action)
			if action == "stage" {
				if m.outcome.Complete || m.draft[tab.screen] != "target" {
					t.Fatal("stage menu action launched instead", tab.screen, m.outcome)
				}
				continue
			}
			expected := map[string]string{"next-apply": "apply-shell", "next-launch": "launch-shell", "save-selection-workspace": "save-selection-workspace"}[action]
			if !m.outcome.Complete || m.outcome.Choice.Action != expected {
				t.Fatal("Enter ran wrong menu action", action, m.outcome)
			}
		}
	}
}
