package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/ui"
)

func TestSaveBrowserSelectionPinsDependenciesAndPersistsOnce(t *testing.T) {
	cfg := config.New()
	cfg.Identities["account"] = config.Identity{Provider: "gcp", Account: "user@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "project", Identities: []string{"account"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1"}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	c := newApplicationController(path, cfg, recency.History{}, nil)
	draft := ui.Draft{ui.ScreenKubernetes: "cluster"}
	step := func(choice ui.Choice) ui.Transition { tr := c.Prepare(choice, draft); c.Accept(tr.Result); return tr }
	tr := step(ui.Choice{Screen: ui.ScreenKubernetes, Action: "save-selection-workspace"})
	if tr.Picker.Screen != screenSaveWorkspaceName {
		t.Fatalf("name dialog: %#v", tr)
	}
	tr = step(ui.Choice{Screen: screenSaveWorkspaceName, Option: ui.Option{Name: "dev"}})
	if tr.Picker.Screen != screenAddWorkspaceBuilder {
		t.Fatal("missing editor")
	}
	tr = step(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-create"}})
	if tr.Picker.Screen != ui.ScreenConfirm || len(c.resources.Saved().Destinations) != 0 {
		t.Fatal("saved before review")
	}
	tr = step(ui.Choice{Screen: ui.ScreenConfirm, Option: ui.Option{Name: "apply"}})
	fresh, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := fresh.Destinations["dev"]
	if got.Identity != "account" || got.Project != "project" || got.Kubernetes != "cluster" {
		t.Fatalf("dependencies not pinned: %#v", got)
	}
	if !tr.ResetNavigation || tr.DraftUpdates[ui.ScreenWorkspaceSource] != "dev" || tr.DraftUpdates[ui.ScreenIdentity] != "account" {
		t.Fatalf("saved selection not restored: %#v", tr)
	}
	backups, _ := filepath.Glob(path + ".backup-*")
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
}

func TestUpdateSelectionPreservesSettingsAndSaveAsNewLeavesOriginal(t *testing.T) {
	for _, update := range []bool{true, false} {
		cfg := config.New()
		cfg.Identities["account"] = config.Identity{Provider: "gcp", Account: "user@example.com"}
		cfg.Docker["old"] = config.Docker{Context: "old"}
		cfg.Docker["new"] = config.Docker{Context: "new"}
		original := config.Destination{Identity: "account", Docker: "old", ADC: "identity", Risk: "production"}
		cfg.Destinations["work"] = original
		editor := &catalogEditorState{}
		flow := interactiveFlow(cfg, cfg, editor)
		draft := ui.Draft{ui.ScreenWorkspaceSource: "work", ui.ScreenIdentity: "account", ui.ScreenDocker: "new"}
		tr := flow(ui.Choice{Action: "save-selection-workspace"}, draft)
		if tr.Picker.Screen != screenAddWorkspaceBuilder {
			t.Fatal("lost source workspace")
		}
		name := "work"
		if update {
			tr = flow(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-save"}}, draft)
		} else {
			flow(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-copy"}}, draft)
			name = "copy"
			tr = flow(ui.Choice{Screen: screenSaveWorkspaceName, Option: ui.Option{Name: name}}, draft)
			tr = flow(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-create"}}, draft)
		}
		if tr.Picker.Screen != ui.ScreenConfirm || !editor.plan.Valid() {
			t.Fatalf("invalid save: %#v", tr)
		}
		got := editor.plan.Config.Destinations[name]
		if got.Docker != "new" || got.ADC != "identity" || got.Risk != "production" {
			t.Fatalf("settings lost: %#v", got)
		}
		if !reflect.DeepEqual(cfg.Destinations["work"], original) || !update && !reflect.DeepEqual(editor.plan.Config.Destinations["work"], original) {
			t.Fatal("original changed before save / on copy")
		}
	}
}

func TestCancelWorkspaceSelectionSaveKeepsCatalogAndSelection(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "local"}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	c := newApplicationController(path, cfg, recency.History{}, nil)
	draft := ui.Draft{ui.ScreenDocker: "local"}
	for _, choice := range []ui.Choice{{Action: "save-selection-workspace"}, {Screen: screenSaveWorkspaceName, Option: ui.Option{Name: "new"}}, {Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-create"}}} {
		tr := c.Prepare(choice, draft)
		c.Accept(tr.Result)
	}
	tr := c.Prepare(ui.Choice{Screen: ui.ScreenConfirm, Option: ui.Option{Name: "cancel"}}, draft)
	c.Accept(tr.Result)
	fresh, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Destinations) != 0 || !tr.ReturnToPrevious || tr.Picker.Screen != screenAddWorkspaceBuilder || tr.Complete {
		t.Fatalf("cancel saved or lost selection: %#v", tr)
	}
	if c.editor.workspace == nil || c.editor.workspace.Name != "new" || c.editor.workspace.Value.Docker != "local" {
		t.Fatal("cancel lost workspace edits")
	}
	backups, _ := filepath.Glob(path + ".backup-*")
	if len(backups) != 0 {
		t.Fatal("cancel wrote configuration")
	}
}
