package main

import (
	"context"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

func launchFlow(cfg, catalogCfg config.Config, choice ui.Choice, draft ui.Draft) ui.Transition {
	if choice.Screen == screenResolveIdentity {
		draft[ui.ScreenIdentity] = choice.Option.Name
		screen := ui.ScreenProject
		if draft[ui.ScreenKubernetes] != "" {
			screen = ui.ScreenKubernetes
		}
		return ui.Transition{ResetNavigation: true, Picker: interactiveBrowserPicker(cfg, catalogCfg, screen, draft), DraftUpdates: ui.Draft{ui.ScreenIdentity: choice.Option.Name}}
	}
	if choice.Action == "default-shell" || choice.Action == "launch-shell" || choice.Action == "apply-shell" {
		if draft[ui.ScreenWorkspace] == "" && draft[ui.ScreenIdentity] == "" {
			projectName := draft[ui.ScreenProject]
			targetName := draft[ui.ScreenKubernetes]
			if target, ok := cfg.Kubernetes[targetName]; ok && target.Project != "" {
				projectName = target.Project
			}
			if _, ok := cfg.Projects[projectName]; ok {
				suggested, candidates, _ := resolver.SuggestedIdentity(cfg, projectName, targetName)
				if suggested == "" {
					options := identityOptions(cfg, candidates)
					description := "Choose the identity to use. Your project and Kubernetes selection are retained."
					if len(options) == 0 {
						description = "No identity has discovered access to this selection. Select an identity and refresh project and cluster discovery."
					}
					return ui.Transition{Picker: ui.Picker{Screen: screenResolveIdentity, Title: "Choose identity", Description: description, Dimension: "identity", Options: options, DisableEnter: len(options) == 0}}
				}
			}
		}
		resolved, err := resolveShellSelection(cfg, draft)
		if err != nil {
			return ui.Transition{Picker: ui.Picker{Screen: "selection-error", Title: "Selection needs attention", Description: err.Error(), HideSearch: true, DisableEnter: true}}
		}
		if resolved.Identity != nil {
			return authenticatedOperation(choice.Context, cfg, resolved.IdentityName, "Session launch", choice.ReportProgress, func(ctx context.Context, report func(string)) ui.Transition {
				return authenticatedADCLaunch(ctx, resolved, choice, report)
			}, resolved.ADCMode == "identity")
		}
		return ui.Transition{Complete: true, CompletionChoice: &choice}
	}

	return ui.Transition{}
}
