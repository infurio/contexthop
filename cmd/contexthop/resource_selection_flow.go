package main

import (
	"errors"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
	"strings"
)

func resourceSelectionFlow(cfg, catalogCfg config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) ui.Transition {
	switch choice.Screen {
	case ui.ScreenWorkspace:
		if choice.Action != "" {
			return resourceBrowserActionTransition(catalogCfg, choice, draft, editor)
		}
		if choice.Option.Name == createWorkspaceSelection {
			return catalogAddTransition(catalogCfg, "add-workspace", draft, editor)
		}
		return ui.Transition{Complete: true}
	case ui.ScreenDocker:
		if choice.Action != "" {
			return resourceBrowserActionTransition(catalogCfg, choice, draft, editor)
		}
		return ui.Transition{Complete: true}
	case ui.ScreenReuse:
		return ui.Transition{Complete: true}
	case ui.ScreenKubernetes:
		if choice.Option.Name == "\x00__discover_clusters__" {
			return discoverBrowserClusters(cfg, catalogCfg, draft[ui.ScreenIdentity], draft[ui.ScreenProject])
		}
		if choice.Action != "" {
			return resourceBrowserActionTransition(catalogCfg, choice, draft, editor)
		}
		if choice.Option.Name == noKubernetesSelection {
			return ui.Transition{Complete: true}
		}
		target := cfg.Kubernetes[choice.Option.Name]
		if target.Project != "" {
			suggested, candidates, _ := resolver.SuggestedIdentity(cfg, target.Project, choice.Option.Name)
			if suggested != "" {
				return ui.Transition{Complete: true}
			}
			identities := identityOptions(cfg, candidates)
			if len(candidates) == 0 {
				return ui.Transition{Picker: ui.Picker{
					Screen: ui.ScreenIdentity, Title: "Select identity for " + cfg.Projects[target.Project].ProjectID,
					Description: "This project has no saved identity mapping. Choose an identity for this session; the catalog is not changed.",
					Dimension:   "identity", Options: compatibleIdentityOptions(cfg, cfg.Projects[target.Project].Provider),
				}}
			}
			if len(candidates) > 1 {
				return ui.Transition{Picker: componentPicker(ui.ScreenIdentity, "Select identity for "+cfg.Projects[target.Project].ProjectID, identities)}
			}
		}
		return ui.Transition{Complete: true}
	case ui.ScreenProject:
		if choice.Action == "save-project-mapping" {
			identityName := draft[ui.ScreenIdentity]
			plan, err := catalog.PlanMapIdentity(catalogCfg, choice.Option.Name, identityName)
			transition := catalogPreviewTransition(plan, err, editor)
			if err == nil && plan.Valid() {
				editor.savingProject = true
				if !transition.Complete {
					transition.Picker.Title = "Save identity mapping"
					transition.Picker.Options[0].Label = "Save mapping"
				}
				transition.DraftUpdates = ui.Draft{screenCatalogList: encodeCatalogRef(catalog.Ref{Kind: catalog.KindProject, Name: choice.Option.Name})}
			}
			return transition
		}
		if choice.Action == "save-project" {
			project, exists := cfg.Projects[choice.Option.Name]
			if !exists {
				return catalogPreviewError(errors.New("project is no longer available; discover again"), editor)
			}
			plan, err := catalog.PlanAddProject(catalogCfg, choice.Option.Name, project)
			transition := catalogPreviewTransition(plan, err, editor)
			if err == nil && plan.Valid() {
				editor.savingProject = true
				if !transition.Complete {
					transition.Picker.Title = "Save project"
					transition.Picker.Options[0].Label = "Save project"
				}
			}
			return transition
		}
		if strings.HasPrefix(choice.Option.Name, authenticateProjectSearchPrefix) {
			return ui.Transition{Picker: providerAuthPicker(cfg, "projects", strings.TrimPrefix(choice.Option.Name, authenticateProjectSearchPrefix), "")}
		}
		if strings.HasPrefix(choice.Option.Name, retryProjectSearchPrefix) {
			return projectSearchPicker(cfg, catalogCfg, strings.TrimPrefix(choice.Option.Name, retryProjectSearchPrefix))
		}
		if choice.Action == "browse-kubernetes" {
			project := cfg.Projects[choice.Option.Name]
			scope := project.ProjectID
			if identityName := draft[ui.ScreenIdentity]; identityName != "" {
				scope = cfg.Identities[identityName].Account + " → " + scope
			}
			return ui.Transition{ReplaceCurrent: true, Picker: resourceBrowserPicker(catalogCfg, ui.ScreenKubernetes, kubernetesOptions(cfg, choice.Option.Name), scope)}
		}
		if choice.Action != "" {
			return resourceBrowserActionTransition(catalogCfg, choice, draft, editor)
		}
		if choice.Option.Name == searchProjectsSelection {
			if identityName := draft[ui.ScreenIdentity]; identityName != "" {
				return projectSearchPicker(cfg, catalogCfg, identityName)
			}
			return ui.Transition{Picker: ui.Picker{
				Screen: screenProjectSearchIdentity, Title: "Projects › Search › Select identity",
				Description: "Project discovery uses only the selected identity's isolated credentials.",
				Dimension:   "identity", Options: identityOptions(cfg, nil),
			}}
		}
		if choice.Option.Name == noProjectSelection {
			return ui.Transition{Complete: true}
		}
		project := cfg.Projects[choice.Option.Name]
		identityName := draft[ui.ScreenIdentity]
		if identityName == "" {
			suggested, candidates, _ := resolver.SuggestedIdentity(cfg, choice.Option.Name, "")
			if suggested != "" {
				identityName = suggested
			} else if len(candidates) > 1 {
				return ui.Transition{Picker: componentPicker(ui.ScreenIdentity, "Select identity for "+project.ProjectID, identityOptions(cfg, candidates))}
			} else if len(candidates) == 0 {
				return ui.Transition{Picker: ui.Picker{
					Screen: ui.ScreenIdentity, Title: "Select identity for " + project.ProjectID,
					Description: "This project has no saved identity mapping. Choose an identity for this session; the catalog is not changed.",
					Dimension:   "identity", Options: compatibleIdentityOptions(cfg, project.Provider),
				}}
			}
		}
		scope := project.ProjectID
		if identityName != "" {
			scope = cfg.Identities[identityName].Account + " → " + scope
		}
		return ui.Transition{Picker: resourceBrowserPicker(catalogCfg, ui.ScreenKubernetes, append(kubernetesOptions(cfg, choice.Option.Name), noKubernetesOption(cfg, choice.Option.Name, identityName)), scope)}
	case ui.ScreenIdentity:
		if choice.Action == "browse-project" || choice.Action == "browse-projects" {
			identity := cfg.Identities[choice.Option.Name]
			return ui.Transition{ReplaceCurrent: true, Picker: resourceBrowserPicker(catalogCfg, ui.ScreenProject, sessionProjectOptions(cfg, choice.Option.Name), identity.Account)}
		}
		if choice.Action != "" {
			return resourceBrowserActionTransition(catalogCfg, choice, draft, editor)
		}
		if draft[ui.ScreenKubernetes] != "" {
			return ui.Transition{Complete: true}
		}
		if projectName := draft[ui.ScreenProject]; projectName != "" {
			project := cfg.Projects[projectName]
			scope := cfg.Identities[choice.Option.Name].Account + " → " + project.ProjectID
			return ui.Transition{Picker: resourceBrowserPicker(catalogCfg, ui.ScreenKubernetes, append(kubernetesOptions(cfg, projectName), noKubernetesOption(cfg, projectName, choice.Option.Name)), scope)}
		}
		identity := cfg.Identities[choice.Option.Name]
		return ui.Transition{Picker: resourceBrowserPicker(catalogCfg, ui.ScreenProject, append(sessionProjectOptions(cfg, choice.Option.Name), noProjectOption(cfg, choice.Option.Name)), identity.Account)}

	}
	return ui.Transition{Complete: true}
}
