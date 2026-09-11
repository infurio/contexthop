package main

import (
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
	"testing"
)

func TestTagEditorCreatesAttachesAndRecolours(t *testing.T) {
	cfg := config.New()
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p"}
	editor := &catalogEditorState{}
	draft := ui.Draft{}
	flow := interactiveFlow(cfg, cfg, editor)
	step := func(screen ui.Screen, name, action string) ui.Transition {
		tr := flow(ui.Choice{Screen: screen, Option: ui.Option{Name: name}, Action: action}, draft)
		for k, v := range tr.DraftUpdates {
			draft[k] = v
		}
		return tr
	}
	step(ui.ScreenProject, "p", "entity-labels")
	step(screenLabels, "\x00add", "")
	tr := step(screenLabelKey, "personal", "")
	if tr.Picker.Screen != screenTagColor {
		t.Fatal(tr)
	}
	tr = step(screenTagColor, "#a78bfa", "")
	if !tr.Complete || !editor.applyImmediately || tr.Picker.Screen == ui.ScreenConfirm || editor.plan.Config.Tags["personal"].Color != "#a78bfa" {
		t.Fatal(tr)
	}
	if got := editor.plan.Config.Projects["p"].TagNames(); len(got) != 1 || got[0] != "personal" {
		t.Fatal(got)
	}
	if len(cfg.Tags) != 0 || len(cfg.Projects["p"].Tags) != 0 {
		t.Fatal("changed source before save")
	}
	cfg = editor.plan.Config
	flow = interactiveFlow(cfg, cfg, editor)
	step(screenLabels, "personal", "")
	step(screenLabelAction, "color", "")
	step(screenTagColor, "#60a5fa", "")
	if editor.plan.Config.Tags["personal"].Color != "#60a5fa" {
		t.Fatal("colour change failed")
	}
	cfg = editor.plan.Config
	flow = interactiveFlow(cfg, cfg, editor)
	step(screenLabels, "personal", "")
	step(screenLabelAction, "remove", "")
	if len(editor.plan.Config.Projects["p"].TagNames()) != 0 || len(editor.plan.Config.Tags) != 1 {
		t.Fatal("remove should keep the reusable definition")
	}
}
func TestLabelsShortcutAvailableForEveryEntity(t *testing.T) {
	cfg := config.New()
	for _, kind := range []catalog.Kind{catalog.KindIdentity, catalog.KindProject, catalog.KindKubernetes, catalog.KindDocker, catalog.KindWorkspace} {
		count := 0
		for _, action := range catalogRowActions(cfg, catalog.Ref{Kind: kind, Name: "x"}) {
			if action.Key == "t" {
				count++
				if action.Action != "entity-labels" {
					t.Fatal(action)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s: %d labels shortcuts", kind, count)
		}
	}
}
func TestDiscoveryImportsLabelsWithoutOverwritingLocal(t *testing.T) {
	cfg := config.New()
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", LabelSet: config.LabelSet{Labels: map[string]string{"env": "local"}}}
	mergeProjectDiscovery(cfg, "", catalog.ProjectResult{Projects: []catalog.Project{{ProjectID: "p", Labels: map[string]string{"env": "google"}}}})
	if cfg.Projects["p"].Effective("")["env"] != "local" || cfg.Projects["p"].GoogleLabels["env"] != "google" {
		t.Fatal(cfg.Projects["p"])
	}
	mergeProjectDiscovery(cfg, "", catalog.ProjectResult{Projects: []catalog.Project{{ProjectID: "p"}}})
	if len(cfg.Projects["p"].GoogleLabels) != 0 || cfg.Projects["p"].Labels["env"] != "local" {
		t.Fatal("refresh lost override or retained deleted cloud label")
	}
}

func TestMappingMenuRetainsAssociationActions(t *testing.T) {
	cfg := config.New()
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Provenance: "manual"}
	cfg.Kubernetes["k"] = config.Kubernetes{Type: "kubeconfig", Context: "k", Project: "p"}
	for _, tc := range []struct {
		screen       ui.Screen
		name, action string
		want         ui.Screen
	}{{ui.ScreenProject, "p", "project-map-identity", screenCatalogProjectMap}, {ui.ScreenKubernetes, "k", "kubernetes-set-project", screenCatalogKubernetesProject}} {
		editor := &catalogEditorState{}
		flow := interactiveFlow(cfg, cfg, editor)
		draft := ui.Draft{}
		tr := flow(ui.Choice{Screen: tc.screen, Option: ui.Option{Name: tc.name}, Action: "entity-map"}, draft)
		found := false
		for _, o := range tr.Picker.Options {
			found = found || o.Name == tc.action
		}
		if !found {
			t.Fatal(tr)
		}
		for k, v := range tr.DraftUpdates {
			draft[k] = v
		}
		tr = flow(ui.Choice{Screen: screenEntityMap, Option: ui.Option{Name: tc.action}}, draft)
		if tr.Picker.Screen != tc.want {
			t.Fatal(tr)
		}
	}
}

func TestTagDeleteRequiresReviewAndListsAssignments(t *testing.T) {
	cfg := config.New()
	cfg.Tags["obsolete"] = config.Tag{Color: "#123456"}
	cfg.Docker["local"] = config.Docker{Context: "local", LabelSet: config.LabelSet{Tags: []string{"obsolete"}}}
	editor := &catalogEditorState{}
	draft := ui.Draft{screenLabelEntity: encodeCatalogRef(catalog.Ref{Kind: catalog.KindDocker, Name: "local"}), screenLabelKey: "obsolete"}
	tr, ok := tagsFlow(cfg, ui.Choice{Screen: screenLabels, Option: ui.Option{Name: "obsolete"}}, draft, editor)
	if !ok {
		t.Fatal("tag flow missing")
	}
	found := false
	for _, row := range tr.Picker.Options {
		found = found || row.Name == "delete"
	}
	if !found {
		t.Fatal("delete action missing")
	}
	tr, ok = tagsFlow(cfg, ui.Choice{Screen: screenLabelAction, Option: ui.Option{Name: "delete"}}, draft, editor)
	if !ok || tr.Picker.Screen != ui.ScreenConfirm || editor.applyImmediately || editor.plan == nil {
		t.Fatal("delete skipped review", tr)
	}
	if len(cfg.Tags) != 1 || len(cfg.Docker["local"].Tags) != 1 {
		t.Fatal("review modified original")
	}
	if len(editor.plan.Config.Tags) != 0 || len(editor.plan.Config.Docker["local"].Tags) != 0 {
		t.Fatal("incomplete deletion plan")
	}
}
