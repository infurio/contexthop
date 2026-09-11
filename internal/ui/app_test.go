package ui

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestInteractiveProcessResumesIntoPreviousPicker(t *testing.T) {
	start := Picker{Screen: ScreenCatalogEntity, Title: "Catalog", Options: []Option{{Name: "auth", Label: "Authenticate"}}}
	resumed := Picker{Screen: ScreenCatalogEntity, Title: "Catalog", Description: "Authentication complete", Options: []Option{{Name: "cluster"}}}
	model := NewAppModel(AppOptions{
		StartScreen: ScreenCatalogEntity,
		Pickers:     map[Screen]Picker{ScreenCatalogEntity: start},
		Flow: func(Choice, Draft) Transition {
			return Transition{Process: &Process{Command: exec.Command("true"), Done: func(error) Transition {
				return Transition{ReturnToPrevious: true, Picker: resumed}
			}}}
		},
	})
	updated, command := updateApp(model, specialKey(tea.KeyEnter))
	if command == nil || updated.current().picker.Title != "Catalog" {
		t.Fatalf("process start = %#v, %v", updated, command)
	}
	result, _ := updated.Update(processFinishedMessage{process: &Process{Done: func(error) Transition {
		return Transition{ReturnToPrevious: true, Picker: resumed}
	}}})
	finished := result.(AppModel)
	if finished.current().picker.Description != "Authentication complete" || len(finished.stack) != 1 {
		t.Fatalf("resumed model = %#v", finished)
	}
}

func TestDirectStartUsesHomeBackStack(t *testing.T) {
	model := NewAppModel(testAppOptions(ScreenKubernetes, nil))
	if model.StackDepth() != 2 || model.CurrentScreen() != ScreenKubernetes {
		t.Fatalf("initial navigation = depth %d, screen %q", model.StackDepth(), model.CurrentScreen())
	}
	model, command := updateApp(model, specialKey(tea.KeyEscape))
	if command != nil || model.StackDepth() != 1 || model.CurrentScreen() != ScreenWorkspace {
		t.Fatalf("after first escape = depth %d, screen %q, command %v", model.StackDepth(), model.CurrentScreen(), command)
	}
	_, command = updateApp(model, textKey("q"))
	assertQuit(t, command)
}

func TestInputScreenValidatesAndEmitsTextChoice(t *testing.T) {
	input := Picker{
		Screen: ScreenInput, Title: "Add project", Description: "Enter a stable local name.", Dimension: "input",
		Input: &Input{Prompt: "Name", Placeholder: "payments-prod", Validate: func(value string) error {
			if strings.Contains(value, " ") {
				return errors.New("Use letters, numbers, and hyphens only.")
			}
			return nil
		}},
	}
	options := testAppOptions(ScreenWorkspace, func(choice Choice, draft Draft) Transition {
		return Transition{Complete: true}
	})
	options.StartScreen = ScreenInput
	options.Pickers[ScreenInput] = input
	model := NewAppModel(options)
	if model.CurrentScreen() != ScreenInput || model.StackDepth() != 2 {
		t.Fatalf("input start = screen %q, depth %d", model.CurrentScreen(), model.StackDepth())
	}
	initialView := model.View().Content
	for _, expected := range []string{"╭", "▏", "e.g. payments-prod"} {
		if !strings.Contains(initialView, expected) {
			t.Fatalf("input field should contain %q, view %q", expected, initialView)
		}
	}
	if rows := shortcutLines(ansi.Strip(initialView)); len(rows) != 2 {
		t.Fatalf("input shortcut rows = %d, want 2: %#v", len(rows), rows)
	}
	model, command := updateApp(model, specialKey(tea.KeyEnter))
	if command != nil || !strings.Contains(model.View().Content, "A value is required.") {
		t.Fatalf("empty input = command %v, view %q", command, model.View().Content)
	}
	for _, character := range []string{"b", "a", "d", " ", "n", "a", "m", "e"} {
		model, _ = updateApp(model, textKey(character))
	}
	model, command = updateApp(model, specialKey(tea.KeyEnter))
	if command != nil || !strings.Contains(model.View().Content, "letters, numbers, and hyphens") {
		t.Fatalf("invalid input = command %v, view %q", command, model.View().Content)
	}
	for range 5 {
		model, _ = updateApp(model, specialKey(tea.KeyBackspace))
	}
	for _, character := range []string{"-", "n", "a", "m", "e"} {
		model, _ = updateApp(model, textKey(character))
	}
	model, command = updateApp(model, specialKey(tea.KeyEnter))
	assertQuit(t, command)
	if model.outcome.Choice.Screen != ScreenInput || model.outcome.Choice.Option.Name != "bad-name" || model.outcome.Draft[ScreenInput] != "bad-name" {
		t.Fatalf("input outcome = %#v", model.outcome)
	}
}

func TestInputScreenEscRestoresPreviousDraft(t *testing.T) {
	flow := func(choice Choice, draft Draft) Transition {
		return Transition{Picker: Picker{Screen: ScreenInput, Dimension: "input", Input: &Input{Prompt: "Name", Initial: "draft"}}}
	}
	model := NewAppModel(testAppOptions(ScreenProject, flow))
	model, _ = updateApp(model, specialKey(tea.KeyEnter))
	if model.CurrentScreen() != ScreenInput || model.draft[ScreenProject] != "project-one" {
		t.Fatalf("input transition = screen %q, draft %#v", model.CurrentScreen(), model.draft)
	}
	model, _ = updateApp(model, textKey("é"))
	model, command := updateApp(model, specialKey(tea.KeyEscape))
	if command != nil || model.CurrentScreen() != ScreenProject || model.draft[ScreenProject] != "" {
		t.Fatalf("input back = screen %q, draft %#v, command %v", model.CurrentScreen(), model.draft, command)
	}
}

func TestInputFooterIsAnchoredBySharedPageLayout(t *testing.T) {
	options := testAppOptions(ScreenWorkspace, nil)
	options.StartScreen = ScreenInput
	options.ResourceBrowser = true
	options.Pickers[ScreenInput] = Picker{
		Screen: ScreenInput, Title: "Add project", Description: "Enter a stable local name.",
		Dimension: "input", Input: &Input{Prompt: "Name", Placeholder: "example-project"},
	}
	model := NewAppModel(options)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 64, Height: 14})
	model = updated.(AppModel)
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(model.View().Content), "\n"), "\n")
	if len(lines) != 14 {
		t.Fatalf("input rendered %d rows into a 14-row terminal", len(lines))
	}
	if !strings.Contains(lines[12], "[enter] Continue") || !strings.Contains(lines[13], "[esc] Back") {
		t.Fatalf("input footer is not anchored to the final two rows: %#v", lines[12:])
	}
}

func TestTransitionCanReturnToAndRefreshPreviousScreen(t *testing.T) {
	options := testAppOptions(ScreenProject, func(choice Choice, draft Draft) Transition {
		if choice.Screen == ScreenProject {
			return Transition{Picker: Picker{
				Screen: ScreenInput, Title: "Edit name", Dimension: "input",
				Input: &Input{Prompt: "Name"},
			}}
		}
		return Transition{ReturnToPrevious: true, Picker: Picker{
			Screen: ScreenProject, Title: "Updated project", Dimension: "project",
			Options: []Option{{Name: "project-two", Project: true}},
		}}
	})
	model := NewAppModel(options)
	model, _ = updateApp(model, specialKey(tea.KeyEnter))
	for _, character := range []string{"n", "e", "w"} {
		model, _ = updateApp(model, textKey(character))
	}
	model, command := updateApp(model, specialKey(tea.KeyEnter))
	if command != nil || model.StackDepth() != 2 || model.CurrentScreen() != ScreenProject {
		t.Fatalf("return transition = depth %d, screen %q, command %v", model.StackDepth(), model.CurrentScreen(), command)
	}
	if model.current().picker.Title != "Updated project" || model.current().picker.Options[0].Name != "project-two" {
		t.Fatalf("previous picker was not refreshed: %#v", model.current().picker)
	}
}

func TestPickerCanHideIrrelevantSearchField(t *testing.T) {
	options := testAppOptions(ScreenWorkspace, nil)
	options.StartScreen = ScreenConfirm
	options.Pickers[ScreenConfirm] = Picker{
		Screen: ScreenConfirm, Title: "Confirm change", Description: "One catalog change.",
		Dimension: "confirm", HideSearch: true,
		Options: []Option{{Name: "apply", Label: "Apply"}, {Name: "cancel", Label: "Cancel"}},
	}
	model := NewAppModel(options)
	if view := model.View().Content; strings.Contains(view, "Search") || strings.Contains(view, "type to filter") {
		t.Fatalf("hidden search was rendered: %q", view)
	}
	model, _ = updateApp(model, textKey("x"))
	if model.current().filter != "" {
		t.Fatalf("hidden search accepted input: %q", model.current().filter)
	}

	options.Pickers[ScreenConfirm] = Picker{
		Screen: ScreenConfirm, Title: "Change blocked", Description: "Resolve dependencies. Press Esc to go back.",
		Dimension: "confirm", HideSearch: true,
	}
	blocked := NewAppModel(options).View().Content
	if strings.Contains(blocked, "No matching options") || strings.Contains(blocked, "[enter]") || !strings.Contains(blocked, "[esc]") {
		t.Fatalf("blocked picker exposes a fake selection: %q", blocked)
	}
}

func TestPickerRowActionsAndModalSearch(t *testing.T) {
	var selected Choice
	picker := Picker{
		Screen: ScreenCatalogEntity, Title: "Catalog › Identities", Dimension: "catalog",
		DisableEnter: true, ShowSelectedInfo: true, ModalActions: true,
		Options: []Option{{
			Name: "identity:work", Label: "work@example.com", Detail: "No projects",
			Summary: "source:GCP verified:GCP\nMappings: unmapped",
			Actions: []KeyAction{{Key: "m", Label: "Map", Action: "identity-map-project"}, {Key: "ctrl+d", Label: "Delete", Action: "remove-entity"}},
		}},
	}
	model := NewAppModel(AppOptions{
		StartScreen: ScreenCatalogEntity,
		Pickers:     map[Screen]Picker{ScreenCatalogEntity: picker},
		Flow: func(choice Choice, _ Draft) Transition {
			selected = choice
			return Transition{Complete: true}
		},
	})
	view := ansi.Strip(model.View().Content)
	for _, expected := range []string{"source:GCP", "Mappings: unmapped", "[/] Search", "[m] Map", "[ctrl+d] Delete"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("identity list should contain %q: %q", expected, view)
		}
	}
	if strings.Contains(view, "[enter] Select") {
		t.Fatalf("action list exposes redundant detail navigation: %q", view)
	}

	model, command := updateApp(model, textKey("m"))
	assertQuit(t, command)
	if selected.Action != "identity-map-project" || selected.Option.Name != "identity:work" || model.current().filter != "" {
		t.Fatalf("direct action = %#v, filter %q", selected, model.current().filter)
	}

	selected = Choice{}
	model = NewAppModel(AppOptions{
		StartScreen: ScreenCatalogEntity,
		Pickers:     map[Screen]Picker{ScreenCatalogEntity: picker},
		Flow: func(choice Choice, _ Draft) Transition {
			selected = choice
			return Transition{Complete: true}
		},
	})
	model, _ = updateApp(model, textKey("/"))
	model, command = updateApp(model, textKey("m"))
	if command != nil || model.current().filter != "m" || selected.Action != "" {
		t.Fatalf("modal search = filter %q, choice %#v, command %v", model.current().filter, selected, command)
	}
	model, _ = updateApp(model, specialKey(tea.KeyEnter))
	if model.current().searching {
		t.Fatal("enter should leave search mode")
	}
}

func TestPickerCanFocusAnOptionWhenReopened(t *testing.T) {
	picker := Picker{
		Screen: ScreenCatalogEntity, Focus: "identity:second",
		Options: []Option{{Name: "identity:first"}, {Name: "identity:second"}},
	}
	model := NewAppModel(AppOptions{StartScreen: ScreenCatalogEntity, Pickers: map[Screen]Picker{ScreenCatalogEntity: picker}})
	if model.current().cursor != 1 || model.current().cursorKey != "identity:second" {
		t.Fatalf("focused picker = cursor %d, key %q", model.current().cursor, model.current().cursorKey)
	}
}

func TestWorkspaceRootSwitchesTabWithoutQuitting(t *testing.T) {
	model := NewAppModel(testAppOptions(ScreenWorkspace, nil))
	model, command := updateApp(model, textKey("K"))
	if command != nil {
		t.Fatalf("push returned command %v", command)
	}
	if model.StackDepth() != 1 || model.CurrentScreen() != ScreenKubernetes {
		t.Fatalf("navigation = depth %d, screen %q", model.StackDepth(), model.CurrentScreen())
	}
}

func TestResourceBrowserTabsDoNotCommitSelectionAndEnterAdvances(t *testing.T) {
	options := testAppOptions(ScreenIdentity, func(choice Choice, _ Draft) Transition {
		if choice.Screen == ScreenIdentity && choice.Action == "" {
			return Transition{Picker: Picker{
				Screen: ScreenProject, Title: "Projects(identity-one)[1]", Dimension: "project",
				Options: []Option{{Name: "project-one", Project: true}}, ResourceBrowser: true, Scoped: true,
			}}
		}
		return Transition{Complete: true}
	})
	options.ResourceBrowser = true
	for screen, picker := range options.Pickers {
		picker.ResourceBrowser = true
		picker.ModalActions = true
		picker.Title = string(screen) + "(all)[1]"
		options.Pickers[screen] = picker
	}
	options.Pickers[ScreenCatalogEntity] = Picker{Screen: ScreenCatalogEntity, Title: "Hidden[0]"}
	model := NewAppModel(options)
	if model.StackDepth() != 1 || model.CurrentScreen() != ScreenIdentity {
		t.Fatalf("browser root = depth %d screen %q", model.StackDepth(), model.CurrentScreen())
	}

	model, _ = updateApp(model, specialKey(tea.KeyDown))
	model, command := updateApp(model, textKey("P"))
	if command != nil || model.CurrentScreen() != ScreenProject || model.current().picker.Scoped {
		t.Fatalf("project tab = screen %q scoped %t command %v", model.CurrentScreen(), model.current().picker.Scoped, command)
	}
	if model.draft[ScreenIdentity] != "" {
		t.Fatalf("tab navigation committed the highlighted identity: %#v", model.draft)
	}

	model.switchResourceTab(ScreenIdentity)
	model, command = updateApp(model, specialKey(tea.KeyEnter))
	if command != nil || model.CurrentScreen() != ScreenProject || !model.current().picker.Scoped || model.draft[ScreenIdentity] != "identity-two" {
		t.Fatalf("enter did not commit and advance: screen %q scoped %t draft %#v command %v", model.CurrentScreen(), model.current().picker.Scoped, model.draft, command)
	}

	model, command = updateApp(model, specialKey(tea.KeyEscape))
	if command != nil || model.CurrentScreen() != ScreenProject || model.current().picker.Scoped || model.draft[ScreenIdentity] != "" {
		t.Fatalf("unwinding selection = screen %q scoped %t draft %#v command %v", model.CurrentScreen(), model.current().picker.Scoped, model.draft, command)
	}

	model, command = updateApp(model, textKey("D"))
	if command != nil || model.CurrentScreen() != ScreenDocker || model.StackDepth() != 1 {
		t.Fatalf("docker switch = screen %q depth %d command %v", model.CurrentScreen(), model.StackDepth(), command)
	}
	model, command = updateApp(model, specialKey(tea.KeyLeft))
	if command != nil || model.CurrentScreen() != ScreenKubernetes || model.StackDepth() != 1 {
		t.Fatalf("left tab = screen %q depth %d command %v", model.CurrentScreen(), model.StackDepth(), command)
	}
	model, command = updateApp(model, specialKey(tea.KeyRight))
	if command != nil || model.CurrentScreen() != ScreenDocker || model.StackDepth() != 1 {
		t.Fatalf("right tab = screen %q depth %d command %v", model.CurrentScreen(), model.StackDepth(), command)
	}
}

func TestResourceBrowserSeedsAndUnwindsSelectionOneLevelAtATime(t *testing.T) {
	options := testAppOptions(ScreenDocker, nil)
	options.ResourceBrowser = true
	identity := options.Pickers[ScreenIdentity]
	identity.Options[0].IdentityAccount = "person@example.com"
	options.Pickers[ScreenIdentity] = identity
	project := options.Pickers[ScreenProject]
	project.Options[0].ProjectID = "example-project"
	options.Pickers[ScreenProject] = project
	kubernetes := options.Pickers[ScreenKubernetes]
	kubernetes.Options[0].KubernetesContext = "example-cluster"
	options.Pickers[ScreenKubernetes] = kubernetes
	for screen, picker := range options.Pickers {
		picker.ResourceBrowser = true
		picker.ModalActions = true
		options.Pickers[screen] = picker
	}
	options.Snapshot.Observed.Identity = "person@example.com"
	options.Snapshot.Observed.Project = "example-project"
	options.Snapshot.Observed.Kubernetes = "example-cluster"
	options.BrowserPicker = func(screen Screen, draft Draft) Picker {
		picker := options.Pickers[screen]
		picker.Scoped = draft[ScreenIdentity] != "" || draft[ScreenProject] != "" || draft[ScreenKubernetes] != ""
		return picker
	}

	model := NewAppModel(options)
	if model.draft[ScreenIdentity] != "identity-one" || model.draft[ScreenProject] != "project-one" || model.draft[ScreenKubernetes] != "cluster-one" {
		t.Fatalf("observed selection was not seeded: %#v", model.draft)
	}
	for _, step := range []struct {
		cleared Screen
		next    string
	}{
		{ScreenKubernetes, "Clear project"},
		{ScreenProject, "Clear identity"},
		{ScreenIdentity, ""},
	} {
		var command tea.Cmd
		model, command = updateApp(model, specialKey(tea.KeyEscape))
		if command != nil || model.draft[step.cleared] != "" {
			t.Fatalf("clearing %q = draft %#v, command %v", step.cleared, model.draft, command)
		}
		view := ansi.Strip(model.View().Content)
		if step.next != "" && !strings.Contains(view, step.next) {
			t.Fatalf("next unwind action %q missing:\n%s", step.next, view)
		}
	}
	// Once all draft fields are cleared, observed state belongs only to the
	// current-context status, never to the pending selection fields.
	for range 3 {
		model, _ = updateApp(model, specialKey(tea.KeyEscape))
		if model.selectedResourcePath() != "" || model.selectedResourceLabels() != [3]string{} {
			t.Fatalf("Escape restored observed selection: %q", model.selectedResourcePath())
		}
		block := strings.Split(ansi.Strip(model.pickerModel(model.current()).resourceSelection()), "\n")
		if !strings.Contains(strings.Join(block[1:], "\n"), "No changes") {
			t.Fatalf("cleared selection should show the empty state: %#v", block)
		}
		if !strings.Contains(block[0], "k8s: example-cluster") {
			t.Fatalf("current context disappeared: %q", block[0])
		}
	}
}

func TestResourceBrowserReservesLowercaseKeysForNavigation(t *testing.T) {
	var selected Choice
	options := testAppOptions(ScreenKubernetes, func(choice Choice, _ Draft) Transition {
		selected = choice
		return Transition{Complete: true}
	})
	options.ResourceBrowser = true
	for screen, picker := range options.Pickers {
		picker.ResourceBrowser = true
		picker.ModalActions = true
		options.Pickers[screen] = picker
	}
	options.Pickers[ScreenKubernetes] = Picker{
		Screen: ScreenKubernetes, Title: "Kubernetes(all)[1]", Dimension: "kubernetes",
		ResourceBrowser: true, ModalActions: true,
		Options: []Option{{Name: "cluster", Kubernetes: true, Actions: []KeyAction{
			{Key: "H", Label: "Hide", Action: "hide-entity"},
			{Key: "ctrl+d", Label: "Delete", Action: "remove-entity"},
		}}},
	}
	model := NewAppModel(options)
	model, command := updateApp(model, textKey("D"))
	if command != nil || model.CurrentScreen() != ScreenDocker || selected.Action != "" {
		t.Fatalf("lowercase d should navigate: screen %q choice %#v command %v", model.CurrentScreen(), selected, command)
	}

	model = NewAppModel(options)
	model, command = updateApp(model, textKey("H"))
	assertQuit(t, command)
	if selected.Action != "hide-entity" || selected.Option.Name != "cluster" {
		t.Fatalf("uppercase hide = %#v", selected)
	}
	if model.draft[ScreenKubernetes] != "" {
		t.Fatalf("row action changed the selected Kubernetes resource: %#v", model.draft)
	}
}

func TestResourceBrowserChildFlowRendersAsContextualDialog(t *testing.T) {
	options := testAppOptions(ScreenKubernetes, func(choice Choice, _ Draft) Transition {
		if choice.Action == "map-identity" {
			return Transition{Picker: Picker{
				Screen: ScreenDependencyTarget, Title: "Catalog › Kubernetes › cluster-one › Map identity",
				Description: "Choose a compatible option.", Dimension: "catalog-target",
				Options: []Option{{Name: "work", Label: "person@example.com", Detail: "work"}},
			}}
		}
		return Transition{Complete: true}
	})
	options.ResourceBrowser = true
	picker := options.Pickers[ScreenKubernetes]
	picker.Title = "Kubernetes(all)[1]"
	picker.ResourceBrowser = true
	picker.ModalActions = true
	picker.Options[0].KubernetesCluster = "cluster-one"
	picker.Options[0].Actions = []KeyAction{{Key: "m", Label: "Map identity", Action: "map-identity"}}
	options.Pickers[ScreenKubernetes] = picker

	model := NewAppModel(options)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	model = updated.(AppModel)
	model, command := updateApp(model, textKey("m"))
	if command != nil || model.CurrentScreen() != ScreenDependencyTarget {
		t.Fatalf("dialog transition = screen %q, command %v", model.CurrentScreen(), command)
	}
	view := ansi.Strip(model.View().Content)
	for _, expected := range []string{
		"cluster-one", "Map identity", "Kubernetes · cluster-one",
		"person@example.com", "[esc] Back",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("contextual dialog should contain %q:\n%s", expected, view)
		}
	}
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(lines) != 30 {
		t.Fatalf("dialog rendered %d rows into a 30-row terminal", len(lines))
	}
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Fatalf("child flow is not visually bounded as a dialog:\n%s", view)
	}

	model, command = updateApp(model, specialKey(tea.KeyEscape))
	if command != nil || model.CurrentScreen() != ScreenKubernetes || model.StackDepth() != 1 {
		t.Fatalf("dialog back = screen %q, depth %d, command %v", model.CurrentScreen(), model.StackDepth(), command)
	}
}

func TestSelectedResourcePathUsesCommittedDisplayValues(t *testing.T) {
	options := testAppOptions(ScreenKubernetes, nil)
	options.InitialDraft = Draft{
		ScreenIdentity:   "identity-one",
		ScreenProject:    "project-one",
		ScreenKubernetes: "cluster-one",
	}
	identity := options.Pickers[ScreenIdentity]
	identity.Options[0].IdentityAccount = "person@example.com"
	options.Pickers[ScreenIdentity] = identity
	project := options.Pickers[ScreenProject]
	project.Options[0].ProjectID = "example-project"
	options.Pickers[ScreenProject] = project
	kubernetes := options.Pickers[ScreenKubernetes]
	kubernetes.Options[0].KubernetesCluster = "example-cluster"
	options.Pickers[ScreenKubernetes] = kubernetes

	model := NewAppModel(options)
	if got := model.selectedResourcePath(); got != "person@example.com → example-project → example-cluster" {
		t.Fatalf("selected resource path = %q", got)
	}
}

func TestClearingScopeCollapsesDependentBrowserStack(t *testing.T) {
	options := testAppOptions(ScreenIdentity, func(choice Choice, _ Draft) Transition {
		if choice.Screen == ScreenIdentity {
			return Transition{Picker: Picker{
				Screen: ScreenProject, Title: "Projects(identity-one)[1]", Dimension: "project",
				Options: []Option{{Name: "project-one", Project: true}}, ResourceBrowser: true, Scoped: true,
			}}
		}
		return Transition{Complete: true}
	})
	options.ResourceBrowser = true
	for screen, picker := range options.Pickers {
		picker.ResourceBrowser = true
		picker.ModalActions = true
		options.Pickers[screen] = picker
	}
	model := NewAppModel(options)
	model, _ = updateApp(model, specialKey(tea.KeyEnter))
	if model.StackDepth() != 2 || !model.current().picker.Scoped {
		t.Fatalf("dependent browser stack = depth %d scoped %t", model.StackDepth(), model.current().picker.Scoped)
	}
	model, command := updateApp(model, specialKey(tea.KeyEscape))
	if command != nil || model.StackDepth() != 1 || model.CurrentScreen() != ScreenProject || model.current().picker.Scoped {
		t.Fatalf("cleared browser root = depth %d screen %q scoped %t command %v", model.StackDepth(), model.CurrentScreen(), model.current().picker.Scoped, command)
	}
	model, command = updateApp(model, specialKey(tea.KeyEscape))
	if command != nil || model.CurrentScreen() != ScreenProject {
		t.Fatalf("unscoped escape should not reveal an old parent: screen %q command %v", model.CurrentScreen(), command)
	}
}

func TestResourceBrowserSearchBoxAppearsOnlyWhileFiltering(t *testing.T) {
	options := testAppOptions(ScreenKubernetes, nil)
	options.ResourceBrowser = true
	picker := options.Pickers[ScreenKubernetes]
	picker.Title = "Kubernetes(all)[1]"
	picker.ResourceBrowser = true
	picker.ModalActions = true
	options.Pickers[ScreenKubernetes] = picker
	model := NewAppModel(options)
	if view := ansi.Strip(model.View().Content); strings.Contains(view, "type to filter") {
		t.Fatalf("idle browser rendered search input: %q", view)
	}

	model, _ = updateApp(model, textKey("/"))
	if view := ansi.Strip(model.View().Content); !strings.Contains(view, "type to filter") || !strings.Contains(view, "[esc] Keep filter") {
		t.Fatalf("active search input = %q", view)
	}
	model, _ = updateApp(model, textKey("x"))
	if model.current().filter != "x" {
		t.Fatalf("search filter = %q", model.current().filter)
	}
	model, command := updateApp(model, specialKey(tea.KeyEscape))
	if command != nil || model.current().searching || model.current().filter != "x" {
		t.Fatal("first escape did not retain query")
	}
	model, command = updateApp(model, specialKey(tea.KeyEscape))
	if command != nil || model.current().searching || model.current().filter != "" {
		t.Fatalf("cleared search = searching %t filter %q command %v", model.current().searching, model.current().filter, command)
	}
	if view := ansi.Strip(model.View().Content); strings.Contains(view, "type to filter") {
		t.Fatalf("cleared search input remained visible: %q", view)
	}
}

func TestCtrlCQuitsEveryScreen(t *testing.T) {
	for _, screen := range []Screen{
		ScreenWorkspace, ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker,
		ScreenCatalogEntity, ScreenCatalogAction, ScreenDependencyTarget, ScreenPreview, ScreenConfirm,
	} {
		t.Run(string(screen), func(t *testing.T) {
			model := NewAppModel(testAppOptions(screen, nil))
			_, command := updateApp(model, tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
			assertQuit(t, command)
		})
	}
}

func TestCatalogMappingFlowUsesGenericCallbackAndBackStack(t *testing.T) {
	next := map[Screen]Picker{
		ScreenCatalogAction: {
			Screen: ScreenCatalogAction, Title: "Choose action", Dimension: "catalog-action",
			Options: []Option{{Name: "map", Label: "Map dependency", Detail: "Attach an identity"}},
		},
		ScreenDependencyTarget: {
			Screen: ScreenDependencyTarget, Title: "Choose identity", Dimension: "dependency-target",
			Options: []Option{{Name: "person", Label: "person@example.com", Detail: "ready"}},
		},
		ScreenPreview: {
			Screen: ScreenPreview, Title: "Review mapping", Description: "Project example-work will use person@example.com.", Dimension: "preview",
			Options: []Option{{Name: "continue", Label: "Continue", Detail: "Review confirmation"}},
		},
		ScreenConfirm: {
			Screen: ScreenConfirm, Title: "Confirm mapping", Description: "No active terminal context will change.", Dimension: "confirm",
			Options: []Option{{Name: "confirm", Label: "Confirm mapping", Detail: "Write the catalog change"}},
		},
	}
	flow := func(choice Choice, draft Draft) Transition {
		switch choice.Screen {
		case ScreenCatalogEntity:
			return Transition{Picker: next[ScreenCatalogAction]}
		case ScreenCatalogAction:
			return Transition{Picker: next[ScreenDependencyTarget]}
		case ScreenDependencyTarget:
			return Transition{Picker: next[ScreenPreview]}
		case ScreenPreview:
			return Transition{Picker: next[ScreenConfirm]}
		default:
			return Transition{Complete: true}
		}
	}
	options := testAppOptions(ScreenWorkspace, flow)
	options.Pickers[ScreenCatalogEntity] = Picker{
		Screen: ScreenCatalogEntity, Title: "Catalog", Dimension: "catalog-entity",
		Options: []Option{{Name: "project:work", Label: "Work project", Detail: "GCP project example-work"}},
	}
	model := NewAppModel(options)
	model.pushConfigured(ScreenCatalogEntity)
	for _, want := range []Screen{ScreenCatalogAction, ScreenDependencyTarget, ScreenPreview, ScreenConfirm} {
		var command tea.Cmd
		model, command = updateApp(model, specialKey(tea.KeyEnter))
		if command != nil || model.CurrentScreen() != want {
			t.Fatalf("transition to %q = screen %q, command %v", want, model.CurrentScreen(), command)
		}
	}
	if got := model.View().Content; !strings.Contains(got, "No active terminal context will change.") || !strings.Contains(got, "Confirm mapping") {
		t.Fatalf("confirm view = %q", got)
	}
	model, command := updateApp(model, specialKey(tea.KeyEscape))
	if command != nil || model.CurrentScreen() != ScreenPreview || model.draft[ScreenPreview] != "" {
		t.Fatalf("back from confirm = screen %q, draft %#v, command %v", model.CurrentScreen(), model.draft, command)
	}
}

func TestOptionLabelAndDetailDriveDisplayAndFiltering(t *testing.T) {
	picker := Picker{
		Screen: ScreenCatalogAction, Title: "Action", Dimension: "catalog-action",
		Options: []Option{{Name: "opaque-id", Label: "Remove mapping", Detail: "Detach the stale identity"}},
	}
	model := listModel{pickerTitle: picker.Title, dimension: picker.Dimension, options: picker.Options, width: 60}
	view := model.View()
	if !strings.Contains(view.Content, "Remove mapping  Detach the stale identity") || strings.Contains(view.Content, "opaque-id") {
		t.Fatalf("option display = %q", view.Content)
	}
	if got := filteredPickerOptions(picker, "dtch"); len(got) != 1 || got[0].Name != "opaque-id" {
		t.Fatalf("detail filter = %#v", got)
	}
}

func TestCatalogOptionDetailsUseAlignedColumns(t *testing.T) {
	options := []Option{
		{Name: "identity", Label: "Identity", Detail: "add a Google account"},
		{Name: "project", Label: "Cloud project", Detail: "add a cloud project"},
		{Name: "kubernetes", Label: "Kubernetes", Detail: "add a cluster"},
	}
	model := listModel{dimension: "catalog-action", options: options, width: 80}
	wantColumn := -1
	for _, option := range options {
		row := model.compactRow(option)
		column := strings.Index(row, option.Detail)
		if column < 0 {
			t.Fatalf("detail missing from row %q", row)
		}
		if wantColumn < 0 {
			wantColumn = column
		} else if column != wantColumn {
			t.Fatalf("detail columns differ: got %d, want %d in %q", column, wantColumn, row)
		}
	}
}

func TestFlowPreservesFramesAndRestoresDraft(t *testing.T) {
	flow := func(choice Choice, draft Draft) Transition {
		switch choice.Screen {
		case ScreenIdentity:
			return Transition{Picker: Picker{Screen: ScreenProject, Title: "Select project", Dimension: "project", Options: []Option{{Name: "project-one", Project: true}, {Name: "project-two", Project: true}}}}
		case ScreenProject:
			return Transition{Picker: Picker{Screen: ScreenKubernetes, Title: "Select Kubernetes", Dimension: "kubernetes", Options: []Option{{Name: "cluster-one", Kubernetes: true}}}}
		default:
			return Transition{Complete: true}
		}
	}
	model := NewAppModel(testAppOptions(ScreenIdentity, flow))
	model, _ = updateApp(model, textKey("é"))
	model, _ = updateApp(model, specialKey(tea.KeyBackspace))
	model, _ = updateApp(model, specialKey(tea.KeyDown))
	identityFrame := model.stack[1]
	model, command := updateApp(model, specialKey(tea.KeyEnter))
	if command != nil || model.CurrentScreen() != ScreenProject {
		t.Fatalf("identity transition returned %v at %q", command, model.CurrentScreen())
	}
	model, _ = updateApp(model, textKey("t"))
	model, _ = updateApp(model, specialKey(tea.KeyDown))
	projectFrame := model.stack[2]
	model, command = updateApp(model, specialKey(tea.KeyEnter))
	if command != nil || model.CurrentScreen() != ScreenKubernetes {
		t.Fatalf("project transition returned %v at %q", command, model.CurrentScreen())
	}
	if model.draft[ScreenIdentity] != "identity-two" || model.draft[ScreenProject] != "project-two" {
		t.Fatalf("draft = %#v", model.draft)
	}

	model, _ = updateApp(model, specialKey(tea.KeyEscape))
	if model.CurrentScreen() != ScreenProject || model.draft[ScreenProject] != "" {
		t.Fatalf("first back: screen=%q draft=%#v", model.CurrentScreen(), model.draft)
	}
	if model.stack[2].filter != projectFrame.filter || model.stack[2].cursor != projectFrame.cursor {
		t.Fatalf("project frame was not preserved: %#v", model.stack[2])
	}
	model, _ = updateApp(model, specialKey(tea.KeyEscape))
	if model.CurrentScreen() != ScreenIdentity || model.draft[ScreenIdentity] != "" {
		t.Fatalf("second back: screen=%q draft=%#v", model.CurrentScreen(), model.draft)
	}
	if model.stack[1].filter != identityFrame.filter || model.stack[1].cursor != identityFrame.cursor {
		t.Fatalf("identity frame was not preserved: %#v", model.stack[1])
	}
}

func TestTerminalChoiceReturnsTypedOutcome(t *testing.T) {
	model := NewAppModel(testAppOptions(ScreenDocker, func(choice Choice, draft Draft) Transition {
		return Transition{Complete: true}
	}))
	model, command := updateApp(model, specialKey(tea.KeyEnter))
	assertQuit(t, command)
	if !model.outcome.Complete || model.outcome.Choice.Screen != ScreenDocker || model.outcome.Choice.Option.Name != "docker-one" {
		t.Fatalf("outcome = %#v", model.outcome)
	}
	if model.outcome.Draft[ScreenDocker] != "docker-one" {
		t.Fatalf("outcome draft = %#v", model.outcome.Draft)
	}
}

func TestUnicodeFilterAndBackspace(t *testing.T) {
	options := testAppOptions(ScreenKubernetes, nil)
	picker := options.Pickers[ScreenKubernetes]
	picker.Options = []Option{{Name: "café", Kubernetes: true}, {Name: "other", Kubernetes: true}}
	options.Pickers[ScreenKubernetes] = picker
	model := NewAppModel(options)
	for _, character := range []string{"c", "a", "f", "é"} {
		model, _ = updateApp(model, textKey(character))
	}
	frame := model.current()
	if frame.filter != "café" || len(filteredPickerOptions(frame.picker, frame.filter)) != 1 {
		t.Fatalf("filter = %q, matches = %v", frame.filter, filteredPickerOptions(frame.picker, frame.filter))
	}
	model, _ = updateApp(model, specialKey(tea.KeyBackspace))
	if got := model.current().filter; got != "caf" {
		t.Fatalf("filter after backspace = %q", got)
	}
	if !fuzzyMatch("CAFÉ", "cé") {
		t.Fatal("Unicode/case-insensitive fuzzy match failed")
	}
}

func TestWindowSizeAndResponsivePicker(t *testing.T) {
	model := NewAppModel(testAppOptions(ScreenKubernetes, nil))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	model = updated.(AppModel)
	if model.width != 60 || model.height != 10 {
		t.Fatalf("size = %dx%d", model.width, model.height)
	}
	view := model.View()
	if !view.AltScreen {
		t.Fatal("view must retain one alternate-screen lifecycle")
	}
	if strings.Contains(view.Content, "KUBERNETES  PROJECT ID") {
		t.Fatalf("narrow view unexpectedly uses detail table: %q", view.Content)
	}
	if !strings.Contains(view.Content, "[esc]") || !strings.Contains(view.Content, "Back") {
		t.Fatalf("picker footer = %q", view.Content)
	}
}

func testAppOptions(start Screen, flow FlowFunc) AppOptions {
	pickers := map[Screen]Picker{
		ScreenIdentity:   {Screen: ScreenIdentity, Title: "Select identity", Dimension: "identity", Options: []Option{{Name: "identity-one", Identity: true}, {Name: "identity-two", Identity: true}}},
		ScreenProject:    {Screen: ScreenProject, Title: "Select project", Dimension: "project", Options: []Option{{Name: "project-one", Project: true}}},
		ScreenKubernetes: {Screen: ScreenKubernetes, Title: "Select Kubernetes", Dimension: "kubernetes", Options: []Option{{Name: "cluster-one", Kubernetes: true}}},
		ScreenDocker:     {Screen: ScreenDocker, Title: "Select Docker", Dimension: "docker", Options: []Option{{Name: "docker-one", Docker: true}}},
		ScreenWorkspace:  {Screen: ScreenWorkspace, Title: "Select workspace", Dimension: "workspace", Options: []Option{{Name: "workspace-one"}}},
	}
	return AppOptions{StartScreen: start, Pickers: pickers, Flow: flow}
}

func updateApp(model AppModel, message tea.KeyPressMsg) (AppModel, tea.Cmd) {
	updated, command := model.Update(message)
	return updated.(AppModel), command
}

func textKey(value string) tea.KeyPressMsg {
	characters := []rune(value)
	return tea.KeyPressMsg(tea.Key{Text: value, Code: characters[0]})
}

func specialKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }

func assertQuit(t *testing.T, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("command returned %T, want tea.QuitMsg", command())
	}
}

func TestBrowserStartupPreservesSaveNoticeAndFocus(t *testing.T) {
	picker := Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, ModalActions: true,
		Description: "Project saved.", Focus: "second", Options: []Option{{Name: "first", Project: true, ProjectID: "first"}, {Name: "second", Project: true, ProjectID: "second", SaveStatus: "Saved"}}}
	model := NewAppModel(AppOptions{StartScreen: ScreenProject, ResourceBrowser: true, Pickers: map[Screen]Picker{ScreenProject: picker}, BrowserPicker: func(Screen, Draft) Picker {
		refreshed := picker
		refreshed.Description, refreshed.Focus = "", ""
		return refreshed
	}})
	if model.current().cursor != 1 || model.current().picker.Description != "Project saved." {
		t.Fatalf("startup discarded save feedback: %#v", model.current())
	}
}

func TestAuthenticationDiscoveryResetsBrowserStackAndDraft(t *testing.T) {
	model := NewAppModel(AppOptions{StartScreen: ScreenProject, ResourceBrowser: true, Pickers: map[Screen]Picker{ScreenProject: {Screen: ScreenProject, ResourceBrowser: true}}})
	model.push(Picker{Screen: "auth"}, model.draft)
	model.draft[ScreenProject] = "\x00__search_projects__"
	updated, _ := model.applyProcessTransition(Transition{ReplaceCurrent: true, Picker: Picker{Screen: ScreenProject, ResourceBrowser: true}, DraftUpdates: Draft{ScreenIdentity: "work", ScreenProject: ""}})
	model = updated.(AppModel)
	if model.StackDepth() != 1 || model.CurrentScreen() != ScreenProject || model.draft[ScreenIdentity] != "work" || model.draft[ScreenProject] != "" {
		t.Fatalf("authentication retained temporary selection or dialog: %#v", model)
	}
}

func TestSlowFlowShowsProgressAndKeepsEventLoopResponsive(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	model := NewAppModel(AppOptions{AsyncFlow: true, ResourceBrowser: true, StartScreen: ScreenProject,
		Pickers: map[Screen]Picker{ScreenProject: {Screen: ScreenProject, ResourceBrowser: true, Options: []Option{{Name: "discover", Label: "Discover accessible projects"}}}},
		Flow: func(choice Choice, draft Draft) Transition {
			close(started)
			<-release
			return Transition{ReplaceCurrent: true, Picker: Picker{Screen: ScreenProject, ResourceBrowser: true, Title: "Discovery complete"}, DraftUpdates: Draft{ScreenProject: ""}}
		},
	})
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(AppModel)
	if !model.working || !strings.Contains(ansi.Strip(model.View().Content), "Working") || !strings.Contains(model.View().Content, "Discover accessible projects") {
		t.Fatal("progress not visible before request begins")
	}
	batch := command().(tea.BatchMsg)
	finished := make(chan tea.Msg, 1)
	go func() { finished <- batch[1]() }()
	<-started
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(AppModel)
	updated, _ = model.Update(workTick{id: model.workID})
	model = updated.(AppModel)
	if model.width != 80 || model.workFrame != 1 {
		t.Fatal("request blocked resize or animation")
	}
	updated, duplicate := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(AppModel)
	if duplicate != nil {
		t.Fatal("busy Enter launched a duplicate operation")
	}
	if _, quit := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); quit == nil {
		t.Fatal("cannot close during request")
	}
	close(release)
	updated, _ = model.Update(<-finished)
	model = updated.(AppModel)
	if model.working || model.CurrentScreen() != ScreenProject || model.current().picker.Title != "Discovery complete" || model.draft[ScreenProject] != "" {
		t.Fatal("request result did not restore browser")
	}
	updated, staleTick := model.Update(workTick{id: model.workID})
	if updated.(AppModel).working || staleTick != nil {
		t.Fatal("spinner continued after completion")
	}
}

func TestAuthenticationRefreshRunsOutsideEventLoop(t *testing.T) {
	called := false
	model := NewAppModel(AppOptions{AsyncFlow: true})
	updated, command := model.Update(processFinishedMessage{process: &Process{Done: func(error) Transition {
		called = true
		return Transition{ReplaceCurrent: true, Picker: Picker{Screen: ScreenProject, ResourceBrowser: true}}
	}}})
	model = updated.(AppModel)
	if called || !model.working {
		t.Fatal("authentication callback blocked the event loop")
	}
	batch := command().(tea.BatchMsg)
	updated, _ = model.Update(batch[1]())
	model = updated.(AppModel)
	if !called || model.working || model.CurrentScreen() != ScreenProject {
		t.Fatal("authentication refresh did not finish")
	}
}

func TestTabsRememberFilterAndRowWithoutChangingSelection(t *testing.T) {
	options := testAppOptions(ScreenProject, nil)
	options.ResourceBrowser = true
	options.InitialDraft = Draft{ScreenIdentity: "identity-one"}
	for screen, picker := range options.Pickers {
		picker.ResourceBrowser = true
		picker.ModalActions = true
		picker.ScopeLabel = "same scope"
		options.Pickers[screen] = picker
	}
	options.Pickers[ScreenProject] = Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, ModalActions: true, ScopeLabel: "same scope", Options: []Option{
		{Name: "a", Project: true, ProjectID: "match-a"}, {Name: "b", Project: true, ProjectID: "match-b"}, {Name: "c", Project: true, ProjectID: "different"},
	}}
	model := NewAppModel(options)
	model, _ = updateApp(model, textKey("/"))
	updated, _ := model.Update(tea.PasteMsg{Content: "match"})
	model = updated.(AppModel)
	model, _ = updateApp(model, specialKey(tea.KeyDown))
	model, _ = updateApp(model, specialKey(tea.KeyTab))
	if model.CurrentScreen() != ScreenKubernetes {
		t.Fatal("Tab did not switch tabs while searching")
	}
	model, _ = updateApp(model, textKey("P"))
	if model.current().filter != "match" || model.current().cursor != 1 || model.current().cursorKey != "b" {
		t.Fatalf("tab lost position: %#v", model.current())
	}
	if model.draft[ScreenIdentity] != "identity-one" || model.draft[ScreenProject] != "" {
		t.Fatal("tab changed resource selection")
	}
}

func TestClearScopeKeepsTabAndInvalidatesScopedPosition(t *testing.T) {
	options := testAppOptions(ScreenProject, nil)
	options.ResourceBrowser = true
	initial := options.Pickers[ScreenProject]
	initial.ResourceBrowser = true
	options.Pickers[ScreenProject] = initial
	options.InitialDraft = Draft{ScreenIdentity: "identity-one", ScreenProject: "project-one"}
	options.BrowserPicker = func(screen Screen, draft Draft) Picker {
		return Picker{Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true, ScopeLabel: "scope:" + draft[ScreenIdentity], Scoped: draft[ScreenIdentity] != "", Options: []Option{{Name: "project-one", Project: true, ProjectID: "project-one"}}}
	}
	model := NewAppModel(options)
	model, _ = updateApp(model, textKey("D"))
	model.stack[len(model.stack)-1].filter = "old filter"
	model, _ = updateApp(model, textKey("P"))
	model, command := updateApp(model, textKey("c"))
	if command != nil || model.CurrentScreen() != ScreenProject || model.current().picker.Scoped || model.draft[ScreenIdentity] != "" || model.draft[ScreenProject] != "" {
		t.Fatal("clear scope navigated or retained selection")
	}
	model, _ = updateApp(model, textKey("D"))
	if model.current().filter != "" {
		t.Fatal("old scope filter restored")
	}
}

func TestDiscoveryShortcutShowsProgressWithoutRebuildingBrowsers(t *testing.T) {
	for _, project := range []string{"", "project-key"} {
		t.Run("project="+project, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			model := NewAppModel(AppOptions{
				AsyncFlow: true, ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject,
				InitialDraft: Draft{ScreenIdentity: "identity-key", ScreenProject: project},
				Pickers: map[Screen]Picker{
					ScreenIdentity: {Screen: ScreenIdentity, Options: []Option{{Name: "identity-key", IdentityAccount: "person@example.com"}}},
					ScreenProject:  {Screen: ScreenProject, ResourceBrowser: true, Options: []Option{{Name: "project-key", ProjectID: "cloud-project"}}},
				},
				Flow: func(choice Choice, draft Draft) Transition {
					close(started)
					<-release
					return Transition{}
				},
			})
			// Browser construction can be expensive. Neither drawing nor starting
			// discovery may invoke it synchronously on the event loop.
			model.browser = func(Screen, Draft) Picker {
				t.Fatal("rendering invoked browser builder")
				return Picker{}
			}
			model.width, model.height = 100, 30
			_ = model.View()
			updated, command := model.Update(textKey("d"))
			model = updated.(AppModel)
			want := "Preparing discovery options"
			if !model.working || model.workLabel != want || !strings.Contains(ansi.Strip(model.View().Content), "Preparing discovery") {
				t.Fatalf("missing discovery progress: %q", model.View().Content)
			}
			batch := command().(tea.BatchMsg)
			go batch[1]()
			<-started
			updated, deferredAuth := model.Update(identityAuthRefreshed{})
			model = updated.(AppModel)
			if deferredAuth == nil {
				t.Fatal("authentication result was not deferred during discovery")
			}
			updated, _ = model.Update(workTick{id: model.workID})
			model = updated.(AppModel)
			if model.workFrame != 1 {
				t.Fatal("discovery blocked animation")
			}
		})
	}
}

func TestSelectionLabelsFollowAcceptedPickerData(t *testing.T) {
	model := NewAppModel(AppOptions{InitialDraft: Draft{ScreenProject: "new-project"}})
	model.replaceCurrent(Picker{Screen: ScreenProject, ResourceBrowser: true, Options: []Option{{Name: "new-project", ProjectID: "discovered-project"}}})
	model.replaceCurrent(Picker{Screen: ScreenKubernetes, ResourceBrowser: true})
	if got := model.selectedResourceLabels()[1]; got != "discovered-project" {
		t.Fatalf("lost discovered label after navigation: %q", got)
	}
	model.acceptTransition(Transition{Pickers: map[Screen]Picker{ScreenProject: {Options: []Option{{Name: "new-project", ProjectID: "renamed-project"}}}}})
	if got := model.selectedResourceLabels()[1]; got != "renamed-project" {
		t.Fatalf("accepted picker did not refresh label: %q", got)
	}
}

func TestWorkerProgressUpdatesAndStopsAfterCompletion(t *testing.T) {
	model := NewAppModel(AppOptions{})
	started, release := make(chan struct{}), make(chan struct{})
	updated, command := model.startWork("Starting", func(_ context.Context, report func(string)) Transition {
		report("Discovering as a@example.com · 2/4 projects checked")
		close(started)
		<-release
		return Transition{ReplaceCurrent: true, Picker: Picker{Screen: ScreenProject, ResourceBrowser: true}}
	}, Choice{}, nil, false)
	model = updated.(AppModel)
	batch := command().(tea.BatchMsg)
	finished := make(chan tea.Msg, 1)
	go func() { finished <- batch[1]() }()
	<-started
	progress := batch[2]()
	updated, next := model.Update(progress)
	model = updated.(AppModel)
	if !strings.Contains(model.workLabel, "2/4") || next == nil {
		t.Fatal("worker progress not displayed")
	}
	close(release)
	updated, _ = model.Update(<-finished)
	model = updated.(AppModel)
	if next() != nil {
		t.Fatal("progress waiter did not stop")
	}
	updated, next = model.Update(progress)
	if updated.(AppModel).working || next != nil {
		t.Fatal("stale progress resumed completed operation")
	}
}

func TestDiscoveryStopKeepsAppOpenUntilResultsSaved(t *testing.T) {
	started := make(chan struct{})
	model := NewAppModel(AppOptions{AsyncFlow: true, StartScreen: "discovery", Pickers: map[Screen]Picker{"discovery": {Screen: "discovery", CompactDialog: true, Options: []Option{{Name: "start", Cancellable: true}}}}, Flow: func(choice Choice, _ Draft) Transition {
		close(started)
		<-choice.Context.Done()
		return Transition{ReplaceCurrent: true, Picker: Picker{Screen: "results", CompactDialog: true, Title: "Completed results saved"}}
	}})
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(AppModel)
	batch := command().(tea.BatchMsg)
	finished := make(chan tea.Msg, 1)
	go func() { finished <- batch[1]() }()
	<-started
	updated, quit := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(AppModel)
	if quit != nil || !model.working || !model.workStopping {
		t.Fatal("stop exited or discarded running work")
	}
	updated, _ = model.Update(<-finished)
	model = updated.(AppModel)
	if model.working || model.CurrentScreen() != "results" {
		t.Fatal("stopped result not shown")
	}
}

func TestCompactDiscoveryDialogFooterAndText(t *testing.T) {
	for _, size := range [][2]int{{120, 36}, {80, 30}, {55, 24}} {
		model := NewAppModel(AppOptions{StartScreen: ScreenIdentity, ResourceBrowser: true, Pickers: map[Screen]Picker{ScreenIdentity: {Screen: ScreenIdentity, ResourceBrowser: true}}})
		model.width, model.height = size[0], size[1]
		description := "Choose the scope and identity, then start discovery."
		model.push(Picker{Screen: "discover", Title: "Discover", Description: description, CompactDialog: true, HideSearch: true, Options: []Option{{Name: "scope", Label: "Scope: Projects"}, {Name: "start", Label: "Start discovery"}}}, model.draft)
		view := ansi.Strip(model.View().Content)
		if strings.Contains(view, "ctrl+c") {
			t.Fatal("discovery advertises Ctrl+C")
		}
		footerFound := false
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "[enter]") {
				footerFound = strings.Contains(line, "[esc]") && strings.Contains(line, "[↑/↓]")
			}
		}
		if !footerFound {
			t.Fatalf("shortcuts not on one row at %v: %s", size, view)
		}
		if !strings.Contains(view, "then start discovery.") {
			t.Fatalf("description truncated at %v: %s", size, view)
		}
	}
}

func TestResultSnapshotOpensWithoutChangingDraft(t *testing.T) {
	model := NewAppModel(AppOptions{InitialDraft: Draft{ScreenIdentity: "staged"}, Pickers: map[Screen]Picker{"run-results": {Screen: "run-results", DisableEnter: true, Options: []Option{{Name: "discovered"}}}}})
	model.push(Picker{Screen: "summary", Options: []Option{{Name: "view", OpenScreen: "run-results"}}}, model.draft)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(AppModel)
	if model.CurrentScreen() != "run-results" || model.draft[ScreenIdentity] != "staged" {
		t.Fatal("result snapshot altered selection")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if updated.(AppModel).CurrentScreen() != "summary" {
		t.Fatal("results did not return to summary")
	}
}

func TestDismissDiscoveryPreservesBrowserPosition(t *testing.T) {
	model := NewAppModel(AppOptions{StartScreen: ScreenProject, ResourceBrowser: true, Pickers: map[Screen]Picker{ScreenProject: {Screen: ScreenProject, ResourceBrowser: true, Options: []Option{{Name: "a"}, {Name: "b"}}}}, Flow: func(Choice, Draft) Transition { return Transition{Dismiss: true} }})
	model.stack[0].filter = "b"
	model.stack[0].cursorKey = "b"
	model.push(Picker{Screen: "discover", Options: []Option{{Name: "cancel"}}}, model.draft)
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(AppModel)
	if model.StackDepth() != 1 || model.current().filter != "b" || model.current().cursorKey != "b" {
		t.Fatal("cancel reset browser position")
	}
}

func TestAuthenticationContinuationRetainsCompletionAndCancellation(t *testing.T) {
	model := NewAppModel(AppOptions{AsyncFlow: true, InitialDraft: Draft{ScreenIdentity: "work"}})
	var operationContext context.Context
	process := &Process{Cancellable: true, Label: "Resuming discovery", Continue: func(ctx context.Context, report func(string), err error) Transition {
		operationContext = ctx
		report("Checking login")
		return Transition{Complete: true, CompletionChoice: &Choice{Action: "apply-shell"}}
	}}
	updated, command := model.Update(processFinishedMessage{process: process})
	model = updated.(AppModel)
	if operationContext != nil || !model.working || model.workCancel == nil {
		t.Fatal("continuation must run asynchronously and allow cancellation")
	}
	batch := command().(tea.BatchMsg)
	updated, _ = model.Update(batch[1]())
	model = updated.(AppModel)
	if operationContext == nil || !model.outcome.Complete || model.outcome.Choice.Action != "apply-shell" || model.outcome.Draft[ScreenIdentity] != "work" {
		t.Fatal("authentication lost pending launch")
	}
}

func TestDiscoveryReturnsThroughLoginToOriginalBrowserPosition(t *testing.T) {
	picker := Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, Options: []Option{{Name: "one", Project: true}, {Name: "two", Project: true}}}
	model := NewAppModel(AppOptions{StartScreen: ScreenProject, Pickers: map[Screen]Picker{ScreenProject: picker}, InitialDraft: Draft{ScreenIdentity: "work"}})
	model.stack[len(model.stack)-1].filter = "two"
	model.stack[len(model.stack)-1].cursorKey = "two"
	model.push(Picker{Screen: "discover", CompactDialog: true}, model.draft)
	model.push(Picker{Screen: "login", CompactDialog: true}, model.draft)
	picker.Description = "Discovered 1 project."
	updated, _ := model.applyProcessTransition(Transition{ReturnToPrevious: true, PreservePosition: true, ReplaceCurrent: true, Picker: picker})
	model = updated.(AppModel)
	if model.StackDepth() != 1 || model.CurrentScreen() != ScreenProject || model.current().filter != "two" || model.current().cursorKey != "two" || model.draft[ScreenIdentity] != "work" {
		t.Fatalf("discovery lost position: depth=%d screen=%s filter=%q key=%q draft=%v options=%v", model.StackDepth(), model.CurrentScreen(), model.current().filter, model.current().cursorKey, model.draft, model.current().picker.Options)
	}
}
