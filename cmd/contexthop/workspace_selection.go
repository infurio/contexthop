package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

const screenSaveWorkspaceTarget ui.Screen = "save-selection-workspace-target"
const screenSaveWorkspaceName ui.Screen = "save-selection-workspace-name"

// Workspace saving captures the same resolved combination used for activation.
func selectionWorkspaceFlow(cfg, saved config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) (ui.Transition, bool) {

	if choice.Screen == screenSaveWorkspaceName {
		if editor.workspace == nil {
			return catalogPreviewError(fmt.Errorf("workspace selection was lost; press Ctrl+W again"), editor), true
		}
		editor.workspace.Name = choice.Option.Name
		editor.workspace.Existing = false
		editor.workspace.Value.Hidden = false
		return ui.Transition{ReturnToScreen: screenAddWorkspaceBuilder, Picker: workspaceBuilderPicker(saved, editor.workspace)}, true
	}
	if choice.Screen == screenSaveWorkspaceTarget {
		updated := cloneApplicationDraft(draft)
		updated[ui.ScreenWorkspaceSource] = choice.Option.Name
		return selectionWorkspaceFlow(cfg, saved, ui.Choice{Action: "save-selection-workspace"}, updated, editor)
	}
	create := choice.Action == "add-resource" && choice.Screen == ui.ScreenWorkspace || choice.Screen == ui.ScreenWorkspace && choice.Option.Name == createWorkspaceSelection
	if choice.Action != "save-selection-workspace" && !create {
		return ui.Transition{}, false
	}
	request := selectionRequest(draft)
	resolved, err := request.ResolveForSave(cfg, saved)
	if err != nil {
		return catalogPreviewError(err, editor), true
	}
	source := request.SourceWorkspace()
	original, existing := saved.Destinations[source]
	if resolved.IdentityName == "" && resolved.ProjectName == "" && resolved.KubernetesName == "" && resolved.DockerName == "" {
		if create {
			return catalogAddTransition(saved, "add-workspace", draft, editor), true
		}
		return ui.Transition{Picker: ui.Picker{Screen: "workspace-selection-empty", Title: "Select workspace components", Description: "Stage entities in the tabs with Space, then press Ctrl+W to save the Selected as a named workspace.", HideSearch: true, DisableEnter: true}}, true
	}
	if !existing {
		matches := matchingWorkspaceNames(saved, resolved)
		if len(matches) > 1 && !create {
			options := make([]ui.Option, 0, len(matches))
			for _, name := range matches {
				workspace := saved.Destinations[name]
				options = append(options, ui.Option{Name: name, Label: name, Detail: "ADC: " + workspaceADCLabel(workspace.ADC) + " · Tags: " + strings.Join(workspace.TagNames(), ", ")})
			}
			return ui.Transition{Picker: ui.Picker{Screen: screenSaveWorkspaceTarget, Title: "Choose matching workspace", Description: "These workspaces contain the selected entities. Choose which one to update or use as the basis for a new workspace.", Options: options}}, true
		}
		if len(matches) == 1 {
			source = matches[0]
			original, existing = saved.Destinations[source]
		}
	}
	value := original
	if draft[ui.ScreenShellADCOverride] != "" || resolved.Identity == nil {
		value.ADC = resolved.ADCMode
	}
	value.Identity, value.Project, value.Kubernetes, value.Docker = resolved.IdentityName, resolved.ProjectName, resolved.KubernetesName, resolved.DockerName
	if existing {
		if previous, err := resolver.Destination(saved, source); err == nil {
			if previous.IdentityName == resolved.IdentityName {
				value.Identity = original.Identity
			}
			if previous.ProjectName == resolved.ProjectName {
				value.Project = original.Project
			}
			if previous.KubernetesName == resolved.KubernetesName {
				value.Kubernetes = original.Kubernetes
			}
			if previous.DockerName == resolved.DockerName {
				value.Docker = original.Docker
			}
		}
	}
	if !existing {
		value.Provenance = "manual"
	}
	editor.selectionWorkspace = ""
	editor.workspace = &workspaceEditorDraft{Name: source, Value: value, Existing: existing, SelectAfterSave: true}
	if draft[ui.ScreenShellADCOverride] != "" {
		editor.workspace.Notice = "The ADC setting from Selected is included. Review it before saving."
	}
	if create || !existing {
		return selectionWorkspaceNamePicker(saved, editor), true
	}
	return ui.Transition{Picker: workspaceBuilderPicker(saved, editor.workspace)}, true
}

func selectionWorkspaceNamePicker(cfg config.Config, editor *catalogEditorState) ui.Transition {
	picker := catalogInputPicker(screenSaveWorkspaceName, "Save as new workspace", "Name", "my-workspace", "Save the selected entities together. You will review the combination before saving.")
	picker.Input.Validate = catalogNameValidator(func(name string) bool { _, exists := cfg.Destinations[name]; return exists })
	return ui.Transition{Picker: picker}
}

func workspaceSelectionDraft(resolved resolver.Resolved) ui.Draft {
	return ui.Draft{ui.ScreenIdentity: resolved.IdentityName, ui.ScreenProject: resolved.ProjectName, ui.ScreenKubernetes: resolved.KubernetesName, ui.ScreenDocker: resolved.DockerName, ui.ScreenWorkspace: resolved.WorkspaceName, ui.ScreenWorkspaceSource: resolved.WorkspaceName}
}

func workspaceSelectionPreview(plan catalog.Plan, err error, name string, editor *catalogEditorState) ui.Transition {
	transition := catalogPreviewTransition(plan, err, editor)
	if err == nil && plan.Valid() {
		editor.selectionWorkspace = name
	}
	return transition
}

// Both the workspace checkmarks and Save use the same resolved entity match.
func matchingWorkspaceNames(cfg config.Config, selected resolver.Resolved) []string {
	matches := []string{}
	for name := range cfg.Destinations {
		workspace, err := resolver.Destination(cfg, name)
		if err == nil && workspace.IdentityName == selected.IdentityName && workspace.ProjectName == selected.ProjectName && workspace.KubernetesName == selected.KubernetesName && workspace.DockerName == selected.DockerName {
			matches = append(matches, name)
		}
	}
	slices.Sort(matches)
	return matches
}
