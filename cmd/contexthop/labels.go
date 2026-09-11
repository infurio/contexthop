package main

import (
	"fmt"
	"slices"
	"sort"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

const screenLabelEntity ui.Screen = "label-entity-ref"

const screenEntityMap ui.Screen = "entity-map"

const screenLabels ui.Screen = "entity-labels"
const screenLabelKey ui.Screen = "label-key"
const screenLabelValue ui.Screen = "label-value"
const screenLabelAction ui.Screen = "label-action"

func labelsFlow(cfg config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) (ui.Transition, bool) {
	if choice.Screen == screenEntityMap {
		ref, err := decodeCatalogRef(firstNonEmpty(draft[screenLabelEntity], catalogDraftEntity(draft)))
		if err != nil {
			return catalogPreviewError(err, editor), true
		}
		next := ui.Choice{Screen: ui.Screen(ref.Kind), Option: ui.Option{Name: ref.Name}, Action: choice.Option.Name}
		if tr, ok := relationshipAction(cfg, cfg, next, draft, editor); ok {
			return tr, true
		}
		return resourceBrowserActionTransition(cfg, next, draft, editor), true
	}
	action := choice.Action
	if action == "" && (choice.Screen == ui.ScreenCatalogAction || choice.Screen == screenCatalogProject) {
		action = choice.Option.Name
	}
	if action == "entity-labels" || action == "entity-map" {
		ref := catalog.Ref{Kind: catalog.Kind(choice.Screen), Name: choice.Option.Name}
		if parsed, err := decodeCatalogRef(choice.Option.Name); err == nil {
			ref = parsed
		}
		if choice.Action == "" {
			ref, _ = decodeCatalogRef(catalogDraftEntity(draft))
		}
		if action == "entity-map" {
			options := []ui.Option{{Name: "preferred-identity", Label: "Preferred identity"}}
			if ref.Kind == catalog.KindProject && cfg.Projects[ref.Name].Provenance == "manual" {
				options = append(options, ui.Option{Name: "project-map-identity", Label: "Manual association"}, ui.Option{Name: "project-unmap-identity", Label: "Remove manual association"})
			}
			if ref.Kind == catalog.KindKubernetes && cfg.Kubernetes[ref.Name].Type != "gke" {
				options = append(options, ui.Option{Name: "kubernetes-set-project", Label: "Set project"})
				if cfg.Kubernetes[ref.Name].Project != "" {
					options = append(options, ui.Option{Name: "kubernetes-remove-project", Label: "Unmap project"})
				}
			}
			return ui.Transition{Picker: ui.Picker{Screen: screenEntityMap, Title: "Map · " + ref.Name, Options: options}, DraftUpdates: ui.Draft{screenCatalogList: encodeCatalogRef(ref), screenLabelEntity: encodeCatalogRef(ref)}}, true
		}
		return ui.Transition{Picker: labelsPicker(cfg, ref), DraftUpdates: ui.Draft{screenCatalogList: encodeCatalogRef(ref), screenLabelEntity: encodeCatalogRef(ref)}}, true
	}
	return tagsFlow(cfg, choice, draft, editor)
}

const screenTagRename ui.Screen = "tag-rename"

const screenTagColor ui.Screen = "tag-color"
const screenTagCustomColor ui.Screen = "tag-custom-color"
const screenTagOperation ui.Screen = "tag-operation"

func tagsFlow(cfg config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) (ui.Transition, bool) {
	if choice.Screen != screenLabels && choice.Screen != screenLabelKey && choice.Screen != screenLabelAction && choice.Screen != screenTagColor && choice.Screen != screenTagCustomColor && choice.Screen != screenTagRename {
		return ui.Transition{}, false
	}
	ref, err := decodeCatalogRef(firstNonEmpty(draft[screenLabelEntity], catalogDraftEntity(draft)))
	if err != nil {
		return catalogPreviewError(err, editor), true
	}
	name := draft[screenLabelKey]
	if choice.Screen == screenLabels {
		if choice.Option.Name == "\x00add" {
			p := catalogInputPicker(screenLabelKey, "Create tag", "Name", "personal", "A reusable tag for any entity.")
			p.Input.Validate = func(name string) error {
				if err := config.ValidateName(name); err != nil {
					return err
				}
				if _, ok := cfg.Tags[name]; ok {
					return fmt.Errorf("tag already exists; select it from the list")
				}
				return nil
			}
			return ui.Transition{Picker: p}, true
		}
		name = choice.Option.Name
		labels, _ := catalog.EntityLabels(cfg, ref)
		action, label := "assign", "Attach tag"
		if slices.Contains(labels.TagNames(), name) {
			action, label = "remove", "Remove from this entity"
		}
		return ui.Transition{Picker: ui.Picker{Screen: screenLabelAction, Title: "Tag · " + name, Options: []ui.Option{{Name: action, Label: label}, {Name: "rename", Label: "Edit tag name"}, {Name: "color", Label: "Change colour everywhere"}, {Name: "delete", Label: "Delete tag everywhere…"}}}, DraftUpdates: ui.Draft{screenLabelKey: name}}, true
	}

	if choice.Screen == screenLabelAction && choice.Option.Name == "rename" {
		p := catalogInputPicker(screenTagRename, "Edit tag name", "Name", name, "Renames this tag everywhere it is used; its colour stays the same.")
		p.Input.Initial = name
		p.Input.Validate = func(value string) error { _, err := catalog.PlanRenameTag(cfg, name, value); return err }
		return ui.Transition{Picker: p}, true
	}
	if choice.Screen == screenTagRename {
		plan, err := catalog.PlanRenameTag(cfg, name, choice.Option.Name)
		transition := catalogPreviewTransition(plan, err, editor)
		if err == nil && plan.Valid() {
			editor.applyImmediately = true
			transition = ui.Transition{Complete: true}
		}
		return transition, true
	}
	if choice.Screen == screenLabelAction && choice.Option.Name == "delete" {
		plan, err := catalog.PlanDeleteTag(cfg, name)
		return catalogPreviewTransition(plan, err, editor), true
	}
	if choice.Screen == screenLabelKey || choice.Screen == screenLabelAction && choice.Option.Name == "color" {
		operation := "color"
		if choice.Screen == screenLabelKey {
			name = choice.Option.Name
			operation = "assign"
		}
		return ui.Transition{Picker: tagColorPicker(name), DraftUpdates: ui.Draft{screenLabelKey: name, screenTagOperation: operation}}, true
	}
	if choice.Screen == screenTagColor && choice.Option.Name == "custom" {
		p := catalogInputPicker(screenTagCustomColor, "Tag colour · "+name, "Colour", "#a78bfa", "Enter a hex colour in #RRGGBB format.")
		p.Input.Validate = func(v string) error {
			if !config.ValidTagColor(v) {
				return fmt.Errorf("use #RRGGBB, for example #a78bfa")
			}
			return nil
		}
		return ui.Transition{Picker: p}, true
	}
	color, operation := "", choice.Option.Name
	if choice.Screen == screenTagColor || choice.Screen == screenTagCustomColor {
		color = choice.Option.Name
		operation = draft[screenTagOperation]
	}
	plan, err := catalog.PlanTag(cfg, ref, name, color, operation)
	transition := catalogPreviewTransition(plan, err, editor)
	if err == nil && plan.Valid() {
		editor.applyImmediately = true
		transition = ui.Transition{Complete: true}
	}
	return transition, true
}
func labelsPicker(cfg config.Config, ref catalog.Ref) ui.Picker {
	labels, _ := catalog.EntityLabels(cfg, ref)
	assigned := labels.TagNames()
	names := append([]string{}, assigned...)
	for name := range cfg.Tags {
		names = append(names, name)
	}
	sort.Strings(names)
	names = slices.Compact(names)
	options := []ui.Option{}
	for _, name := range names {
		detail := "Available"
		if slices.Contains(assigned, name) {
			detail = "Attached"
		}
		color := cfg.Tags[name].Color
		if color == "" {
			color = config.DefaultTagColor
		}
		options = append(options, ui.Option{Name: name, Label: ui.RenderTags([]ui.Tag{{Name: name, Color: color}}), Detail: detail})
	}
	options = append(options, ui.Option{Name: "\x00add", Label: "+ Create tag"})
	return ui.Picker{Screen: screenLabels, Title: "Tags · " + ref.Name, Description: "Select a tag to attach, detach, rename, recolour or delete it everywhere. Colours are shared across entities.", Options: options}
}
func tagColorPicker(name string) ui.Picker {
	options := []ui.Option{}
	for _, c := range []struct{ name, color string }{{"Grey", "#94a3b8"}, {"Red", "#f87171"}, {"Orange", "#fb923c"}, {"Yellow", "#facc15"}, {"Green", "#4ade80"}, {"Teal", "#2dd4bf"}, {"Blue", "#60a5fa"}, {"Purple", "#a78bfa"}, {"Pink", "#f472b6"}} {
		options = append(options, ui.Option{Name: c.color, Label: ui.RenderTags([]ui.Tag{{Name: c.name, Color: c.color}})})
	}
	options = append(options, ui.Option{Name: "custom", Label: "Custom colour…"})
	return ui.Picker{Screen: screenTagColor, Title: "Tag colour · " + name, Description: "This colour applies everywhere the tag is used.", Options: options}
}
