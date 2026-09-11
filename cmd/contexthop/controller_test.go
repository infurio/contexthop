package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/testenv"
	"github.com/infurio/contexthop/internal/ui"
)

func controllerFixture(t *testing.T) *applicationController {
	t.Helper()
	fixture := testenv.New(t, testenv.Options{})
	dir := fixture.Bin
	if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte("#!/bin/sh\nprintf '%s' '[{\"projectId\":\"new-project\"},{\"projectId\":\"existing-project\"}]'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: dir}
	cfg.Projects["existing-project"] = config.Project{Provider: "gcp", ProjectID: "existing-project"}
	path := filepath.Join(dir, "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	return newApplicationController(path, cfg, recency.History{}, nil)
}

func TestControllerPublishesDiscoveryOnlyWhenAccepted(t *testing.T) {
	c := controllerFixture(t)
	transition := c.Prepare(ui.Choice{Screen: ui.ScreenProject, Option: ui.Option{Name: searchProjectsSelection}}, ui.Draft{ui.ScreenIdentity: "work"})
	if len(c.resources.Selection().Projects) != 1 {
		t.Fatal("worker mutated live application state")
	}
	c.Accept(transition.Result)
	if len(c.resources.Selection().Projects) != 2 || len(c.resources.Saved().Projects) != 1 {
		t.Fatal("discovery was not published as session-only data")
	}
	// Neither the returned result nor a future auth continuation owns accepted maps.
	result := transition.Result.(applicationResult)
	result.resources.SavedChange(config.New())
	if len(c.resources.Saved().Projects) != 1 {
		t.Fatal("accepted result aliases worker state")
	}
}

func TestControllerKeepsSameBrowserAcrossDiscoveryMappingAndSave(t *testing.T) {
	c := controllerFixture(t)
	model := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenIdentity, ResourceBrowser: true, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept})
	press := func(key string) {
		msg := tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
		if key == "home" {
			msg = tea.KeyPressMsg{Code: tea.KeyHome}
		}
		if key == "esc" {
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		}
		if key == "enter" {
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		}
		updated, command := model.Update(msg)
		if command != nil {
			t.Fatalf("%s tried to exit/restart the browser", key)
		}
		model = updated.(ui.AppModel)
	}

	press("enter") // work identity
	press("d")
	press("enter")
	if model.CurrentScreen() != ui.ScreenProject || model.StackDepth() != 1 {
		t.Fatal("discovery left browser")
	}
	press("home")
	press("S") // existing-project: save selected identity mapping
	if model.CurrentScreen() != ui.ScreenProject || model.StackDepth() != 1 {
		t.Fatal("save did not return to same browser")
	}
	if !strings.Contains(model.View().Content, "saved.") {
		t.Fatal("save notice missing")
	}
	if len(c.resources.Selection().Projects) != 2 || len(c.resources.Saved().Projects) != 1 {
		t.Fatal("mapping lost or persisted unrelated discovery")
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(ui.AppModel)
	press("S") // new-project
	fresh, err := config.Load(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Projects) != 2 || mappedIdentityForProject(fresh, "existing-project", "work") == "" || mappedIdentityForProject(fresh, "new-project", "work") == "" {
		t.Fatal("same-session saves did not persist")
	}
}

func TestControllerSaveFailureDoesNotPublishSuccess(t *testing.T) {
	c := controllerFixture(t)
	c.apply = func(string, catalog.Plan) error { return errors.New("disk unavailable") }
	transition := c.Prepare(ui.Choice{Screen: ui.ScreenProject, Action: "save-project-mapping", Option: ui.Option{Name: "existing-project"}}, ui.Draft{ui.ScreenIdentity: "work"})
	c.Accept(transition.Result)
	if transition.Complete || !strings.Contains(transition.Picker.Description, "disk unavailable") {
		t.Fatal("save failure did not remain in UI")
	}
	if mappedIdentityForProject(c.resources.Saved(), "existing-project", "work") != "" {
		t.Fatal("failed save changed saved state")
	}
}

func TestControllerCancelReviewDoesNotExitOrSave(t *testing.T) {
	c := controllerFixture(t)
	draft := ui.Draft{screenCatalogList: encodeCatalogRef(catalog.Ref{Kind: catalog.KindProject, Name: "existing-project"})}
	transition := c.Prepare(ui.Choice{Screen: ui.ScreenProject, Action: "remove-entity", Option: ui.Option{Name: "existing-project"}}, draft)
	c.Accept(transition.Result)
	if transition.Picker.Screen != ui.ScreenConfirm {
		t.Fatal("delete skipped review")
	}
	transition = c.Prepare(ui.Choice{Screen: ui.ScreenConfirm, Option: ui.Option{Name: "cancel"}}, draft)
	c.Accept(transition.Result)
	if transition.Complete || !transition.ResetNavigation {
		t.Fatal("cancel closed application")
	}
	if len(c.resources.Saved().Projects) != 1 {
		t.Fatal("cancel saved the deletion")
	}
}

func TestBackgroundSavePublishesOnUIEventLoop(t *testing.T) {
	c := controllerFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	originalApply := c.apply
	c.apply = func(path string, plan catalog.Plan) error {
		close(started)
		<-release
		return originalApply(path, plan)
	}
	// Simulate completed discovery through the same controller boundary.
	discovery := c.Prepare(ui.Choice{Screen: ui.ScreenProject, Option: ui.Option{Name: searchProjectsSelection}}, ui.Draft{ui.ScreenIdentity: "work"})
	c.Accept(discovery.Result)
	model := ui.NewAppModel(ui.AppOptions{AsyncFlow: true, StartScreen: ui.ScreenProject, ResourceBrowser: true,
		InitialDraft: ui.Draft{ui.ScreenIdentity: "work"}, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept})
	updated, command := model.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	model = updated.(ui.AppModel)
	batch := command().(tea.BatchMsg)
	finished := make(chan tea.Msg, 1)
	go func() { finished <- batch[1]() }()
	<-started
	if !strings.Contains(model.View().Content, "Working") {
		t.Fatal("save progress missing")
	}
	for range 10 {
		_ = c.Browser(ui.ScreenProject, ui.Draft{ui.ScreenIdentity: "work"})
		if mappedIdentityForProject(c.resources.Saved(), "existing-project", "work") != "" {
			t.Fatal("worker published state before UI acceptance")
		}
	}
	close(release)
	result := <-finished
	if mappedIdentityForProject(c.resources.Saved(), "existing-project", "work") != "" {
		t.Fatal("background completion mutated UI-owned state")
	}
	updated, command = model.Update(result)
	model = updated.(ui.AppModel)
	if command != nil || model.StackDepth() != 1 || mappedIdentityForProject(c.resources.Saved(), "existing-project", "work") == "" {
		t.Fatal("result did not update existing browser")
	}
}

func TestControllerRejectsStaleSaveWithoutOverwritingExternalEdit(t *testing.T) {
	c := controllerFixture(t)
	fresh := c.resources.Saved()
	fresh.Docker["external"] = config.Docker{Context: "external"}
	if err := config.Write(c.path, fresh); err != nil {
		t.Fatal(err)
	}
	transition := c.Prepare(ui.Choice{Screen: ui.ScreenProject, Action: "save-project-mapping", Option: ui.Option{Name: "existing-project"}}, ui.Draft{ui.ScreenIdentity: "work"})
	c.Accept(transition.Result)
	if transition.Complete || !strings.Contains(transition.Picker.Description, "stale") {
		t.Fatal("stale save was not blocked")
	}
	after, err := config.Load(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := after.Docker["external"]; !exists {
		t.Fatal("external edit overwritten")
	}
	if mappedIdentityForProject(after, "existing-project", "work") != "" {
		t.Fatal("stale mapping persisted")
	}
}

func TestControllerDeleteDoesNotResurrectDiscoveredSavedProject(t *testing.T) {
	c := controllerFixture(t)
	draft := ui.Draft{ui.ScreenIdentity: "work"}
	discovered := c.Prepare(ui.Choice{Screen: ui.ScreenProject, Option: ui.Option{Name: searchProjectsSelection}}, draft)
	c.Accept(discovered.Result)
	remove := c.Prepare(ui.Choice{Screen: ui.ScreenProject, Action: "remove-entity", Option: ui.Option{Name: "existing-project"}}, draft)
	c.Accept(remove.Result)
	for screen, value := range remove.DraftUpdates {
		draft[screen] = value
	}
	applied := c.Prepare(ui.Choice{Screen: ui.ScreenConfirm, Option: ui.Option{Name: "apply"}}, draft)
	c.Accept(applied.Result)
	if applied.Complete || !applied.ResetNavigation {
		t.Fatal("delete restarted UI")
	}
	if _, exists := c.resources.Selection().Projects["existing-project"]; exists {
		t.Fatal("old discovery restored deleted project")
	}
	if _, exists := c.resources.Selection().Projects["new-project"]; !exists {
		t.Fatal("delete discarded unrelated discovery")
	}
}

func TestAuthContinuationPublishesAnIndependentSnapshot(t *testing.T) {
	c := controllerFixture(t)
	resources := c.resources.Clone()
	view := resources.Selection()
	editor := catalogEditorState{}
	transition := c.finish(resources, view, &editor, ui.Transition{Process: &ui.Process{Done: func(error) ui.Transition {
		view.Projects["from-auth"] = config.Project{Provider: "gcp", ProjectID: "from-auth", Identities: []string{"work"}}
		return discoveredProjectsTransition(view, resources.Saved(), "work", []ui.Option{{Name: "from-auth"}}, nil)
	}}}, ui.Choice{Screen: screenProviderAuth}, ui.Draft{ui.ScreenIdentity: "work"})
	c.Accept(transition.Result)
	result := transition.Process.Done(nil)
	if _, exists := c.resources.Selection().Projects["from-auth"]; exists {
		t.Fatal("auth worker mutated accepted snapshot")
	}
	c.Accept(result.Result)
	if _, exists := c.resources.Selection().Projects["from-auth"]; !exists {
		t.Fatal("auth result was lost")
	}
}

func TestBrowserDiscoveryAutomaticallySavesProjectsAndClusters(t *testing.T) {
	c := controllerFixture(t)
	c.autoSaveDiscovery = true
	transition := c.Prepare(ui.Choice{Screen: ui.ScreenProject, Option: ui.Option{Name: searchProjectsSelection}}, ui.Draft{ui.ScreenIdentity: "work"})
	c.Accept(transition.Result)
	fresh, err := config.Load(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Projects) != 2 || len(fresh.Projects["existing-project"].Identities) != 1 {
		t.Fatal("discovered projects or mappings were not saved")
	}
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte("#!/bin/sh\nprintf '%s' '[{\"name\":\"cluster-a\",\"location\":\"us-east1\",\"status\":\"RUNNING\"}]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	transition = c.Prepare(ui.Choice{Screen: screenDiscover, Option: ui.Option{Name: "start"}}, ui.Draft{discoverIdentity: "work", discoverProject: "new-project", discoverScope: "clusters", discoverOrigin: string(ui.ScreenKubernetes)})
	if transition.Result == nil {
		t.Fatalf("cluster discovery failed: %#v", transition)
	}
	c.Accept(transition.Result)
	fresh, err = config.Load(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Kubernetes) != 1 {
		t.Fatalf("clusters not saved: %#v", transition.Picker)
	}
	for _, target := range fresh.Kubernetes {
		if target.Project != "new-project" || target.Provenance != "gcp" || target.Cluster != "cluster-a" {
			t.Fatal("cluster metadata missing")
		}
	}
}

func TestBrowserDeleteRequiresConfirmationAndIsBackedUp(t *testing.T) {
	c := controllerFixture(t)
	removed := c.Prepare(ui.Choice{Screen: ui.ScreenProject, Action: "remove-entity", Option: ui.Option{Name: "existing-project"}}, ui.Draft{})
	c.Accept(removed.Result)
	if _, ok := c.resources.Saved().Projects["existing-project"]; !ok || removed.Picker.Screen != ui.ScreenConfirm || removed.Picker.Focus != "cancel" {
		t.Fatal("delete did not wait for explicit confirmation")
	}
	approved := c.Prepare(ui.Choice{Screen: ui.ScreenConfirm, Option: ui.Option{Name: "apply"}}, ui.Draft{})
	c.Accept(approved.Result)
	if _, ok := c.resources.Saved().Projects["existing-project"]; ok {
		t.Fatal("confirmed deletion was not saved")
	}
	backups, err := filepath.Glob(c.path + ".backup-*")
	if err != nil || len(backups) == 0 {
		t.Fatalf("configuration backup missing: %v", err)
	}
	prior, err := config.Load(backups[len(backups)-1])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := prior.Projects["existing-project"]; !ok {
		t.Fatal("backup did not preserve deleted entity")
	}

}

func TestTagChangesSaveImmediately(t *testing.T) {
	c := controllerFixture(t)
	change := func(kind catalog.Kind, entity, operation, color string) {
		t.Helper()
		screen, name := screenLabelAction, operation
		if color != "" {
			screen, name = screenTagColor, color
		}
		tr := c.Prepare(ui.Choice{Screen: screen, Option: ui.Option{Name: name}}, ui.Draft{screenLabelEntity: encodeCatalogRef(catalog.Ref{Kind: kind, Name: entity}), screenLabelKey: "personal", screenTagOperation: operation})
		if tr.Picker.Screen == ui.ScreenConfirm || !tr.Picker.ResourceBrowser {
			t.Fatalf("tag change did not return directly to browser: %#v", tr)
		}
		c.Accept(tr.Result)
	}
	change(catalog.KindProject, "existing-project", "assign", "#a78bfa")
	saved, err := config.Load(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tags["personal"].Color != "#a78bfa" || len(saved.Projects["existing-project"].Tags) != 1 {
		t.Fatal("create was not persisted")
	}
	change(catalog.KindIdentity, "work", "assign", "")
	change(catalog.KindProject, "existing-project", "color", "#60a5fa")
	change(catalog.KindProject, "existing-project", "remove", "")
	saved, err = config.Load(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tags["personal"].Color != "#60a5fa" || len(saved.Projects["existing-project"].Tags) != 0 || len(saved.Identities["work"].Tags) != 1 {
		t.Fatal("tag mutations were not persisted correctly")
	}

}

func TestImmediateTagWriteFailurePreservesSavedState(t *testing.T) {
	c := controllerFixture(t)
	c.apply = func(string, catalog.Plan) error { return errors.New("tag write failed") }
	tr := c.Prepare(ui.Choice{Screen: screenTagColor, Option: ui.Option{Name: "#a78bfa"}}, ui.Draft{screenLabelEntity: encodeCatalogRef(catalog.Ref{Kind: catalog.KindProject, Name: "existing-project"}), screenLabelKey: "personal", screenTagOperation: "assign"})
	c.Accept(tr.Result)
	if c.editor.err == nil || !strings.Contains(c.editor.err.Error(), "tag write failed") {
		t.Fatal("write error was not surfaced")
	}
	saved, err := config.Load(c.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Tags) != 0 || len(c.resources.Saved().Tags) != 0 {
		t.Fatal("failed tag write changed state")
	}
}
