package main

import (
	"fmt"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/selection"
	"github.com/infurio/contexthop/internal/ui"
)

const screenShellMenu ui.Screen = "shell-menu"
const screenShellADC ui.Screen = "shell-adc"

func shellADCMode(value string) (string, error) {
	return selection.ADCMode(selection.ADCChoice(value))
}

func overrideWorkspaceADC(cfg *config.Config, name, value string) error {
	workspace, ok := cfg.Destinations[name]
	if !ok {
		return fmt.Errorf("unknown workspace %q", name)
	}
	mode, err := shellADCMode(value)
	if err != nil {
		return err
	}
	workspace.ADC = mode
	cfg.Destinations[name] = workspace
	return nil
}

func shellMenuFlow(cfg config.Config, choice ui.Choice, draft ui.Draft) (ui.Transition, bool) {
	if choice.Action == "shell-menu" {
		return ui.Transition{Picker: shellMenuPicker(cfg, draft, choice.CanApplyShell)}, true
	}
	if choice.Screen == screenShellADC {
		value := choice.Option.Name
		if value != "default" {
			if _, err := shellADCMode(value); err != nil {
				return shellMenuError(err), true
			}
		}
		updated := cloneApplicationDraft(draft)
		updated[ui.ScreenShellADCOverride] = value
		if value == "default" {
			delete(updated, ui.ScreenShellADCOverride)
			value = ""
		}
		return ui.Transition{ReturnToPrevious: true, Picker: shellMenuPicker(cfg, updated, choice.CanApplyShell), DraftUpdates: ui.Draft{ui.ScreenShellADCOverride: value}}, true
	}
	if choice.Screen != screenShellMenu {
		return ui.Transition{}, false
	}
	switch choice.Option.Name {
	case "adc":
		return ui.Transition{Picker: ui.Picker{Screen: screenShellADC, Title: "Application credentials (ADC)", HideSearch: true, CompactDialog: true, ActionRows: true, Description: "CLI commands use the selected cloud identity. Application code uses ADC separately. This choice applies only to this launch.", Options: []ui.Option{{Name: "default", Label: "Use workspace setting", Detail: "disabled when no workspace setting is present"}, {Name: "identity", Label: "Use selected identity’s ADC", Detail: "isolated application credentials configured for this identity"}, {Name: "off", Label: "Disable ADC", Detail: "do not expose application credentials"}}}}, true
	case "integration":
		return ui.Transition{Picker: ui.Picker{Screen: "shell-integration", Title: "Enable shell integration", Description: "In Zsh, run this in the shell you want to manage:\n\neval \"$(chop shell-init zsh --in-place)\"\n\nThen reopen chop and choose Use in current shell.", HideSearch: true, DisableEnter: true}}, true
	case "apply-shell":
		if !choice.CanApplyShell {
			return shellMenuError(fmt.Errorf("current-shell integration is not enabled")), true
		}
	}
	return ui.Transition{}, false
}

func shellMenuError(err error) ui.Transition {
	return ui.Transition{Picker: ui.Picker{Screen: "shell-selection-error", Title: "Shell selection needs attention", Description: err.Error(), HideSearch: true, DisableEnter: true}}
}

func shellMenuPicker(cfg config.Config, draft ui.Draft, canApply bool) ui.Picker {
	resolved, err := resolveShellSelection(cfg, draft)
	if err != nil {
		return shellMenuError(err).Picker
	}
	identity, project, kubernetes, docker := "None", "None", "None", "None"
	if resolved.Identity != nil {
		identity = resolved.Identity.Account + " [" + resolved.IdentityName + "]"
	}
	if resolved.Project != nil {
		project = resolved.Project.ProjectID
	}
	if resolved.Kubernetes != nil {
		kubernetes = firstNonEmpty(resolved.Kubernetes.Context, resolved.Kubernetes.Cluster, resolved.KubernetesName)
	}
	if resolved.Docker != nil {
		docker = resolved.Docker.Context
	}
	adc := "Disabled"
	if resolved.ADCMode == "identity" {
		adc = "Selected identity’s ADC"
	}
	source := "default"
	if firstNonEmpty(draft[ui.ScreenWorkspace], draft[ui.ScreenWorkspaceSource]) != "" {
		source = "workspace setting"
	}
	if draft[ui.ScreenShellADCOverride] != "" {
		source = "this launch"
	}
	options := []ui.Option{{Name: "default-shell", Label: "Update shared", EnterLabel: "Update shared", Detail: "Update the shared config and follow it in this terminal. Pinned shells and subshells stay unchanged."}, {Name: "adc", Label: "Application credentials (ADC)", Detail: adc + " · " + source, EnterLabel: "Change"}, {Name: "launch-shell", Label: "Start subshell", EnterLabel: "Start", Detail: "Exit to return to your original shell."}}
	if canApply {
		options = append(options, ui.Option{Name: "apply-shell", Label: "Pin", EnterLabel: "Pin", Detail: "Keep this terminal separate from shared config changes."})
	} else {
		options = append(options, ui.Option{Name: "integration", Label: "Enable shell integration", EnterLabel: "Set up", Detail: "Set up Zsh to switch this shell."})
	}
	fields := []ui.PickerField{{Label: "Identity", Value: identity}, {Label: "Project", Value: project}, {Label: "Kubernetes", Value: kubernetes}, {Label: "Docker", Value: docker}}
	if resolved.Kubernetes != nil {
		fields = append(fields, ui.PickerField{Label: "Namespace", Value: firstNonEmpty(resolved.Kubernetes.Namespace, "default")})
	}
	return ui.Picker{Screen: screenShellMenu, Title: "Launch settings", Focus: "default-shell", KeepDraftOnCancel: true, HideSearch: true, CompactDialog: true, ActionRows: true, ContextFields: fields, Options: options}
}
