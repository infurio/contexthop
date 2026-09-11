package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/state"
)

func TestSharedConfigHeaderScopesAndWidths(t *testing.T) {
	for _, width := range []int{35, 60, 100, 124, 164} {
		for _, scope := range []string{"local", "shared"} {
			m := listModel{width: width, height: 30, resourceBrowser: true,
				snapshot: state.Snapshot{Scope: scope, Managed: true, SharedConfigChecked: true,
					Observed:     state.Component{Identity: "active-user", Project: "active-project"},
					SharedConfig: &state.Manifest{Expected: state.Component{Identity: "shared-user", Project: "shared-project", Kubernetes: "cluster", Namespace: "apps"}, ADCMode: "identity"}},
				nextShellPreview: &LaunchPreview{Available: true, Fields: []PickerField{{Label: "id", Value: "selected-user"}}},
			}
			view := ansi.Strip(m.resourceSelection())
			if strings.Contains(view, "Shared config") != (scope == "local" && width >= 140) {
				t.Fatal(width, scope, view)
			}
			if scope == "local" && width >= 140 {
				for _, want := range []string{"shared-user", "shared-project", "cluster/apps", "Selected"} {
					if !strings.Contains(view, want) {
						t.Fatal(width, want, view)
					}
				}
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > m.contentWidth() {
					t.Fatal("overflow", width, line)
				}
			}
			m.snapshot.SharedConfig = nil
			if scope == "local" && width >= 140 && !strings.Contains(ansi.Strip(m.resourceSelection()), "Not set") {
				t.Fatal("missing empty shared state")
			}
		}
	}
}

func TestUseSharedConfigPreservesSelected(t *testing.T) {
	before := Draft{ScreenDocker: "staged"}
	var called Choice
	m := NewAppModel(AppOptions{CanApplyShell: true, ResourceBrowser: true, ComposeSelection: true, InitialDraft: before,
		Pickers: map[Screen]Picker{ScreenWorkspace: {Screen: ScreenWorkspace, ResourceBrowser: true, Options: []Option{{Name: "item"}}}},
		Flow: func(c Choice, d Draft) Transition {
			called = c
			if !reflect.DeepEqual(d, before) {
				t.Fatal(d)
			}
			return Transition{}
		},
	})
	m, _ = updateApp(m, textKey("o"))
	m = chooseMenuAction(t, m, "follow-shared")
	if called.Action != "follow-shared" || !reflect.DeepEqual(m.draft, before) {
		t.Fatal(called, m.draft)
	}
	// Refreshing the shared preview must never stage it.
	updated, _ := m.Update(sharedConfigResult{manifest: &state.Manifest{Destination: "new-shared"}})
	m = updated.(AppModel)
	if m.snapshot.SharedConfig.Destination != "new-shared" || !reflect.DeepEqual(m.draft, before) {
		t.Fatal("refresh changed Selected")
	}
	m.canApplyShell = false
	updated, _ = m.executeBrowserCommand(KeyAction{Action: "follow-shared"}, Option{})
	m = updated.(AppModel)
	if m.CurrentScreen() != "shell-integration" {
		t.Fatal("missing setup guidance")
	}
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if !reflect.DeepEqual(m.draft, before) {
		t.Fatal(m.draft)
	}
}

func TestUseSharedShortcutAcrossTabsAndSearch(t *testing.T) {
	for _, tab := range applicationTabs {
		for _, searching := range []bool{false, true} {
			for _, canApply := range []bool{false, true} {
				before := Draft{ScreenDocker: "staged"}
				called := false
				m := NewAppModel(AppOptions{StartScreen: tab.screen, CanApplyShell: canApply, ResourceBrowser: true, ComposeSelection: true, InitialDraft: before,
					Pickers: map[Screen]Picker{tab.screen: {Screen: tab.screen, ResourceBrowser: true}},
					Flow: func(c Choice, d Draft) Transition {
						called = true
						if c.Action != "follow-shared" || !reflect.DeepEqual(d, before) {
							t.Fatal(c, d)
						}
						return Transition{}
					},
				})
				m.stack[0].searching = searching
				m.stack[0].filter = "no matches"
				m, _ = updateApp(m, tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
				if called != canApply {
					t.Fatal("shortcut dispatch", tab.screen, searching, canApply)
				}
				if !reflect.DeepEqual(m.draft, before) {
					t.Fatal("shortcut changed Selected")
				}
				if !canApply && m.CurrentScreen() != "shell-integration" {
					t.Fatal("missing integration guidance")
				}
			}
		}
	}
}

func TestJoinSharedFooterIsContextual(t *testing.T) {
	for _, width := range []int{35, 60, 100, 164} {
		for _, scope := range []string{"local", "shared"} {
			for _, exists := range []bool{false, true} {
				snapshot := state.Snapshot{Scope: scope}
				if exists {
					snapshot.SharedConfig = &state.Manifest{Destination: "shared"}
				}
				m := NewAppModel(AppOptions{Snapshot: snapshot, CanApplyShell: true, ResourceBrowser: true, ComposeSelection: true,
					Pickers: map[Screen]Picker{ScreenWorkspace: {Screen: ScreenWorkspace, ResourceBrowser: true, Options: []Option{{Name: "item"}}}},
				})
				m.width, m.height = width, 30
				view := ansi.Strip(m.View().Content)
				if strings.Contains(view, "[ctrl+g] Join shared") != (exists && scope != "shared") {
					t.Fatal(width, scope, exists, view)
				}
				for _, line := range strings.Split(view, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatal("overflow", width, line)
					}
				}
			}
		}
	}
}
