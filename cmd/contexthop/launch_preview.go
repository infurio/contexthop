package main

import (
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
	"strings"
)

func (c *applicationController) LaunchPreview(screen ui.Screen, option ui.Option, staged ui.Draft) ui.LaunchPreview {
	return nextShellPreview(c.resources.Selection(), screen, option, staged)
}

// Compute the exact launch request from local catalog data. Merely highlighting
// a row must never discover resources, authenticate, or mutate staged choices.
func nextShellPreview(cfg config.Config, screen ui.Screen, option ui.Option, staged ui.Draft) ui.LaunchPreview {
	name := option.Name

	draft := cloneApplicationDraft(staged)
	delete(draft, screenProjectSearchIdentity)
	delete(draft, screenProjectSearchResults)
	if screen == ui.ScreenWorkspace {
		draft = ui.Draft{ui.ScreenWorkspace: name, ui.ScreenWorkspaceSource: name}
		draft[ui.ScreenShellADCOverride] = staged[ui.ScreenShellADCOverride]
	} else if screen != "" {
		if draft[screen] != name {
			delete(draft, ui.ScreenWorkspace)
			delete(draft, ui.ScreenWorkspaceSource)
		}
		draft[screen] = name
		switch screen {
		case ui.ScreenIdentity:
			if project := draft[ui.ScreenProject]; project != "" && !resolver.ProjectIdentityAvailable(cfg, project, name) {
				delete(draft, ui.ScreenProject)
				delete(draft, ui.ScreenKubernetes)
			} else if cluster := draft[ui.ScreenKubernetes]; cluster != "" && !resolver.KubernetesIdentityAvailable(cfg, cluster, name) {
				delete(draft, ui.ScreenKubernetes)
			}
		case ui.ScreenProject:
			if strings.HasPrefix(name, "\x00__contexthop_no_") {
				delete(draft, ui.ScreenProject)
			}
			if cluster := draft[ui.ScreenKubernetes]; cluster != "" && cfg.Kubernetes[cluster].Project != draft[ui.ScreenProject] {
				delete(draft, ui.ScreenKubernetes)
			}
		case ui.ScreenKubernetes:
			if strings.HasPrefix(name, "\x00__contexthop_no_") {
				delete(draft, ui.ScreenKubernetes)
			} else {
				draft[ui.ScreenProject] = cfg.Kubernetes[name].Project
			}
		}
	}
	preview := ui.LaunchPreview{Available: true, Draft: draft}
	if draft[ui.ScreenWorkspace] == "" && draft[ui.ScreenProject] != "" {
		project, cluster, identity := draft[ui.ScreenProject], draft[ui.ScreenKubernetes], draft[ui.ScreenIdentity]
		eligible := resolver.ProjectIdentityAvailable(cfg, project, identity)
		if cluster != "" {
			eligible = resolver.KubernetesIdentityAvailable(cfg, cluster, identity)
		}
		if !eligible {
			suggested, candidates, err := resolver.SuggestedIdentity(cfg, project, cluster)
			draft[ui.ScreenIdentity] = suggested
			if err != nil {
				preview.Error = err.Error()
			}
			if suggested == "" {
				preview.NeedsIdentity = true
				preview.IdentityChoices = identityOptions(cfg, candidates)
			}
		}
	}
	resolved, err := resolveShellSelection(cfg, draft)
	if err == nil {
		for key, value := range workspaceSelectionDraft(resolved) {
			draft[key] = value
		}
	} else {
		preview.Error = err.Error()
	}
	// Render explicit absence as well as presence: a plain Docker launch must
	// not appear to retain the active cloud or Kubernetes context.
	identity := cfg.Identities[draft[ui.ScreenIdentity]].Account
	if preview.NeedsIdentity {
		identity = "Choose identity…"
	}
	cluster := cfg.Kubernetes[draft[ui.ScreenKubernetes]]
	clusterLabel := previewKubernetesContext(cluster, draft[ui.ScreenKubernetes], cfg.Projects[cluster.Project].ProjectID)
	if clusterLabel != "" {
		clusterLabel += "/" + previewKubernetesNamespace(cluster)
	}

	preview.Fields = []ui.PickerField{
		{Label: "id", Value: firstNonEmpty(identity, "none")},
		{Label: "project", Value: firstNonEmpty(cfg.Projects[draft[ui.ScreenProject]].ProjectID, "none")},
		{Label: "k8s", Value: firstNonEmpty(clusterLabel, "none")},
		{Label: "docker", Value: firstNonEmpty(cfg.Docker[draft[ui.ScreenDocker]].Context, "none")},
	}
	adc := "off"
	if resolved.ADCMode == "identity" {
		adc = "identity"
	}
	preview.Fields = append(preview.Fields, ui.PickerField{Label: "ADC", Value: adc})
	if preview.NeedsIdentity && len(preview.IdentityChoices) == 0 {
		preview.Error = "No identity has access to this context. Select an identity and discover resources with d, then retry."
	}
	return preview
}
