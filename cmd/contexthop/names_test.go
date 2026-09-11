package main

import (
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func TestFriendlyIdentityNamesUseIsolatedDirectory(t *testing.T) {
	dirs := map[string]bool{}
	for _, name := range []string{"Work Account", "../other", "a/b", "a b", "開発", "__Account"} {
		dir := identityCredentialDirectory(name)
		suffix := strings.TrimPrefix(dir, "~/.config/contexthop/gcloud/")
		if suffix == dir || strings.ContainsAny(suffix, "/\\") || !strings.HasPrefix(suffix, "_name-") || dirs[dir] {
			t.Fatalf("unsafe or colliding directory for %q: %q", name, dir)
		}
		dirs[dir] = true
	}
	if got := identityCredentialDirectory("work"); got != "~/.config/contexthop/gcloud/work" {
		t.Fatal(got)
	}
}

func TestFriendlyWorkspaceNameSavesAndRemainsAResource(t *testing.T) {
	name := "__Payments Dev: café / team"
	cfg := config.New()
	cfg.Docker["Local Docker"] = config.Docker{Context: "local"}
	editor := &catalogEditorState{}
	flow := interactiveFlow(cfg, cfg, editor)
	draft := ui.Draft{ui.ScreenDocker: "Local Docker"}
	tr := flow(ui.Choice{Action: "save-selection-workspace"}, draft)
	if err := tr.Picker.Input.Validate(name); err != nil {
		t.Fatal(err)
	}
	tr = flow(ui.Choice{Screen: screenSaveWorkspaceName, Option: ui.Option{Name: name}}, draft)
	tr = flow(ui.Choice{Screen: screenAddWorkspaceBuilder, Option: ui.Option{Name: "workspace-create"}}, draft)
	if tr.Picker.Screen != ui.ScreenConfirm || !editor.plan.Valid() {
		t.Fatalf("cannot save: %#v", tr)
	}
	saved := editor.plan.Config
	picker := resourceBrowserPicker(saved, ui.ScreenWorkspace, workspacePickerOptions(saved), "")
	found := false
	for _, option := range picker.Options {
		if option.Name == name {
			found = true
			if option.Selection[ui.ScreenDocker] != "Local Docker" || len(option.Actions) == 0 {
				t.Fatalf("name treated as an action: %#v", option)
			}
		}
	}
	if !found {
		t.Fatal("workspace missing")
	}
	ref := catalog.Ref{Kind: catalog.KindWorkspace, Name: name}
	got, err := decodeCatalogRef(encodeCatalogRef(ref))
	if err != nil || got != ref {
		t.Fatalf("reference changed: %#v %v", got, err)
	}
	if err := catalogNameValidator(func(existing string) bool { return existing == name })(name); err == nil {
		t.Fatal("duplicate name accepted")
	}
}
