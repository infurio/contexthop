package main

import (
	"fmt"
	"strings"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func catalogPlanTransition(cfg config.Config, encoded, action, target string, editor *catalogEditorState) ui.Transition {
	ref, err := decodeCatalogRef(encoded)
	var plan catalog.Plan
	if err == nil {
		switch {
		case action == "identity-map-project":
			plan, err = catalog.PlanMapIdentity(cfg, target, ref.Name)
		case action == "project-map-identity":
			plan, err = catalog.PlanMapIdentity(cfg, ref.Name, target)
		case strings.HasPrefix(action, "project-remove-identity:"):
			plan, err = catalog.PlanRemoveIdentityMapping(cfg, ref.Name, target)
		case strings.HasPrefix(action, "identity-remove-project:"):
			plan, err = catalog.PlanRemoveIdentityMapping(cfg, strings.TrimPrefix(action, "identity-remove-project:"), ref.Name)
		case action == "kubernetes-set-project":
			plan, err = catalog.PlanSetKubernetesProject(cfg, ref.Name, target)
		case action == "kubernetes-remove-project":
			plan, err = catalog.PlanRemoveKubernetesProject(cfg, ref.Name)
		case action == "kubernetes-map-identity":
			plan, err = catalog.PlanMapIdentity(cfg, cfg.Kubernetes[ref.Name].Project, target)
		case strings.HasPrefix(action, "kubernetes-remove-identity:"):
			plan, err = catalog.PlanRemoveIdentityMapping(cfg, cfg.Kubernetes[ref.Name].Project, strings.TrimPrefix(action, "kubernetes-remove-identity:"))
		case action == "docker-map-workspace":
			plan, err = catalog.PlanSetWorkspaceComponent(cfg, target, catalog.KindDocker, ref.Name)
		case strings.HasPrefix(action, "docker-remove-workspace:"):
			plan, err = catalog.PlanSetWorkspaceComponent(cfg, strings.TrimPrefix(action, "docker-remove-workspace:"), catalog.KindDocker, "")
		case strings.HasPrefix(action, "workspace-set:"):
			plan, err = catalog.PlanSetWorkspaceComponent(cfg, ref.Name, catalog.Kind(strings.TrimPrefix(action, "workspace-set:")), target)
		case strings.HasPrefix(action, "workspace-remove:"):
			plan, err = catalog.PlanSetWorkspaceComponent(cfg, ref.Name, catalog.Kind(strings.TrimPrefix(action, "workspace-remove:")), "")
		default:
			err = fmt.Errorf("unsupported catalog action %q", action)
		}
	}
	return catalogPreviewTransition(plan, err, editor)
}

func catalogPreviewTransition(plan catalog.Plan, err error, editor *catalogEditorState) ui.Transition {
	editor.workspaceReview = false
	editor.applyImmediately = false
	editor.selectionWorkspace = ""
	editor.err = err
	editor.savingProject = false
	editor.plan = nil
	if err == nil {
		editor.plan = &plan
	}
	if err == nil && routineCatalogPlan(plan) {
		editor.applyImmediately = true
		return ui.Transition{Complete: true}
	}
	return ui.Transition{Picker: catalogConfirmPicker(editor)}
}

// Additive local edits do not need a second approval. Changes that alter a
// workspace's resolved behavior or configure cluster access retain their review.
func routineCatalogPlan(plan catalog.Plan) bool {
	if !plan.Valid() || len(plan.Impacts) > 0 || len(plan.Changes) == 0 {
		return false
	}
	for _, change := range plan.Changes {
		switch change.Action {
		case "map":
			if change.From.Kind != catalog.KindIdentity || change.To.Kind != catalog.KindProject {
				return false
			}
		case "hide", "unhide", "remove_mapping", "browser":
			// Dependency impacts above still require review.
		case "add":
			if change.To.Kind != catalog.KindIdentity && change.To.Kind != catalog.KindProject && change.To.Kind != catalog.KindDocker {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func catalogPlanDescription(plan catalog.Plan, err error) string {
	if err != nil {
		return "Cannot prepare this change: " + err.Error()
	}
	parts := make([]string, 0, len(plan.Changes)+len(plan.Impacts)+1)
	for _, change := range plan.Changes {
		if change.Action == "delete_tag" {
			parts = append(parts, fmt.Sprintf("Delete shared tag %q everywhere.", change.To.Name))
			continue
		}
		if change.Action == "untag" {
			parts = append(parts, "Remove assignment from "+formatCatalogPlanRef(plan.Config, change.To))
			continue
		}
		if change.Detail != "" {
			action := strings.ReplaceAll(change.Action, "_", " ")
			parts = append(parts, action+" "+formatCatalogPlanRef(plan.Config, change.To)+" "+change.Detail)
			continue
		}
		switch change.Action {
		case "add":
			parts = append(parts, "add "+formatCatalogPlanRef(plan.Config, change.To))
		case "remove":
			parts = append(parts, "delete "+formatCatalogRef(change.From)+" from ContextHop (the external resource is not changed; discovery may import it again)")
		case "forget":
			parts = append(parts, "forget "+formatCatalogRef(change.From)+" (discovery may add it again)")
		case "hide":
			parts = append(parts, "hide "+formatCatalogRef(change.From)+" from normal catalog lists; existing mappings remain active")
		case "unhide":
			parts = append(parts, "unhide "+formatCatalogRef(change.From)+" and return it to normal catalog lists")
		default:
			parts = append(parts, fmt.Sprintf("%s %s → %s", strings.ReplaceAll(change.Action, "_", " "), formatCatalogPlanRef(plan.Config, change.From), formatCatalogPlanRef(plan.Config, change.To)))
		}
	}
	for _, impact := range plan.Impacts {
		parts = append(parts, "workspace "+impact.Workspace+": "+formatWorkspaceState(impact.Before)+" → "+formatWorkspaceState(impact.After))
	}
	for _, problem := range plan.Problems {
		parts = append(parts, "BLOCKED: "+problem)
	}
	if len(parts) == 0 {
		return "No configuration change is required."
	}
	return strings.Join(parts, "\n")
}

func formatCatalogPlanRef(cfg config.Config, ref catalog.Ref) string {
	switch ref.Kind {
	case catalog.KindIdentity:
		if identity, ok := cfg.Identities[ref.Name]; ok && identity.Account != "" && identity.Account != ref.Name {
			return "identity:" + identity.Account + " [" + ref.Name + "]"
		}
	case catalog.KindProject:
		if project, ok := cfg.Projects[ref.Name]; ok && project.ProjectID != "" && project.ProjectID != ref.Name {
			return "project:" + project.ProjectID + " [" + ref.Name + "]"
		}
	}
	return formatCatalogRef(ref)
}

func formatCatalogRef(ref catalog.Ref) string {
	if ref.Kind == "" || ref.Name == "" {
		return "none"
	}
	return string(ref.Kind) + ":" + ref.Name
}

func formatWorkspaceState(state catalog.WorkspaceState) string {
	if !state.Valid {
		if state.Error == "" {
			return "absent"
		}
		return "invalid (" + state.Error + ")"
	}
	components := make([]string, 0, 5)
	for _, component := range []struct{ kind, name string }{
		{"identity", state.Identity}, {"project", state.Project}, {"kubernetes", state.Kubernetes}, {"docker", state.Docker},
	} {
		if component.name != "" {
			components = append(components, component.kind+":"+component.name)
		}
	}

	if state.ADC != "" {
		components = append(components, "ADC:"+state.ADC)
	}
	if len(components) == 0 {
		return "valid"
	}
	return strings.Join(components, ", ")
}

func catalogConfirmPicker(editor *catalogEditorState) ui.Picker {
	blocked := func(description string) ui.Picker {
		return ui.Picker{
			Screen: ui.ScreenConfirm, Title: "Catalog › Change blocked",
			Description: description, OperationDetails: description,
			Dimension: "catalog-confirm", HideSearch: true,
		}
	}
	if editor.err != nil {
		return blocked(catalogPlanDescription(catalog.Plan{}, editor.err) + "\n\nPress Esc to go back.")
	}
	if editor.plan == nil {
		return blocked("No configuration change was prepared.\n\nPress Esc to go back.")
	}
	description := catalogPlanDescription(*editor.plan, nil)
	if !editor.plan.Valid() {
		return blocked(description + "\n\nResolve these dependencies, then try again. Press Esc to go back.")
	}
	title, focus := "Catalog › Confirm change", ""
	for _, change := range editor.plan.Changes {
		switch change.Action {
		case "remove", "forget", "delete_tag":
			title, focus = "Catalog › Confirm deletion", "cancel"
		}
	}
	return ui.Picker{
		Screen: ui.ScreenConfirm, Title: title, Focus: focus, Description: description,
		OperationDetails: description, Dimension: "catalog-confirm", HideSearch: true,
		Options: []ui.Option{
			{Name: "apply", Label: catalogApplyLabel(*editor.plan), Detail: "save these changes"},
			{Name: "cancel", Label: "Cancel", Detail: "leave configuration unchanged"},
		},
	}
}

func catalogApplyLabel(plan catalog.Plan) string {
	if len(plan.Changes) > 0 && plan.Changes[0].Action == "delete_tag" {
		return "Delete tag everywhere"
	}
	if len(plan.Changes) != 1 {
		return "Apply changes"
	}
	change := plan.Changes[0]
	switch change.Action {
	case "hide":
		return "Hide from catalog"
	case "unhide":
		return "Unhide"
	case "forget":
		return catalogForgetLabel(change.From.Kind)
	case "remove":
		return "Delete from ContextHop"
	case "add":
		return "Add " + string(change.To.Kind)
	}
	return "Apply change"
}

type catalogPostApply struct {
	Entity   string
	Category string
	Notice   string
}

func catalogPostApplyDestination(plan catalog.Plan, draft ui.Draft) catalogPostApply {
	for _, change := range plan.Changes {
		switch change.Action {
		case "hide":
			return catalogPostApply{Entity: encodeCatalogRef(change.From), Notice: catalogKindLabel(change.From.Kind) + " hidden successfully."}
		case "unhide":
			return catalogPostApply{Entity: encodeCatalogRef(change.From), Notice: catalogKindLabel(change.From.Kind) + " unhidden successfully."}
		}
	}
	for _, change := range plan.Changes {
		if change.Action != "forget" && change.Action != "remove" {
			continue
		}
		if parent := catalogDraftParentEntity(draft, change.From); parent.Name != "" && catalogRefExists(plan.Config, parent) {
			return catalogPostApply{Entity: encodeCatalogRef(parent), Notice: catalogKindLabel(change.From.Kind) + " removed successfully."}
		}
		verb := "forgotten"
		if change.Action == "remove" {
			verb = "deleted"
		}
		category := catalogCategoryForKind(change.From.Kind)
		if draft[ui.ScreenCatalogEntity] == catalogCategoryPrefix+"hidden" {
			category = "hidden"
		}
		return catalogPostApply{Category: category, Notice: catalogKindLabel(change.From.Kind) + " " + verb + " successfully."}
	}

	if current, err := decodeCatalogRef(catalogDraftEntity(draft)); err == nil && catalogRefExists(plan.Config, current) {
		return catalogPostApply{Entity: encodeCatalogRef(current), Notice: catalogKindLabel(current.Kind) + " updated successfully."}
	}
	for index := len(plan.Changes) - 1; index >= 0; index-- {
		change := plan.Changes[index]
		if change.To.Name == "" || !catalogRefExists(plan.Config, change.To) {
			continue
		}
		verb := "updated"
		if change.Action == "add" {
			verb = "added"
		}
		return catalogPostApply{Entity: encodeCatalogRef(change.To), Notice: catalogKindLabel(change.To.Kind) + " " + verb + " successfully."}
	}
	return catalogPostApply{Notice: "Configuration updated successfully."}
}

func catalogDraftParentEntity(draft ui.Draft, removed catalog.Ref) catalog.Ref {
	path := make([]catalog.Ref, 0, 4)
	for _, screen := range []ui.Screen{ui.ScreenCatalogEntity, screenCatalogList, screenCatalogIdentity, screenCatalogProject} {
		ref, err := decodeCatalogRef(draft[screen])
		if err != nil || len(path) > 0 && path[len(path)-1] == ref {
			continue
		}
		path = append(path, ref)
	}
	for index := len(path) - 1; index >= 0; index-- {
		if path[index] != removed {
			continue
		}
		if index > 0 {
			return path[index-1]
		}
		break
	}
	return catalog.Ref{}
}

func catalogRefExists(cfg config.Config, ref catalog.Ref) bool {
	switch ref.Kind {
	case catalog.KindIdentity:
		_, ok := cfg.Identities[ref.Name]
		return ok
	case catalog.KindProject:
		_, ok := cfg.Projects[ref.Name]
		return ok
	case catalog.KindKubernetes:
		_, ok := cfg.Kubernetes[ref.Name]
		return ok
	case catalog.KindDocker:
		_, ok := cfg.Docker[ref.Name]
		return ok
	case catalog.KindWorkspace:
		_, ok := cfg.Destinations[ref.Name]
		return ok
	default:
		return false
	}
}

func catalogCategoryForKind(kind catalog.Kind) string {
	switch kind {
	case catalog.KindIdentity:
		return "identities"
	case catalog.KindProject:
		return "projects"
	case catalog.KindKubernetes:
		return "kubernetes"
	case catalog.KindDocker:
		return "docker"
	case catalog.KindWorkspace:
		return "workspaces"
	default:
		return ""
	}
}

func catalogKindLabel(kind catalog.Kind) string {
	switch kind {
	case catalog.KindKubernetes:
		return "Kubernetes target"
	case catalog.KindDocker:
		return "Docker context"
	case catalog.KindWorkspace:
		return "Workspace"
	case catalog.KindIdentity:
		return "Identity"
	case catalog.KindProject:
		return "Project"
	default:
		return "Resource"
	}
}

func projectIdentityOptions(cfg config.Config, projectName string) []ui.Option {
	project, ok := cfg.Projects[projectName]
	if !ok || len(project.Identities) == 0 {
		return nil
	}
	return identityOptions(cfg, project.Identities)
}
