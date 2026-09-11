package main

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

func workspaceBuilderPicker(cfg config.Config, draft *workspaceEditorDraft) ui.Picker {
	resolved, resolveErr := resolveWorkspaceDraft(cfg, draft)
	title := "Configure workspace › " + draft.Name
	description := "Changes aren’t saved until confirmed."
	if !draft.Existing {
		title = "Create workspace › " + draft.Name
	}
	if draft.Notice != "" {
		description = draft.Notice + "\n\n" + description
	}
	if resolveErr != nil {
		description += "\n\nNeeds attention: " + resolveErr.Error()
	}
	options := []ui.Option{}
	if !draft.Existing {
		options = append(options, ui.Option{Name: "workspace-name", Label: "Name", Detail: draft.Name})
	}
	options = append(options, workspaceEditorComponents(cfg, draft.Value, resolved)...)
	if original, ok := cfg.Destinations[draft.Name]; draft.Existing && ok {
		previous, _ := resolveWorkspaceDraft(cfg, &workspaceEditorDraft{Name: draft.Name, Value: original})
		for index, old := range workspaceEditorComponents(cfg, original, previous) {
			if options[index].Detail != old.Detail {
				options[index].Summary = "Previously: " + old.Detail + "\nNow: " + options[index].Detail
				options[index].Detail = old.Detail + " → " + options[index].Detail
				options[index].Label += " *"
			}
		}
		if !reflect.DeepEqual(original, draft.Value) {
			description += "\n* Changed field — i shows full details."
		}
	}
	if draft.Existing {
		if current, ok := cfg.Destinations[draft.Name]; ok && !reflect.DeepEqual(current, draft.Value) {
			detail := "review all staged changes, then save once"
			if resolveErr != nil {
				detail = "blocked until the workspace resolves"
			}
			options = append(options, ui.Option{Name: "workspace-save", Label: "Save workspace", Detail: detail})
		} else {
			description += "\n\nNo unsaved changes."
		}
		options = append(options,
			ui.Option{Name: "workspace-copy", Label: "Save as new workspace", Detail: "create a copy with a different name"},
			ui.Option{Name: "workspace-delete", Label: "Delete from ContextHop", Detail: "remove only the saved workspace; external resources are not changed"},
		)
	} else {
		createDetail := "review one atomic change, then save"
		if resolveErr != nil {
			createDetail = "blocked until the workspace resolves"
		}
		options = append(options, ui.Option{Name: "workspace-create", Label: "Create workspace", Detail: createDetail})
	}
	fields := []ui.PickerField{}
	return ui.Picker{
		ContextFields: fields,
		Screen:        screenAddWorkspaceBuilder, Title: title, Focus: workspaceSaveFocus(cfg, draft),
		Description: description, Dimension: "catalog-action", HideSearch: true, ShowSelectedInfo: true, Options: options,
	}
}

func workspaceEditorComponents(cfg config.Config, value config.Destination, resolved resolver.Resolved) []ui.Option {
	return []ui.Option{
		ui.Option{Name: "workspace-set:" + string(catalog.KindKubernetes), Label: "Kubernetes", Detail: workspaceDraftComponentSummary(cfg, value.Kubernetes, resolved.KubernetesName, catalog.KindKubernetes)},
		ui.Option{Name: "workspace-set:" + string(catalog.KindProject), Label: "Project", Detail: workspaceDraftComponentSummary(cfg, value.Project, resolved.ProjectName, catalog.KindProject)},
		ui.Option{Name: "workspace-set:" + string(catalog.KindIdentity), Label: "Identity", Detail: workspaceDraftComponentSummary(cfg, value.Identity, resolved.IdentityName, catalog.KindIdentity)},
		ui.Option{Name: "workspace-set:" + string(catalog.KindDocker), Label: "Docker", Detail: workspaceDraftComponentSummary(cfg, value.Docker, resolved.DockerName, catalog.KindDocker)},
		ui.Option{Name: "workspace-adc", Label: "Application credentials (ADC)", Detail: workspaceADCLabel(value.ADC)},
	}
}

func workspaceEditPicker(cfg config.Config, name string, editor *catalogEditorState, notice string) ui.Picker {
	workspace, ok := cfg.Destinations[name]
	if !ok {
		return ui.Picker{Screen: screenAddWorkspaceBuilder, Title: "Configure workspace", Description: "Unknown workspace " + name, Dimension: "catalog-action", HideSearch: true}
	}
	editor.workspace = &workspaceEditorDraft{Name: name, Value: workspace, Existing: true, Notice: notice}
	return workspaceBuilderPicker(cfg, editor.workspace)
}

func resolveWorkspaceDraft(cfg config.Config, draft *workspaceEditorDraft) (resolver.Resolved, error) {
	if draft == nil {
		return resolver.Resolved{}, errors.New("workspace draft is unavailable")
	}
	return resolver.Components(cfg, resolver.Selection{
		Name: draft.Name, Identity: draft.Value.Identity, Project: draft.Value.Project,
		Kubernetes: draft.Value.Kubernetes, Docker: draft.Value.Docker,
		ADC: draft.Value.ADC, Risk: draft.Value.Risk,
	})
}

func workspaceDraftComponentSummary(cfg config.Config, explicit, resolved string, kind catalog.Kind) string {
	name := explicit
	status := "explicit"
	if name == "" {
		name = resolved
		status = "inferred"
	}
	if name == "" {
		return "None"
	}
	return workspaceComponentDisplay(cfg, kind, name) + " · " + status
}

func workspaceComponentDisplay(cfg config.Config, kind catalog.Kind, name string) string {
	switch kind {
	case catalog.KindIdentity:
		if item, ok := cfg.Identities[name]; ok {
			return item.Account
		}
	case catalog.KindProject:
		if item, ok := cfg.Projects[name]; ok {
			return item.ProjectID
		}
	case catalog.KindKubernetes:
		if item, ok := cfg.Kubernetes[name]; ok {
			return firstNonEmpty(item.Cluster, item.Context, name)
		}
	case catalog.KindDocker:
		if item, ok := cfg.Docker[name]; ok {
			return item.Context
		}
	}
	return name
}

func workspaceADCLabel(adc string) string {
	if adc == "identity" {
		return "Enabled · uses workspace identity"
	}
	return "Disabled"
}

func workspaceRiskLabel(explicit, resolved string) string {
	if explicit != "" {
		return strings.ToUpper(explicit) + " · explicit"
	}
	if resolved != "" {
		return strings.ToUpper(resolved) + " · inferred"
	}
	return "None · automatic"
}

func workspaceComponentPicker(cfg config.Config, draft *workspaceEditorDraft, screen ui.Screen) ui.Picker {
	kind := draft.Editing
	options := compatibleWorkspaceComponentOptions(cfg, draft.Value, kind)
	options = appendCurrentHiddenWorkspaceComponent(cfg, draft.Value, kind, options)
	if screen == screenAddWorkspaceComponent {
		label := "No " + string(kind)
		detail := "leave this component unset"
		if inferred := inferredWorkspaceComponent(cfg, draft, kind); inferred != "" {
			label = "Automatic " + string(kind)
			detail = "use " + workspaceComponentDisplay(cfg, kind, inferred) + " from catalog mappings"
		}
		options = append([]ui.Option{{Name: "", Label: label, Detail: detail}}, options...)
	}
	return ui.Picker{
		Screen: screen, Title: "Workspace › Select " + string(kind), Dimension: "catalog-target",
		Description: "Compatible choices are shown with the context needed to distinguish them.", Options: options,
	}
}

func appendCurrentHiddenWorkspaceComponent(cfg config.Config, workspace config.Destination, kind catalog.Kind, options []ui.Option) []ui.Option {
	name, err := workspaceComponentName(workspace, kind)
	if err != nil || name == "" || !catalogRefHidden(cfg, catalog.Ref{Kind: kind, Name: name}) {
		return options
	}
	for index, option := range options {
		if option.Name == name {
			options[index].Detail = strings.TrimSpace(options[index].Detail + " · HIDDEN")
			return options
		}
	}
	var option ui.Option
	switch kind {
	case catalog.KindIdentity:
		item := cfg.Identities[name]
		option = ui.Option{Name: name, Identity: true, IdentityAccount: item.Account, Provider: item.Provider, AuthStatus: "hidden"}
	case catalog.KindProject:
		item := cfg.Projects[name]
		option = ui.Option{Name: name, Project: true, ProjectID: item.ProjectID, IdentityAccount: projectIdentitySummary(cfg, item), KubernetesContext: kubernetesCountSummary(cfg, name), Risk: item.Risk}
	case catalog.KindKubernetes:
		item := cfg.Kubernetes[name]
		option = ui.Option{Name: name, Kubernetes: true, KubernetesCluster: kubernetesPhysicalName(name, item), KubernetesContext: firstNonEmpty(item.Context, item.Cluster, name), KubernetesLocation: item.Location, Risk: item.Risk}
	case catalog.KindDocker:
		item := cfg.Docker[name]
		option = ui.Option{Name: name, Docker: true, DockerContext: item.Context, Risk: item.Risk}
	default:
		return options
	}
	formatted := formatCatalogComponentOptions(kind, []ui.Option{option})[0]
	formatted.Detail = strings.TrimSpace(formatted.Detail + " · HIDDEN")
	return append(options, formatted)
}

func compatibleWorkspaceComponentOptions(cfg config.Config, workspace config.Destination, kind catalog.Kind) []ui.Option {
	switch kind {
	case catalog.KindIdentity:
		projectName := workspace.Project
		if target, ok := cfg.Kubernetes[workspace.Kubernetes]; projectName == "" && ok {
			projectName = target.Project
		}
		if project, ok := cfg.Projects[projectName]; ok {
			if len(project.Identities) > 0 {
				visible := slices.DeleteFunc(slices.Clone(project.Identities), func(name string) bool {
					return name != workspace.Identity && cfg.Identities[name].Hidden
				})
				return formatCatalogComponentOptions(kind, identityOptions(cfg, visible))
			}
			return formatCatalogComponentOptions(kind, compatibleIdentityOptions(cfg, project.Provider))
		}
	case catalog.KindProject:
		if target, ok := cfg.Kubernetes[workspace.Kubernetes]; ok && target.Project != "" {
			options := projectOptions(cfg, "")
			return formatCatalogComponentOptions(kind, filterOptionsByName(options, []string{target.Project}))
		}
		if workspace.Identity != "" {
			return formatCatalogComponentOptions(kind, projectOptions(cfg, workspace.Identity))
		}
	case catalog.KindKubernetes:
		return formatCatalogComponentOptions(kind, kubernetesOptions(cfg, workspace.Project))
	}
	return catalogComponentOptions(cfg, kind)
}

func filterOptionsByName(options []ui.Option, names []string) []ui.Option {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	result := make([]ui.Option, 0, len(options))
	for _, option := range options {
		if allowed[option.Name] {
			result = append(result, option)
		}
	}
	return result
}

func inferredWorkspaceComponent(cfg config.Config, draft *workspaceEditorDraft, kind catalog.Kind) string {
	withoutExplicit := *draft
	setWorkspaceDraftComponent(&withoutExplicit.Value, kind, "")
	resolved, err := resolveWorkspaceDraft(cfg, &withoutExplicit)
	if err != nil {
		return ""
	}
	switch kind {
	case catalog.KindIdentity:
		return resolved.IdentityName
	case catalog.KindProject:
		return resolved.ProjectName
	case catalog.KindKubernetes:
		return resolved.KubernetesName
	case catalog.KindDocker:
		return resolved.DockerName
	}
	return ""
}

func setWorkspaceDraftComponent(workspace *config.Destination, kind catalog.Kind, value string) {
	switch kind {
	case catalog.KindIdentity:
		workspace.Identity = value
	case catalog.KindProject:
		workspace.Project = value
	case catalog.KindKubernetes:
		workspace.Kubernetes = value
	case catalog.KindDocker:
		workspace.Docker = value
	}
}

func workspaceADCPicker() ui.Picker {
	return ui.Picker{
		Screen: screenAddWorkspaceADC, Title: "Workspace › Application credentials (ADC)", Dimension: "catalog-action", HideSearch: true,
		Description: "ADC is opt-in and uses the identity resolved by this workspace.",
		Options: []ui.Option{
			{Name: "", Label: "Disabled", Detail: "do not expose application-default credentials"},
			{Name: "identity", Label: "Enabled", Detail: "use isolated ADC for the workspace identity"},
		},
	}
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func workspaceComponentName(workspace config.Destination, kind catalog.Kind) (string, error) {
	switch kind {
	case catalog.KindIdentity:
		return workspace.Identity, nil
	case catalog.KindProject:
		return workspace.Project, nil
	case catalog.KindKubernetes:
		return workspace.Kubernetes, nil
	case catalog.KindDocker:
		return workspace.Docker, nil
	default:
		return "", fmt.Errorf("unsupported workspace component %q", kind)
	}
}

func workspaceSaveFocus(cfg config.Config, draft *workspaceEditorDraft) string {
	if !draft.Existing {
		return "workspace-create"
	}
	if !reflect.DeepEqual(cfg.Destinations[draft.Name], draft.Value) {
		return "workspace-save"
	}
	return "workspace-set:kubernetes"
}

func workspaceEditorPreview(plan catalog.Plan, err error, editor *catalogEditorState) ui.Transition {
	var transition ui.Transition
	if editor.workspace.SelectAfterSave {
		transition = workspaceSelectionPreview(plan, err, editor.workspace.Name, editor)
	} else {
		transition = catalogPreviewTransition(plan, err, editor)
	}
	editor.workspaceReview = true
	return transition
}
