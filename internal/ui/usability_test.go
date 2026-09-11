package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestInputEditingAndPasteDoNotExecuteActions(t *testing.T) {
	options := testAppOptions(ScreenInput, nil)
	options.Pickers[ScreenInput] = Picker{Screen: ScreenInput, Input: &Input{Initial: "a界c"}}
	model := NewAppModel(options)
	model, _ = updateApp(model, specialKey(tea.KeyLeft))
	updated, cmd := model.Update(tea.PasteMsg{Content: "q\n/"})
	model = updated.(AppModel)
	if cmd != nil || model.current().input != "a界q /c" {
		t.Fatalf("paste = %#v", model.current())
	}
	model, _ = updateApp(model, specialKey(tea.KeyBackspace))
	model, _ = updateApp(model, specialKey(tea.KeyDelete))
	if model.current().input != "a界q " {
		t.Fatalf("editing = %q", model.current().input)
	}
	model, _ = updateApp(model, specialKey(tea.KeyHome))
	model, _ = updateApp(model, textKey("z"))
	if model.current().input != "za界q " {
		t.Fatalf("insert at home = %q", model.current().input)
	}
}

func TestBrowserSearchEnterSelectsResult(t *testing.T) {
	picker := Picker{Screen: ScreenKubernetes, Dimension: "kubernetes", ResourceBrowser: true, ModalActions: true,
		Options: []Option{{Name: "alpha", Kubernetes: true}, {Name: "beta", Kubernetes: true}}}
	model := NewAppModel(AppOptions{StartScreen: ScreenKubernetes, Pickers: map[Screen]Picker{ScreenKubernetes: picker}})
	model, _ = updateApp(model, textKey("/"))
	updated, _ := model.Update(tea.PasteMsg{Content: "beta"})
	model = updated.(AppModel)
	model, cmd := updateApp(model, specialKey(tea.KeyEnter))
	if cmd == nil || !model.outcome.Complete || model.outcome.Choice.Option.Name != "beta" {
		t.Fatalf("search outcome = %#v", model.outcome)
	}
}

func TestCompactTabsKeepActiveResourceVisible(t *testing.T) {
	for _, dimension := range []string{"identity", "project", "kubernetes", "docker", "workspace"} {
		view := ansi.Strip(resourcePanelTabs(dimension, 35)[0])
		if !strings.Contains(view, tabFor(Screen(dimension)).key) || lipgloss.Width(view) > 35 {
			t.Fatalf("%s tabs: %q", dimension, view)
		}
	}
	if browserTarget(ScreenWorkspace, "shift+tab") != ScreenDocker || browserTarget(ScreenDocker, "tab") != ScreenWorkspace {
		t.Fatal("tab navigation does not wrap")
	}
}

func TestLongInputKeepsCursorOnOneLine(t *testing.T) {
	for _, offset := range []int{0, 70, 100} {
		view := ansi.Strip(inputTextField(strings.Repeat("界", 100), "", 30, offset))
		lines := strings.Split(view, "\n")
		if len(lines) != 3 || !strings.Contains(lines[1], "▏") {
			t.Fatalf("input offset %d: %q", offset, view)
		}
		for _, line := range lines {
			if lipgloss.Width(line) > 30 {
				t.Fatalf("input exceeds width: %q", line)
			}
		}
	}
}

func TestLongDescriptionCannotHideAllChoices(t *testing.T) {
	model := listModel{width: 80, height: 14, pickerTitle: "Choose a project", pickerDescription: strings.Repeat("Long description. ", 80), hideSearch: true,
		options: []Option{{Name: "selectable-project"}}}
	view := ansi.Strip(model.standardPickerView())
	if !strings.Contains(view, "> selectable-project") {
		t.Fatalf("choice hidden: %s", view)
	}
}

func TestListBoundaryNavigation(t *testing.T) {
	options := testAppOptions(ScreenKubernetes, nil)
	picker := options.Pickers[ScreenKubernetes]
	picker.Options = make([]Option, 50)
	for i := range picker.Options {
		picker.Options[i] = Option{Name: strings.Repeat("x", i+1), Kubernetes: true}
	}
	options.Pickers[ScreenKubernetes] = picker
	model := NewAppModel(options)
	model.height = 20
	model, _ = updateApp(model, specialKey(tea.KeyEnd))
	if model.current().cursor != 49 {
		t.Fatal("end did not reach last row")
	}
	model, _ = updateApp(model, specialKey(tea.KeyPgUp))
	if model.current().cursor != 39 {
		t.Fatal("page up did not move")
	}
	model, _ = updateApp(model, specialKey(tea.KeyHome))
	if model.current().cursor != 0 {
		t.Fatal("home did not reach first row")
	}
}

func TestInputReplacementUsesSharedFlowTransitions(t *testing.T) {
	options := testAppOptions(ScreenInput, func(Choice, Draft) Transition {
		return Transition{ReplaceCurrent: true, DraftUpdates: Draft{ScreenProject: "project"}, Picker: Picker{Screen: ScreenInput, Input: &Input{Initial: "next"}}}
	})
	options.Pickers[ScreenInput] = Picker{Screen: ScreenInput, Input: &Input{Initial: "first"}}
	model := NewAppModel(options)
	depth := model.StackDepth()
	model, _ = updateApp(model, specialKey(tea.KeyHome))
	model, _ = updateApp(model, specialKey(tea.KeyEnter))
	if model.StackDepth() != depth || model.current().input != "next" || model.current().inputOffset != 0 || model.draft[ScreenProject] != "project" {
		t.Fatalf("replacement: %#v", model)
	}
}

func TestBrowserHelpReturnsToFilteredSelection(t *testing.T) {
	picker := Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "project", Project: true}}}
	model := NewAppModel(AppOptions{StartScreen: ScreenProject, Pickers: map[Screen]Picker{ScreenProject: picker}})
	model.stack[len(model.stack)-1].filter = "project"
	model, _ = updateApp(model, textKey("?"))
	if !model.helpVisible || !strings.Contains(ansi.Strip(model.View().Content), "Keyboard shortcuts") {
		t.Fatal("help did not open")
	}
	model, _ = updateApp(model, specialKey(tea.KeyEscape))
	if model.CurrentScreen() != ScreenProject || model.current().filter != "project" {
		t.Fatal("help lost browser state")
	}
}

func TestNarrowSelectionKeepsComponentLabelsVisible(t *testing.T) {
	model := listModel{resourceBrowser: true, width: 80, selection: [3]string{"person@example.com", "example-project", "example-cluster"}}
	view := ansi.Strip(model.resourceSelection())
	for _, value := range []string{"Pending", "id:", "project:", "k8s:"} {
		if !strings.Contains(view, value) {
			t.Fatalf("selection hides %q: %s", value, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > model.contentWidth() {
			t.Fatal("selection exceeds width")
		}
	}
}

func TestActionDialogFitsContentAndHidesParentShortcuts(t *testing.T) {
	picker := Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "search", Project: true}}}
	model := NewAppModel(AppOptions{StartScreen: ScreenProject, Pickers: map[Screen]Picker{ScreenProject: picker}, Flow: func(Choice, Draft) Transition {
		return Transition{Picker: Picker{Screen: Screen("recovery"), Title: "Sign in to search projects", Description: "The isolated login for person@example.com needs authentication.", Dimension: "action", HideSearch: true, EnterLabel: "Continue", Options: []Option{{Name: "auth", Label: "Authenticate and retry"}}}}
	}})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	model = updated.(AppModel)
	model, _ = updateApp(model, specialKey(tea.KeyEnter))
	view := ansi.Strip(model.View().Content)
	for _, unwanted := range []string{"type to filter", "[q] Close", "[←/→]"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("dialog exposes inactive or irrelevant UI %q: %s", unwanted, view)
		}
	}
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	top, bottom := -1, -1
	for index, line := range lines {
		if lipgloss.Width(line) > 120 {
			t.Fatalf("dialog exceeds terminal width: %q", line)
		}
		if strings.Contains(line, "╭") {
			top = index
		}
		if strings.Contains(line, "╰") {
			bottom = index
		}
	}
	if len(lines) != 35 || top < 0 || bottom-top > 12 {
		t.Fatalf("dialog is not sized to the prompt: top=%d bottom=%d rows=%d", top, bottom, len(lines))
	}
	model, _ = updateApp(model, specialKey(tea.KeyEscape))
	if model.CurrentScreen() != ScreenProject {
		t.Fatal("back lost parent screen")
	}
}

func TestMappingDialogSeparatesActionContextFilterAndFooter(t *testing.T) {
	picker := Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, Options: []Option{{Name: "project", Project: true, ProjectID: "team-a-kubernetes"}}}
	model := NewAppModel(AppOptions{StartScreen: ScreenProject, Pickers: map[Screen]Picker{ScreenProject: picker}, Flow: func(Choice, Draft) Transition {
		return Transition{Picker: Picker{Screen: ScreenDependencyTarget, Title: "Catalog › Projects › team-a-kubernetes › Map identity", Description: "Choose an option to review before saving.", Dimension: "catalog-target", Options: []Option{
			{Name: "team-a", Label: "developer@team-a.example.com", Detail: "team-a"},
			{Name: "team-b", Label: "developer@team-b.example.com", Detail: "team-b"},
			{Name: "personal", Label: "developer@example.com", Detail: "personal"},
		}}}
	}})
	model, _ = updateApp(model, specialKey(tea.KeyEnter))
	for _, size := range [][2]int{{80, 24}, {120, 35}, {180, 45}} {
		updated, _ := model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model = updated.(AppModel)
		view := ansi.Strip(model.View().Content)
		dialogStart := strings.Index(view, "Map identity")
		dialogEnd := strings.Index(view, "[esc] Back")
		if dialogStart >= 0 && dialogEnd > dialogStart && strings.Contains(view[dialogStart:dialogEnd], "╭") {
			t.Fatalf("nested search border remains: %s", view)
		}
		for _, want := range []string{"Map identity", "Projects · team-a-kubernetes", "Filter", "[enter] Select", "[↑/↓] Move", "[esc] Back"} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing %q at %v: %s", want, size, view)
			}
		}
		if strings.Contains(view, "Catalog › Projects") {
			t.Fatal("breadcrumb still used as heading")
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size[0] {
				t.Fatal("dialog exceeds terminal width")
			}
		}
	}
}
