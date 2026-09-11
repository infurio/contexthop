package main

import (
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
	"strings"
)

func projectSearchFlow(cfg, catalogCfg config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) ui.Transition {
	switch choice.Screen {
	case screenProjectSearchIdentity:
		return projectSearchPicker(cfg, catalogCfg, choice.Option.Name)
	case screenProjectSearchResults:
		if strings.HasPrefix(choice.Option.Name, authenticateProjectSearchPrefix) {
			identityName := strings.TrimPrefix(choice.Option.Name, authenticateProjectSearchPrefix)
			return ui.Transition{Picker: providerAuthPicker(cfg, "projects", identityName, "")}
		}
		if strings.HasPrefix(choice.Option.Name, retryProjectSearchPrefix) {
			return projectSearchPicker(cfg, catalogCfg, strings.TrimPrefix(choice.Option.Name, retryProjectSearchPrefix))
		}
		project := cfg.Projects[choice.Option.Name]
		identityName := firstNonEmpty(draft[ui.ScreenIdentity], draft[screenProjectSearchIdentity])
		// Discovery is a temporary chooser. Commit its result to the normal
		// selection and return to the browser, dropping the dialog stack.
		scope := strings.Join(nonEmpty(cfg.Identities[identityName].Account, project.ProjectID), " → ")
		return ui.Transition{
			ReplaceCurrent: true,
			DraftUpdates: ui.Draft{
				ui.ScreenIdentity: identityName, ui.ScreenProject: choice.Option.Name,
				ui.ScreenKubernetes: "", screenProjectSearchIdentity: "", screenProjectSearchResults: "",
			},
			Picker: resourceBrowserPicker(catalogCfg, ui.ScreenKubernetes,
				append(kubernetesOptions(cfg, choice.Option.Name), noKubernetesOption(cfg, choice.Option.Name, identityName)), scope),
		}

	}
	return ui.Transition{Complete: true}
}
