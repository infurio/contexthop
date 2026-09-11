package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestResourceActionsPreserveSelectionAndExposeFullDetails(t *testing.T) {
	picker := Picker{Screen: ScreenProject, ResourceBrowser: true, ModalActions: true, Dimension: "project", OperationDetails: strings.Repeat("full diagnostic\n", 30), Options: []Option{{Name: "project", Project: true, ProjectID: "project-id", Risk: "prod"}, {Name: "\x00__discover__", Label: "Discover"}}}
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject, InitialDraft: Draft{ScreenProject: "project"}, Pickers: map[Screen]Picker{ScreenProject: picker}})
	if len(m.current().picker.Options) != 1 {
		t.Fatal("synthetic action remains in table")
	}
	before := m.draft[ScreenProject]
	m, _ = updateApp(m, textKey("o"))
	m = chooseMenuAction(t, m, "ui:status")
	if m.current().screen != "operation-details" || !strings.Contains(m.current().picker.ReadOnlyText, "full diagnostic") || len(m.current().picker.Options) != 0 {
		t.Fatal("details are not fully scrollable")
	}
	m, _ = updateApp(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.current().screen != ScreenProject || m.draft[ScreenProject] != before {
		t.Fatal("actions changed selection")
	}
	m, _ = updateApp(m, textKey("v"))
	if m.CurrentScreen() != ScreenProject || m.draft[ScreenProject] != before {
		t.Fatal("removed preview key changed browser state")
	}

}

func TestTabReturnRestoresHiddenCursorAndNotice(t *testing.T) {
	pickers := map[Screen]Picker{}
	for _, s := range []Screen{ScreenIdentity, ScreenProject} {
		pickers[s] = Picker{Screen: s, Dimension: string(s), ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "visible", Identity: true, Project: true}, {Name: "hidden", Hidden: true, Identity: true, Project: true}}}
	}
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenIdentity, Pickers: pickers})
	m, _ = updateApp(m, textKey("h"))
	m.stack[0].cursor = 1
	m.stack[0].picker.Description = "Refresh failed"
	m.switchResourceTab(ScreenProject)
	m.switchResourceTab(ScreenIdentity)
	if m.current().cursor != 1 || !m.current().picker.ShowHidden || m.current().picker.Description != "Refresh failed" {
		t.Fatal("tab state was not restored")
	}
}

func TestBrowserKeepsDirectShortcutsBehindOptions(t *testing.T) {
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker, ScreenWorkspace} {
		picker := Picker{Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "item", Identity: true, Project: true, Kubernetes: true, Docker: true, Actions: []KeyAction{{Key: "ctrl+d", Label: "Delete", Action: "remove-entity"}}}}}
		model := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: screen, Pickers: map[Screen]Picker{screen: picker}})
		model, _ = updateApp(model, textKey("a"))
		if model.CurrentScreen() != screen || model.StackDepth() != 1 || model.current().filter != "" {
			t.Fatalf("a changed %s panel", screen)
		}
		resized, _ := model.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
		model = resized.(AppModel)
		view := ansi.Strip(model.View().Content)
		if strings.Contains(view, "[a]") || strings.Contains(view, "Actions") || strings.Contains(view, "[ctrl+d] Delete") || !strings.Contains(view, "[o] Options") {
			t.Fatalf("direct controls missing: %s", view)
		}
		help := model.helpText()
		for _, shortcut := range []string{"ctrl+w", "enter", "ctrl+a", "i  Resource info", "ctrl+d  Delete"} {
			if !strings.Contains(help, shortcut) {
				t.Fatalf("%s help missing %s", screen, shortcut)
			}
		}
		model, _ = updateApp(model, textKey("i"))
		if model.CurrentScreen() != "resource-details" {
			t.Fatalf("i did not open information on %s", screen)
		}
		model, _ = updateApp(model, tea.KeyPressMsg{Code: tea.KeyEscape})
		if model.CurrentScreen() != screen {
			t.Fatal("information changed active panel")
		}
		model, _ = updateApp(model, textKey("?"))
		if !model.helpVisible {
			t.Fatal("help shortcut unavailable")
		}
	}
}

func TestDockerTabUsesUppercaseDAndImportUsesL(t *testing.T) {
	var action string
	pickers := map[Screen]Picker{
		ScreenProject: {Screen: ScreenProject, ResourceBrowser: true, ModalActions: true},
		ScreenDocker:  {Screen: ScreenDocker, ResourceBrowser: true, ModalActions: true},
	}
	model := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject, Pickers: pickers, Flow: func(choice Choice, _ Draft) Transition {
		action = choice.Action
		return Transition{Picker: Picker{Screen: "discovery"}}
	}})
	model, _ = updateApp(model, textKey("g"))
	if action != "" || model.CurrentScreen() != ScreenProject {
		t.Fatal("old discovery key still active")
	}
	model, _ = updateApp(model, textKey("D"))
	if action != "" || model.CurrentScreen() != ScreenDocker {
		t.Fatal("Shift+D did not switch to Docker")
	}
	model, _ = updateApp(model, textKey("d"))
	if action != "" {
		t.Fatal("Docker still imports with d")
	}
	model, _ = updateApp(model, textKey("l"))
	if action != "import-local-contexts" {
		t.Fatal("l did not import local contexts")
	}
}

func TestLabelsKeyOpensEditorOnEveryResourceTab(t *testing.T) {
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker, ScreenWorkspace} {
		picker := Picker{Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "item", Identity: true, Project: true, Kubernetes: true, Docker: true, Actions: []KeyAction{{Key: "t", Label: "Tags", Action: "entity-labels"}}}}}
		called := false
		model := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: screen, Pickers: map[Screen]Picker{screen: picker}, Flow: func(c Choice, _ Draft) Transition {
			called = c.Action == "entity-labels" && c.Option.Name == "item"
			return Transition{Picker: Picker{Screen: "labels-editor", Title: "Labels"}}
		}})
		model, _ = updateApp(model, textKey("t"))
		if !called || model.CurrentScreen() != "labels-editor" {
			t.Fatalf("labels shortcut failed on %s: called=%v current=%s options=%#v", screen, called, model.CurrentScreen(), model.current().picker.Options)
		}
	}
}
