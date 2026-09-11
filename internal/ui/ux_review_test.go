package ui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSearchAndInputTreatActionCharactersAsText(t *testing.T) {
	calls := 0
	picker := Picker{Screen: ScreenCatalogEntity, Options: []Option{{Name: "mat?h", Actions: []KeyAction{{Key: "m", Action: "map"}, {Key: "a", Action: "auth"}, {Key: "t", Action: "tags"}, {Key: "h", Action: "hide"}}}}}
	m := NewAppModel(AppOptions{StartScreen: ScreenCatalogEntity, Pickers: map[Screen]Picker{ScreenCatalogEntity: picker}, Flow: func(Choice, Draft) Transition { calls++; return Transition{} }})
	m, _ = updateApp(m, textKey("/"))
	for _, r := range "mat?h" {
		m, _ = updateApp(m, textKey(string(r)))
	}
	if calls != 0 || m.helpVisible || m.current().filter != "mat?h" {
		t.Fatal("search executed a shortcut", calls, m.current())
	}
	m = NewAppModel(AppOptions{StartScreen: ScreenInput, Pickers: map[Screen]Picker{ScreenInput: {Screen: ScreenInput, Input: &Input{}}}})
	m, _ = updateApp(m, textKey("?"))
	m, _ = updateApp(m, specialKey(tea.KeyF1))
	updated, _ := m.Update(tea.PasteMsg{Content: "ignored"})
	m = updated.(AppModel)
	if !m.helpVisible || m.current().input != "?" {
		t.Fatal("help changed input")
	}
	if strings.Contains(m.helpText(), "Start subshell") {
		t.Fatal("inactive browser help in input")
	}
}

func TestDirectEditUsesHighlightedRowAndPreservesSelection(t *testing.T) {
	picker := Picker{Screen: ScreenWorkspace, Dimension: "workspace", ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "selected"}, {Name: "highlighted", Actions: []KeyAction{{Key: "e", Label: "Edit workspace", Action: "workspace-configure"}}}}}
	before := Draft{ScreenWorkspace: "selected", ScreenDocker: "local"}
	called := false
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenWorkspace, InitialDraft: before, Pickers: map[Screen]Picker{ScreenWorkspace: picker}, Flow: func(c Choice, d Draft) Transition {
		if c.Action != "workspace-configure" || c.Option.Name != "highlighted" || !reflect.DeepEqual(d, before) {
			t.Fatalf("wrong action target: %#v %v", c, d)
		}
		called = true
		return Transition{Picker: Picker{Screen: "editor"}}
	}})
	m, _ = updateApp(m, specialKey(tea.KeyDown))
	m, _ = updateApp(m, textKey("e"))
	if !called || m.CurrentScreen() != "editor" || m.StackDepth() != 2 {
		t.Fatal("edit did not dispatch or leaked navigation")
	}
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if !reflect.DeepEqual(m.draft, before) || m.current().cursor != 1 {
		t.Fatal("cancel lost browser state")
	}
}

func TestBrowseAllKeepsLaunchSelection(t *testing.T) {
	before := Draft{ScreenIdentity: "account", ScreenProject: "project", ScreenKubernetes: "cluster"}
	browser := func(screen Screen, d Draft) Picker {
		scope := "all"
		if d[ScreenIdentity] != "" {
			scope = "account"
		}
		return Picker{Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true, ScopeLabel: scope}
	}
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject, InitialDraft: before, Pickers: map[Screen]Picker{ScreenProject: browser(ScreenProject, before)}, BrowserPicker: browser})
	m, _ = updateApp(m, textKey("b"))
	if !reflect.DeepEqual(m.draft, before) || m.current().picker.ScopeLabel != "all" {
		t.Fatal("browsing cleared selected context")
	}
	m, _ = updateApp(m, textKey("b"))
	if m.current().picker.ScopeLabel != "account" || !reflect.DeepEqual(m.draft, before) {
		t.Fatal("scope not restored")
	}
	m, _ = updateApp(m, textKey("c"))
	if m.hasStagedSelection() {
		t.Fatal("explicit clear failed")
	}
}

func TestResourceInfoAndResultsAreSeparateAndFitSmallTerminals(t *testing.T) {
	picker := Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, ModalActions: true, Description: "Scan complete", OperationDetails: "SCAN DIAGNOSTIC", Options: []Option{{Name: "project", Project: true, ProjectID: "example", Summary: "RESOURCE METADATA"}}}
	for _, size := range [][2]int{{35, 20}, {80, 24}, {120, 35}} {
		m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject, Pickers: map[Screen]Picker{ScreenProject: picker}})
		resized, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = resized.(AppModel)
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "Subshell") || !strings.Contains(view, "[?]") {
			t.Fatal("primary affordances hidden", size, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("layout overflow", size, line)
			}
		}
		m, _ = updateApp(m, textKey("i"))
		if !strings.Contains(m.current().picker.ReadOnlyText, "RESOURCE METADATA") || strings.Contains(m.current().picker.ReadOnlyText, "SCAN DIAGNOSTIC") {
			t.Fatal("resource info mixed with scan results")
		}
		m, _ = updateApp(m, specialKey(tea.KeyEscape))
		m, _ = updateApp(m, textKey("o"))
		m = chooseMenuAction(t, m, "ui:status")
		if m.current().picker.ReadOnlyText != "SCAN DIAGNOSTIC" {
			t.Fatal("missing results")
		}
	}
}

func TestGracefulQuitAcceptsCompletedWorkBeforeExiting(t *testing.T) {
	accepted := false
	picker := Picker{Screen: ScreenProject, ResourceBrowser: true}
	m := NewAppModel(AppOptions{ResourceBrowser: true, StartScreen: ScreenProject, Pickers: map[Screen]Picker{ScreenProject: picker}, AcceptResult: func(any) { accepted = true }})
	next, work := m.startWork("Discovery", func(ctx context.Context, _ func(string)) Transition {
		if ctx.Err() == nil {
			t.Fatal("operation not cancelled")
		}
		return Transition{ReplaceCurrent: true, Picker: picker, Result: "saved"}
	}, Choice{Option: Option{Cancellable: true}}, m.draft, false)
	m = next.(AppModel)
	m, quit := updateApp(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if quit != nil || !m.workStopping {
		t.Fatal("quit skipped completion")
	}
	commands := work().(tea.BatchMsg)
	next, quit = m.Update(commands[1]())
	if !accepted || next.(AppModel).working {
		t.Fatal("results not accepted before quit")
	}
	assertQuit(t, quit)
}

func TestRootCatalogCanCloseWithoutEmptyNavigationStack(t *testing.T) {
	m := NewAppModel(AppOptions{ResourceBrowser: true, StartScreen: ScreenCatalogEntity, Pickers: map[Screen]Picker{ScreenCatalogEntity: {Screen: ScreenCatalogEntity}}})
	m, cmd := updateApp(m, specialKey(tea.KeyEscape))
	assertQuit(t, cmd)
	if m.StackDepth() != 1 {
		t.Fatal("root stack removed")
	}
}

func TestRetryFromRestoredTabUsesThatOperationsContext(t *testing.T) {
	pickers := map[Screen]Picker{}
	for _, screen := range []Screen{ScreenProject, ScreenKubernetes} {
		pickers[screen] = Picker{Screen: screen, ResourceBrowser: true, ModalActions: true, ScopeLabel: "all", OperationDetails: "result", OperationActions: []KeyAction{{Key: "R", Action: "retry"}}, OperationDraft: Draft{"operation-identity": string(screen)}}
	}
	called := false
	m := NewAppModel(AppOptions{ResourceBrowser: true, ComposeSelection: true, StartScreen: ScreenProject, InitialDraft: Draft{ScreenIdentity: "selected"}, Pickers: pickers, Flow: func(c Choice, d Draft) Transition {
		called = true
		if c.Action != "retry" || d["operation-identity"] != "project" || d[ScreenIdentity] != "selected" {
			t.Fatal("retry used another tabs operation", d)
		}
		return Transition{Picker: Picker{Screen: "retry-review"}}
	}})
	m.switchResourceTab(ScreenKubernetes)
	m.switchResourceTab(ScreenProject)
	m, _ = updateApp(m, textKey("R"))
	if !called || m.draft[ScreenIdentity] != "selected" || m.draft["operation-identity"] != "" {
		t.Fatal("operation metadata entered the selection")
	}
}

func TestProjectSelectionKeepsExplicitEligibleIdentity(t *testing.T) {
	picker := Picker{Screen: ScreenProject, ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "project", Project: true, IdentityChoices: []Option{{Name: "chosen"}, {Name: "other"}}}}}
	m := NewAppModel(AppOptions{CanApplyShell: true, ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject, InitialDraft: Draft{ScreenIdentity: "chosen"}, Pickers: map[Screen]Picker{ScreenProject: picker}})
	for range 2 {
		m, _ = updateApp(m, specialKey(tea.KeyEnter))
	}
	if m.CurrentScreen() != ScreenProject || m.draft[ScreenIdentity] != "chosen" || m.draft[ScreenProject] != "project" {
		t.Fatal("reconfirming project reopened identity chooser")
	}
}

func TestBrowserEscapeClearsSelectionsOneLevelAtATime(t *testing.T) {
	picker := Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, ModalActions: true}
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject,
		InitialDraft: Draft{ScreenIdentity: "account", ScreenProject: "project", ScreenKubernetes: "cluster", ScreenDocker: "docker", ScreenWorkspace: "workspace", ScreenWorkspaceSource: "workspace", ScreenShellADCOverride: "true"},
		Pickers:      map[Screen]Picker{ScreenProject: picker}})
	m.stack[len(m.stack)-1].filter = "query"
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if m.current().filter != "" || m.draft[ScreenKubernetes] != "cluster" {
		t.Fatal("first Escape must clear search only")
	}
	screens := []Screen{ScreenKubernetes, ScreenProject, ScreenIdentity, ScreenDocker}
	for i, screen := range screens {
		if m.selectionClearLabel() == "" {
			t.Fatal("missing Escape hint")
		}
		m, _ = updateApp(m, specialKey(tea.KeyEscape))
		if m.draft[screen] != "" {
			t.Fatalf("Escape did not clear %s", screen)
		}
		if i < len(screens)-1 && m.draft[ScreenWorkspaceSource] != "workspace" {
			t.Fatal("lost workspace settings before clearing all components")
		}
		if i == len(screens)-1 && m.draft[ScreenWorkspaceSource] != "" {
			t.Fatal("retained source after last selection cleared")
		}
		for _, remaining := range screens[i+1:] {
			if m.draft[remaining] == "" {
				t.Fatalf("Escape prematurely cleared %s", remaining)
			}
		}
		for _, metadata := range []Screen{ScreenWorkspace, ScreenShellADCOverride} {
			if m.draft[metadata] != "" {
				t.Fatalf("stale selection metadata: %s", metadata)
			}
		}
	}
	m, cmd := updateApp(m, specialKey(tea.KeyEscape))
	if cmd != nil || m.current().screen != ScreenProject || m.hasStagedSelection() {
		t.Fatal("Escape with no selections must stay on the main page")
	}
}
