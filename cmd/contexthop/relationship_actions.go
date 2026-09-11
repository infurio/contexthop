package main

import (
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

const screenPreferredIdentity ui.Screen = "preferred-identity"
const screenRelationshipDiscovery ui.Screen = "relationship-discovery-identity"

func relationshipAction(cfg, saved config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) (ui.Transition, bool) {
	if choice.Screen == screenPreferredIdentity {
		ref, err := decodeCatalogRef(draft[screenCatalogList])
		if err != nil {
			return catalogPreviewError(err, editor), true
		}
		plan, err := catalog.PlanPreferredIdentity(saved, ref, choice.Option.Name)
		return catalogPreviewTransition(plan, err, editor), true
	}
	if choice.Screen == screenRelationshipDiscovery {
		ref, err := decodeCatalogRef(draft[screenCatalogList])
		if err != nil {
			return catalogPreviewError(err, editor), true
		}
		project := ref.Name
		if ref.Kind == catalog.KindKubernetes {
			project = cfg.Kubernetes[ref.Name].Project
		}
		return discoverBrowserClusters(cfg, saved, choice.Option.Name, project), true
	}
	action := choice.Action
	if action == "" && (choice.Screen == ui.ScreenCatalogAction || choice.Screen == screenCatalogIdentity || choice.Screen == screenCatalogProject) {
		action = choice.Option.Name
	}
	if action != "preferred-identity" && action != "refresh-relationships" {
		return ui.Transition{}, false
	}
	ref := catalog.Ref{Kind: catalog.Kind(choice.Screen), Name: choice.Option.Name}
	if parsed, err := decodeCatalogRef(choice.Option.Name); err == nil {
		ref = parsed
	} else if choice.Screen == ui.ScreenCatalogAction || choice.Screen == screenCatalogIdentity || choice.Screen == screenCatalogProject {
		ref, _ = decodeCatalogRef(catalogDraftEntity(draft))
	}
	encoded := encodeCatalogRef(ref)
	if action == "refresh-relationships" && ref.Kind == catalog.KindIdentity {
		return projectSearchPicker(cfg, saved, ref.Name), true
	}
	project := ref.Name
	target := ""
	if ref.Kind == catalog.KindKubernetes {
		target = ref.Name
		project = cfg.Kubernetes[target].Project
	}
	candidates := resolver.EligibleIdentities(cfg, project, target)
	title, screen, description := "Preferred identity", screenPreferredIdentity, "Choose a default from the available identities. Automatic uses the sole eligible identity, or asks when several are available."
	if action == "refresh-relationships" {
		candidates = resolver.EligibleIdentities(cfg, project, "")
		title = "Refresh clusters · choose identity"
		screen = screenRelationshipDiscovery
		description = "Discovery records the clusters visible to this specific identity. It does not grant access."
	}
	options := formatCatalogComponentOptions(catalog.KindIdentity, identityOptions(cfg, candidates))
	if action == "preferred-identity" {
		options = append([]ui.Option{{Name: "", Label: "Automatic", Detail: "clear the explicit preference"}}, options...)
	}
	if len(options) == 0 {
		description = "Discover projects from an identity first, then refresh this project's clusters."
	}
	return ui.Transition{Picker: ui.Picker{Screen: screen, Title: title, Description: description, Options: options, DisableEnter: len(options) == 0}, DraftUpdates: ui.Draft{screenCatalogList: encoded}}, true
}
