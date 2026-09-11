package main

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/ui"
)

func workspaceRegressionController(t *testing.T) *applicationController {
	t.Helper()
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "local"}
	cfg.Docker["other"] = config.Docker{Context: "other"}
	cfg.Destinations["work"] = config.Destination{Docker: "local"}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	return newApplicationController(path, cfg, recency.History{}, nil)
}

func TestEscapeFromWorkspaceSaveDoesNotAffectLaterDeletion(t *testing.T) {
	for _, review := range []bool{false, true} {
		c := workspaceRegressionController(t)
		draft := ui.Draft{ui.ScreenWorkspace: "work", ui.ScreenWorkspaceSource: "work", ui.ScreenDocker: "local"}
		model := ui.NewAppModel(ui.AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ui.ScreenWorkspace, InitialDraft: draft, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept})
		press := func(key tea.KeyPressMsg) { next, _ := model.Update(key); model = next.(ui.AppModel) }
		press(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
		if review {
			press(tea.KeyPressMsg{Code: tea.KeyEnter})
			press(tea.KeyPressMsg{Code: tea.KeyEscape})
		}
		press(tea.KeyPressMsg{Code: tea.KeyEscape})
		if model.CurrentScreen() != ui.ScreenWorkspace {
			t.Fatal("did not return from save")
		}
		press(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
		if model.CurrentScreen() != ui.ScreenConfirm {
			t.Fatal("delete skipped review")
		}
		press(tea.KeyPressMsg{Code: tea.KeyHome}) // Choose Delete instead of the default Cancel.
		press(tea.KeyPressMsg{Code: tea.KeyEnter})
		if len(c.resources.Saved().Destinations) != 0 {
			t.Fatal("workspace was not deleted")
		}
		press(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
		if model.CurrentScreen() != screenSaveWorkspaceName || c.editor.workspace == nil || c.editor.workspace.Value.Docker != "local" {
			t.Fatalf("deletion lost selected Docker after abandoned save: %#v", c.editor)
		}
	}
}

func TestSaveAsNewBackThenUpdateKeepsOriginalTarget(t *testing.T) {
	c := workspaceRegressionController(t)
	draft := ui.Draft{ui.ScreenWorkspaceSource: "work", ui.ScreenDocker: "other"}
	model := ui.NewAppModel(ui.AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ui.ScreenDocker, InitialDraft: draft, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept})
	press := func(key tea.KeyPressMsg) { next, _ := model.Update(key); model = next.(ui.AppModel) }
	press(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	// Save is focused; the next row offers a copy.
	press(tea.KeyPressMsg{Code: tea.KeyDown})
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.CurrentScreen() != screenSaveWorkspaceName {
		t.Fatal("copy name not opened")
	}
	press(tea.KeyPressMsg{Code: 'c', Text: "c"})
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if c.editor.workspace.Name != "work" || model.CurrentScreen() != screenAddWorkspaceBuilder {
		t.Fatal("cancelled copy changed original target")
	}
	press(tea.KeyPressMsg{Code: tea.KeyUp})
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if c.editor.selectionWorkspace != "work" || model.CurrentScreen() != ui.ScreenConfirm {
		t.Fatal("update lost its target")
	}
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	fresh, err := config.Load(c.path)
	if err != nil || len(fresh.Destinations) != 1 || fresh.Destinations["work"].Docker != "other" {
		t.Fatal("wrong workspace saved", err)
	}
}

func TestMatchingWorkspaceOffersUpdateAndPreservesSettings(t *testing.T) {
	c := workspaceRegressionController(t)
	cfg := c.resources.Saved()
	original := cfg.Destinations["work"]
	original.Risk = "production"
	cfg.Destinations["work"] = original
	draft := ui.Draft{ui.ScreenDocker: "local"}
	editor := &catalogEditorState{}
	flow := interactiveFlow(cfg, cfg, editor)
	tr := flow(ui.Choice{Action: "save-selection-workspace"}, draft)
	if tr.Picker.Screen != screenAddWorkspaceBuilder || editor.workspace.Name != "work" || editor.workspace.Value.Risk != "production" {
		t.Fatalf("matching workspace not recognized: %#v", editor)
	}
	for _, option := range tr.Picker.Options {
		if option.Name == "workspace-save" {
			t.Fatal("unchanged workspace offers redundant save")
		}
	}
	editor.workspace.Value.Docker = "other"
	tr = flow(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-save"}}, draft)
	if tr.Picker.Screen != ui.ScreenConfirm || editor.selectionWorkspace != "work" || !editor.plan.Valid() {
		t.Fatalf("matching update invalid: %#v", editor)
	}
	cfg.Destinations["duplicate"] = original
	editor = &catalogEditorState{}
	flow = interactiveFlow(cfg, cfg, editor)
	tr = flow(ui.Choice{Action: "save-selection-workspace"}, draft)
	if tr.Picker.Screen != screenSaveWorkspaceTarget || len(tr.Picker.Options) != 2 {
		t.Fatalf("ambiguous matches silently chose a target: %#v", tr)
	}
	tr = flow(ui.Choice{Screen: screenSaveWorkspaceTarget, Option: ui.Option{Name: "duplicate"}}, draft)
	if tr.Picker.Screen != screenAddWorkspaceBuilder || editor.workspace.Name != "duplicate" {
		t.Fatal("chosen matching workspace ignored")
	}
	// An explicit source remains the update target while editing its components.
	draft[ui.ScreenWorkspaceSource] = "work"
	tr = flow(ui.Choice{Action: "save-selection-workspace"}, draft)
	if tr.Picker.Screen != screenAddWorkspaceBuilder || editor.workspace.Name != "work" {
		t.Fatal("explicit source lost to ambiguous matching")
	}
}
