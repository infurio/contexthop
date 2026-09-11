package main

import (
	"strings"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func catalogActionTransition(cfg config.Config, action string, draft ui.Draft, editor *catalogEditorState) ui.Transition {
	entity := catalogDraftEntity(draft)
	if action == "kubernetes-map" || action == "unmap-resource" {
		picker := catalogActionPicker(cfg, entity)
		picker.Title = "Map Kubernetes target"
		if action == "unmap-resource" {
			picker.Title = "Unmap component"
		}
		options := []ui.Option{}
		for _, option := range picker.Options {
			if action == "kubernetes-map" && (option.Name == "kubernetes-set-project" || option.Name == "kubernetes-map-identity") || action == "unmap-resource" && (strings.Contains(option.Name, "-remove:") || strings.Contains(option.Name, "-remove-identity:")) {
				options = append(options, option)
			}
		}

		picker.Options = options
		return ui.Transition{Picker: picker}
	}
	if strings.HasPrefix(action, "add-") {
		return catalogAddTransition(cfg, action, draft, editor)
	}
	if strings.HasPrefix(action, "project-remove-identity:") {
		return catalogPlanTransition(cfg, entity, action, strings.TrimPrefix(action, "project-remove-identity:"), editor)
	}
	if strings.HasPrefix(action, "identity-remove-project:") || strings.HasPrefix(action, "kubernetes-remove-identity:") || strings.HasPrefix(action, "docker-remove-workspace:") || strings.HasPrefix(action, "workspace-remove:") {
		return catalogPlanTransition(cfg, entity, action, "", editor)
	}
	if action == "kubernetes-remove-project" {
		return catalogPlanTransition(cfg, entity, action, "", editor)
	}
	if action == "hide-entity" || action == "unhide-entity" {
		ref, err := decodeCatalogRef(entity)
		if err != nil {
			return catalogPreviewError(err, editor)
		}
		plan, err := catalog.PlanSetHidden(cfg, ref, action == "hide-entity")
		return catalogVisibilityTransition(plan, err, editor)
	}
	if action == "forget-entity" || action == "remove-entity" {
		ref, err := decodeCatalogRef(entity)
		if err != nil {
			return catalogPreviewError(err, editor)
		}
		var plan catalog.Plan
		if action == "forget-entity" {
			plan, err = catalog.PlanForget(cfg, ref)
		} else {
			plan, err = catalog.PlanRemove(cfg, ref)
		}
		return catalogPreviewTransition(plan, err, editor)
	}
	return ui.Transition{Picker: catalogDependencyPicker(cfg, entity, action)}
}

func catalogVisibilityTransition(plan catalog.Plan, err error, editor *catalogEditorState) ui.Transition {
	editor.selectionWorkspace = ""
	if err != nil || !plan.Valid() {
		return catalogPreviewTransition(plan, err, editor)
	}
	editor.err = nil
	editor.plan = &plan
	return ui.Transition{Complete: true}
}

func catalogVisibilityPlan(plan *catalog.Plan) bool {
	if plan == nil || !plan.Valid() || len(plan.Changes) != 1 {
		return false
	}
	return plan.Changes[0].Action == "hide" || plan.Changes[0].Action == "unhide"
}

func catalogInlineActionTransition(cfg config.Config, action string, draft ui.Draft, editor *catalogEditorState) ui.Transition {
	return catalogActionTransition(cfg, action, draft, editor)
}

func catalogDraftEntity(draft ui.Draft) string {
	for _, screen := range []ui.Screen{screenCatalogProject, screenCatalogIdentity, screenCatalogList, ui.ScreenCatalogEntity} {
		if entity := draft[screen]; entity != "" {
			if _, err := decodeCatalogRef(entity); err == nil {
				return entity
			}
		}
	}
	return ""
}

func catalogDraftAction(draft ui.Draft) string {
	for _, screen := range []ui.Screen{ui.ScreenCatalogAction, screenCatalogProject, screenCatalogIdentity} {
		action := draft[screen]
		if action == "" {
			continue
		}
		if _, err := decodeCatalogRef(action); err != nil {
			return action
		}
	}
	return ""
}
