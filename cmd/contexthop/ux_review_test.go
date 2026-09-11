package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func TestInvalidConfigEditCanBeRecoveredWithoutReplacingOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.New()
	cfg.Docker["old"] = config.Docker{Context: "old"}
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	script := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(script, []byte("printf 'invalid: [\\n' > \"$1\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "sh "+script)
	err := editConfig(path)
	recovery, _ := filepath.Glob(filepath.Join(dir, ".contexthop-edit-*.yaml"))
	after, _ := os.ReadFile(path)
	if err == nil || len(recovery) != 1 || !strings.Contains(err.Error(), recovery[0]) || string(before) != string(after) {
		t.Fatal("invalid edit was lost or original overwritten", err, recovery)
	}
	info, _ := os.Stat(recovery[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal("recovery permissions", info.Mode())
	}
	valid := filepath.Join(dir, "valid.yaml")
	cfg.Docker["new"] = config.Docker{Context: "new"}
	if err := config.Write(valid, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTHOP_TEST_VALID", valid)
	if err := os.WriteFile(script, []byte("cp \"$CONTEXTHOP_TEST_VALID\" \"$1\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := editConfig(path, recovery[0]); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(path)
	if err != nil || saved.Docker["new"].Context != "new" {
		t.Fatal("recovery not applied", err)
	}
}

func TestShellReviewIncludesNamespaceAndIdentityProfile(t *testing.T) {
	cfg := discoveryDialogFixture()
	target := cfg.Kubernetes["k"]
	target.Namespace = "payments"
	target.Risk = "production"
	cfg.Kubernetes["k"] = target
	picker := shellMenuPicker(cfg, ui.Draft{ui.ScreenIdentity: "a", ui.ScreenProject: "p", ui.ScreenKubernetes: "k"}, true)
	fields := map[string]string{}
	for _, field := range picker.ContextFields {
		fields[field.Label] = field.Value
	}
	if fields["Namespace"] != "payments" || fields["Risk"] != "" || !strings.Contains(fields["Identity"], "[a]") {
		t.Fatal("incomplete launch review", fields, picker.Description)
	}
}

func TestWorkspaceSaveReviewsNextShellSettingsBeforePersisting(t *testing.T) {
	cfg := config.New()
	cfg.Identities["a"] = config.Identity{Provider: "gcp", Account: "a@example.com"}
	editor := &catalogEditorState{}
	flow := interactiveFlow(cfg, cfg, editor)
	draft := ui.Draft{ui.ScreenIdentity: "a", ui.ScreenShellADCOverride: "identity"}
	tr := flow(ui.Choice{Action: "save-selection-workspace"}, draft)
	if tr.Picker.Screen != screenSaveWorkspaceName {
		t.Fatal("no name entry")
	}
	tr = flow(ui.Choice{Screen: screenSaveWorkspaceName, Option: ui.Option{Name: "new"}}, draft)
	if tr.Picker.Screen != screenAddWorkspaceBuilder || editor.plan != nil || !strings.Contains(tr.Picker.Description, "ADC setting from Selected") {
		t.Fatal("save skipped settings review")
	}
	if editor.workspace.Value.ADC != "identity" || len(cfg.Destinations) != 0 {
		t.Fatal("preview setting lost or saved before review")
	}
	flow(ui.Choice{Screen: screenAddWorkspaceADC, Option: ui.Option{Name: "identity"}}, draft)
	tr = flow(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-create"}}, draft)
	if tr.Picker.Screen != ui.ScreenConfirm || editor.plan.Config.Destinations["new"].ADC != "identity" || len(cfg.Destinations) != 0 {
		t.Fatal("explicit saved settings not reviewed atomically")
	}
}

func TestWorkspaceActionsDoNotExposeHidingAndDoExposeEditing(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "local"}
	cfg.Destinations["w"] = config.Destination{Docker: "local", Hidden: true}
	if len(catalogCategoryPicker(cfg, "workspaces").Options) != 1 || len(catalogCategoryPicker(cfg, "hidden").Options) != 0 {
		t.Fatal("legacy workspace hiding inconsistent")
	}
	picker := resourceBrowserPicker(cfg, ui.ScreenWorkspace, workspacePickerOptions(cfg), "")
	editor := &catalogEditorState{}
	tr := interactiveFlow(cfg, cfg, editor)(ui.Choice{Screen: ui.ScreenWorkspace, Option: picker.Options[0], Action: "workspace-configure"}, nil)
	if tr.Picker.Screen != screenAddWorkspaceBuilder || editor.workspace.Name != "w" {
		t.Fatal("workspace editor unreachable")
	}
	for _, option := range tr.Picker.Options {
		if option.Name == "workspace-hide" || option.Name == "workspace-save" {
			t.Fatal("unchanged preset offers hide or redundant save")
		}
	}
}

func TestDiscoveryEmptyStateProvidesSetupActions(t *testing.T) {
	cfg := config.New()
	editor := &catalogEditorState{}
	flow := interactiveFlow(cfg, cfg, editor)
	tr := flow(ui.Choice{Screen: ui.ScreenIdentity, Action: "discover-resources"}, nil)
	if tr.Picker.Screen != "discovery-setup" || len(tr.Picker.Options) != 2 {
		t.Fatal("empty state is a dead end")
	}
	tr = flow(ui.Choice{Screen: "discovery-setup", Option: ui.Option{Name: "add-identity"}}, nil)
	if tr.Picker.Screen != screenAddIdentityName || tr.DraftUpdates[ui.ScreenCatalogAction] != "add-identity" {
		t.Fatal("setup action did not enter identity creation")
	}
}

type retryOnlyClient struct{ calls []string }

func (f *retryOnlyClient) RefreshProjects(context.Context, config.Identity) (catalog.ProjectResult, error) {
	return catalog.ProjectResult{}, errors.New("unexpected project enumeration")
}
func (f *retryOnlyClient) RefreshClusters(_ context.Context, _ config.Identity, project string) (catalog.ClusterResult, error) {
	f.calls = append(f.calls, project)
	return catalog.ClusterResult{Freshness: catalog.FreshnessLive}, nil
}
func TestDiscoveryRetryRetainsIdentityAndOnlyScansFailedProjects(t *testing.T) {
	cfg := discoveryDialogFixture()
	draft := ui.Draft{discoverIdentity: "a", discoverScope: "all", discoverOrigin: string(ui.ScreenProject), discoverRetryProjects: "p", ui.ScreenIdentity: "b"}
	tr, ok := discoveryDialogFlow(cfg, cfg, ui.Choice{Action: "retry-discovery"}, draft)
	if !ok || tr.DraftUpdates[discoverScope] != "retry" || discoveryValidation(cfg, draft) != "" {
		t.Fatal("retry scope unavailable")
	}
	fake := &retryOnlyClient{}
	tr = runScopedDiscovery(cfg, cfg.Clone(), draft, nil, fake)
	if !reflect.DeepEqual(fake.calls, []string{"project-id"}) || draft[ui.ScreenIdentity] != "b" || hasResourceUpdates(tr.DraftUpdates) {
		t.Fatal("retry crossed scopes or altered selection", fake.calls)
	}
	if len(tr.Picker.OperationActions) != 0 || tr.DraftUpdates[discoverRetryProjects] != "" {
		t.Fatal("successful retry still advertised")
	}
}

func TestEditingAnotherWorkspaceDoesNotReplaceStagedSelection(t *testing.T) {
	c := workspaceRegressionController(t)
	cfg := c.resources.Saved()
	cfg.Destinations["second"] = config.Destination{Docker: "other"}
	if err := config.Write(c.path, cfg); err != nil {
		t.Fatal(err)
	}
	c.resources.SavedChange(cfg)
	before := ui.Draft{ui.ScreenWorkspace: "work", ui.ScreenWorkspaceSource: "work", ui.ScreenDocker: "local"}
	tr := c.Prepare(ui.Choice{Screen: ui.ScreenWorkspace, Option: ui.Option{Name: "second"}, Action: "workspace-configure"}, before)
	c.Accept(tr.Result)
	c.editor.workspace.Editing = catalog.KindDocker
	tr = c.Prepare(ui.Choice{Screen: screenAddWorkspaceComponent, Option: ui.Option{Name: "local"}}, before)
	c.Accept(tr.Result)
	tr = c.Prepare(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-save"}}, before)
	c.Accept(tr.Result)
	if c.editor.selectionWorkspace != "" {
		t.Fatal("catalog editing selected the workspace")
	}
	if hasResourceUpdates(tr.DraftUpdates) || c.resources.Saved().Destinations["second"].Docker != "other" {
		t.Fatal("editing changed staged or saved state before confirmation")
	}
	tr = c.Prepare(ui.Choice{Screen: ui.ScreenConfirm, Option: ui.Option{Name: "apply"}}, before)
	c.Accept(tr.Result)
	if tr.DraftUpdates[ui.ScreenWorkspace] != "work" || tr.DraftUpdates[ui.ScreenDocker] != "local" || c.resources.Saved().Destinations["second"].Docker != "local" {
		t.Fatal("save replaced another selected workspace", tr.DraftUpdates)
	}
}
