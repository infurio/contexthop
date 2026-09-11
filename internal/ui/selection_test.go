package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSpaceStagesCombinationAndLaunchKeysAreConsistent(t *testing.T) {
	for _, launch := range []string{"enter", "shift+enter", "ctrl+a"} {
		t.Run(launch, func(t *testing.T) {
			pickers := map[Screen]Picker{}
			for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker, ScreenWorkspace} {
				pickers[screen] = Picker{Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: string(screen) + "-one", Identity: true, Project: true, Kubernetes: true, Docker: true}}}
			}
			calls := 0
			model := NewAppModel(AppOptions{ComposeSelection: true, CanApplyShell: true, ResourceBrowser: true, StartScreen: ScreenIdentity, Pickers: pickers, Flow: func(choice Choice, draft Draft) Transition {
				calls++
				if len(draft) != 4 {
					t.Fatalf("staged combination = %#v", draft)
				}
				want := "apply-shell"
				if launch == "enter" {
					want = "default-shell"
				}
				if choice.Action != want {
					t.Fatalf("action=%s", choice.Action)
				}
				return Transition{Complete: true}
			}})
			for _, key := range []string{"I", "P", "K", "D"} {
				if key == "I" {
					model.switchResourceTab(ScreenIdentity)
				} else {
					model, _ = updateApp(model, textKey(key))
				}
				model, _ = updateApp(model, textKey(" "))
				if calls != 0 {
					t.Fatal("Space launched flow")
				}
				if !strings.Contains(ansi.Strip(model.View().Content), "✓ ") {
					t.Fatal("staged row is not marked")
				}
			}
			model.switchResourceTab(ScreenIdentity)
			var message tea.KeyPressMsg
			switch launch {
			case "enter":
				message = specialKey(tea.KeyEnter)
			case "shift+enter":
				message = tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
			case "ctrl+a":
				message = tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
			}
			_, cmd := updateApp(model, message)
			if calls != 1 || cmd == nil {
				t.Fatal("launch key did not launch staged combination")
			}
		})
	}
}

func TestApplyWithoutShellHookKeepsSelection(t *testing.T) {
	picker := Picker{Screen: ScreenDocker, Dimension: "docker", ResourceBrowser: true, Options: []Option{{Name: "local", Docker: true}}}
	model := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenDocker, Pickers: map[Screen]Picker{ScreenDocker: picker}})
	model, _ = updateApp(model, textKey(" "))
	model, cmd := updateApp(model, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if cmd != nil || model.CurrentScreen() != Screen("shell-integration") || model.draft[ScreenDocker] != "local" {
		t.Fatal("missing integration lost selection or launched shell")
	}
}

func TestHelpTogglesDuringInputAndBackgroundWork(t *testing.T) {
	model := NewAppModel(AppOptions{StartScreen: ScreenInput, Pickers: map[Screen]Picker{ScreenInput: {Screen: ScreenInput, Input: &Input{Initial: "retained"}}}})
	for _, working := range []bool{false, true} {
		model.working = working
		model, _ = updateApp(model, specialKey(tea.KeyF1))
		if !model.helpVisible {
			t.Fatal("help unavailable")
		}
		model, _ = updateApp(model, specialKey(tea.KeyF1))
		if model.helpVisible || model.current().input != "retained" {
			t.Fatal("help lost input state")
		}
	}
}

func TestActionRowsCannotApplyStagedContext(t *testing.T) {
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker, ScreenWorkspace} {
		picker := Picker{Screen: screen, ResourceBrowser: true, Options: []Option{{Name: "setup", OpenScreen: ScreenInput}}}
		m := NewAppModel(AppOptions{ComposeSelection: true, CanApplyShell: true, ResourceBrowser: true, StartScreen: screen, InitialDraft: Draft{ScreenDocker: "docker"}, Pickers: map[Screen]Picker{screen: picker}, Flow: func(Choice, Draft) Transition { t.Fatal("action row applied a context"); return Transition{} }})
		m, cmd := updateApp(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
		if cmd != nil || m.outcome.Complete {
			t.Fatal("applied without launch preview")
		}
	}
}

func TestEnterLaunchesHighlightedResourceAcrossPanelsAndSearch(t *testing.T) {
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker, ScreenWorkspace} {
		for _, searching := range []bool{false, true} {
			picker := Picker{Screen: screen, ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "selected"}}}
			m := NewAppModel(AppOptions{CanApplyShell: true, ComposeSelection: true, ResourceBrowser: true, StartScreen: screen, Pickers: map[Screen]Picker{screen: picker}})
			m.stack[0].searching = searching
			m, cmd := updateApp(m, specialKey(tea.KeyEnter))
			if cmd == nil || !m.outcome.Complete || m.outcome.Choice.Action != "default-shell" || m.outcome.Draft[screen] != "selected" || m.CurrentScreen() != screen {
				t.Fatal("Enter must launch without advancing", screen, m.outcome)
			}
		}
	}
}

func TestSpaceStagesWhileTabNavigates(t *testing.T) {
	pickers := map[Screen]Picker{}
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes} {
		pickers[screen] = Picker{Screen: screen, ResourceBrowser: true, Options: []Option{{Name: string(screen)}}}
	}
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenIdentity, Pickers: pickers})
	for _, screen := range []Screen{ScreenIdentity, ScreenProject} {
		var cmd tea.Cmd
		m, cmd = updateApp(m, textKey(" "))
		if cmd != nil || m.CurrentScreen() != screen || m.draft[screen] != string(screen) {
			t.Fatal("Space moved or launched", screen)
		}
		m, _ = updateApp(m, specialKey(tea.KeyTab))
		if m.draft[screen] != string(screen) {
			t.Fatal("Tab discarded staging")
		}
		// Continue using the updated model in the next iteration.
		if screen == ScreenIdentity {
			if m.CurrentScreen() != ScreenProject {
				t.Fatal(m.CurrentScreen())
			}
		}
	}
}

func TestWorkspaceSourceSurvivesEditsAndClearsWithSelection(t *testing.T) {
	pickers := map[Screen]Picker{
		ScreenWorkspace: {Screen: ScreenWorkspace, ResourceBrowser: true, Options: []Option{{Name: "work", Selection: Draft{ScreenDocker: "old"}}}},
		ScreenDocker:    {Screen: ScreenDocker, ResourceBrowser: true, Options: []Option{{Name: "new", Docker: true}}},
	}
	calls := 0
	model := NewAppModel(AppOptions{ComposeSelection: true, StartScreen: ScreenWorkspace, Pickers: pickers, Flow: func(choice Choice, draft Draft) Transition {
		calls++
		if choice.Action != "save-selection-workspace" || draft[ScreenWorkspaceSource] != "work" || draft[ScreenWorkspace] != "" || draft[ScreenDocker] != "new" {
			t.Fatalf("wrong save selection: %#v %#v", choice, draft)
		}
		return Transition{Picker: Picker{Screen: "save-dialog"}}
	}})
	model, _ = updateApp(model, textKey(" "))
	model, _ = updateApp(model, textKey("D"))
	model, _ = updateApp(model, textKey(" "))
	model, _ = updateApp(model, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if calls != 1 {
		t.Fatal("save shortcut not routed")
	}
	model, _ = updateApp(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model, _ = updateApp(model, textKey("c"))
	if model.draft[ScreenWorkspaceSource] != "" {
		t.Fatal("clear retained source")
	}
}

func TestUnderscoreNameIsASelectableResource(t *testing.T) {
	name := "__create_workspace__"
	picker := Picker{Screen: ScreenWorkspace, ResourceBrowser: true, Options: []Option{{Name: name, Selection: Draft{ScreenDocker: "Local Docker"}}}}
	model := NewAppModel(AppOptions{ComposeSelection: true, StartScreen: ScreenWorkspace, Pickers: map[Screen]Picker{ScreenWorkspace: picker}, Flow: func(Choice, Draft) Transition { t.Fatal("resource triggered an internal action"); return Transition{} }})
	model, _ = updateApp(model, textKey(" "))
	if model.draft[ScreenWorkspace] != name || model.draft[ScreenDocker] != "Local Docker" {
		t.Fatal("friendly workspace not selected")
	}
}

func TestWorkspaceUnselectionClearsEntireCombination(t *testing.T) {
	components := Draft{ScreenIdentity: "account", ScreenProject: "project", ScreenKubernetes: "cluster", ScreenDocker: "docker"}
	browser := func(screen Screen, draft Draft) Picker {
		matches := true
		for component, value := range components {
			if draft[component] != value {
				matches = false
			}
		}
		return Picker{Screen: screen, Dimension: string(screen), ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "work", Selection: cloneDraft(components), MatchesSelection: matches}}}
	}
	for _, explicit := range []bool{true} {
		for _, key := range []tea.KeyPressMsg{textKey(" ")} {
			draft := cloneDraft(components)
			if explicit {
				draft[ScreenWorkspace] = "work"
				draft[ScreenWorkspaceSource] = "work"
			}
			model := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenWorkspace, InitialDraft: draft, Pickers: map[Screen]Picker{ScreenWorkspace: browser(ScreenWorkspace, draft)}, BrowserPicker: browser})
			model, _ = updateApp(model, key)
			for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker, ScreenWorkspace, ScreenWorkspaceSource} {
				if model.draft[screen] != "" {
					t.Fatalf("explicit=%t key=%s retained %s: %#v", explicit, key.String(), screen, model.draft)
				}
			}
			if model.current().picker.Options[0].MatchesSelection {
				t.Fatal("cleared workspace still marked selected")
			}
			model, _ = updateApp(model, textKey(" "))
			for screen, value := range components {
				if model.draft[screen] != value {
					t.Fatal("workspace cannot be reapplied after clearing")
				}
			}
			if model.draft[ScreenWorkspaceSource] != "work" {
				t.Fatal("reselection lost update target")
			}
		}
	}
	// Editing an individual item detaches the selected workspace but retains its
	// source for Update; selecting that workspace again restores the preset.
	draft := cloneDraft(components)
	draft[ScreenDocker] = "other"
	draft[ScreenWorkspaceSource] = "work"
	model := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenWorkspace, InitialDraft: draft, Pickers: map[Screen]Picker{ScreenWorkspace: browser(ScreenWorkspace, draft)}, BrowserPicker: browser})
	model, _ = updateApp(model, textKey(" "))
	if model.draft[ScreenDocker] != "docker" || model.draft[ScreenWorkspace] != "work" {
		t.Fatal("modified combination was cleared instead of applying preset")
	}
}

func TestProjectIdentityChoiceIsAtomicAndCanBeCancelled(t *testing.T) {
	original := Draft{ScreenIdentity: "old", ScreenProject: "old-project", ScreenKubernetes: "old-cluster", ScreenWorkspace: "workspace", ScreenDocker: "docker"}
	project := Option{Name: "new-project", ProjectID: "new-project", IdentityChoices: []Option{{Name: "a"}, {Name: "b"}}}
	model := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject, InitialDraft: original, Pickers: map[Screen]Picker{
		ScreenProject:    {Screen: ScreenProject, ResourceBrowser: true, Options: []Option{project}},
		ScreenKubernetes: {Screen: ScreenKubernetes, ResourceBrowser: true},
	}})
	updated, _ := model.stageResource(project)
	model = updated.(AppModel)
	for key, value := range original {
		if model.draft[key] != value {
			t.Fatal("opening chooser modified selection")
		}
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(AppModel)
	for key, value := range original {
		if model.draft[key] != value {
			t.Fatal("cancel modified selection")
		}
	}
	updated, _ = model.stageResource(project)
	model = updated.(AppModel)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(AppModel)
	if model.draft[ScreenIdentity] != "a" || model.draft[ScreenProject] != "new-project" || model.draft[ScreenKubernetes] != "" || model.draft[ScreenWorkspace] != "" || model.draft[ScreenDocker] != "docker" {
		t.Fatalf("wrong selection: %v", model.draft)
	}
	updated, _ = model.enterResource(project)
	model = updated.(AppModel)
	if model.CurrentScreen() != ScreenProject || model.draft[ScreenIdentity] != "a" {
		t.Fatal("launching project did not retain chosen identity")
	}
}

func TestWorkspaceSpaceTogglesAndEnterLaunches(t *testing.T) {
	p := Picker{Screen: ScreenWorkspace, Dimension: "workspace", ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "work", Selection: Draft{ScreenDocker: "local"}}}}
	m := NewAppModel(AppOptions{CanApplyShell: true, ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenWorkspace, Pickers: map[Screen]Picker{ScreenWorkspace: p}})
	m, _ = updateApp(m, textKey(" "))
	if m.draft[ScreenWorkspace] != "work" || m.outcome.Complete {
		t.Fatal("Space failed to stage")
	}
	m, _ = updateApp(m, textKey(" "))
	if m.draft[ScreenWorkspace] != "" || m.draft[ScreenDocker] != "" {
		t.Fatal("Space failed to clear workspace")
	}
	m, cmd := updateApp(m, specialKey(tea.KeyEnter))
	if cmd == nil || m.outcome.Draft[ScreenWorkspace] != "work" || m.outcome.Draft[ScreenDocker] != "local" {
		t.Fatal("Enter did not launch workspace")
	}
}

func TestResourceIdentityDefaultsAvoidRedundantPicker(t *testing.T) {
	for _, screen := range []Screen{ScreenProject, ScreenKubernetes} {
		for _, scenario := range []string{"sole", "preferred", "current"} {
			option := Option{Name: "target", Project: true, Kubernetes: true, RequiresIdentity: true, IdentityChoices: []Option{{Name: "alice"}}, Selection: Draft{}}
			if screen == ScreenKubernetes {
				option.Selection[ScreenProject] = "project"
			}
			initial := Draft{}
			want := "alice"
			if scenario != "sole" {
				option.IdentityChoices = append(option.IdentityChoices, Option{Name: "bob"})
				option.Selection[ScreenIdentity] = "bob"
				want = "bob"
			}
			if scenario == "current" {
				initial[ScreenIdentity] = "alice"
				want = "alice"
			}
			m := NewAppModel(AppOptions{CanApplyShell: true, StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, InitialDraft: initial,
				Pickers: map[Screen]Picker{screen: {Screen: screen, ResourceBrowser: true, Options: []Option{option}}}})
			m, _ = updateApp(m, specialKey(tea.KeyEnter))
			if m.StackDepth() != 1 || m.draft[ScreenIdentity] != want || m.draft[screen] != "target" {
				t.Fatal(screen, scenario, "unnecessary identity picker or wrong account", m.current(), m.draft)
			}
		}
	}
}
