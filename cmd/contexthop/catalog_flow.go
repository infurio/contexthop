package main

import (
	"fmt"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
	"strings"
)

func catalogFlow(cfg, catalogCfg config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) ui.Transition {
	switch choice.Screen {
	case ui.ScreenCatalogEntity:
		if choice.Option.Name == catalogAddSelection {
			return ui.Transition{Picker: catalogActionPicker(catalogCfg, choice.Option.Name)}
		}
		if strings.HasPrefix(choice.Option.Name, catalogCategoryPrefix) {
			return ui.Transition{Picker: catalogCategoryPicker(catalogCfg, strings.TrimPrefix(choice.Option.Name, catalogCategoryPrefix))}
		}
		ref, err := decodeCatalogRef(choice.Option.Name)
		if err != nil {
			return ui.Transition{Picker: catalogActionPicker(catalogCfg, choice.Option.Name)}
		}
		switch ref.Kind {
		case catalog.KindIdentity:
			return ui.Transition{Picker: catalogIdentityPicker(catalogCfg, ref)}
		case catalog.KindProject:
			return ui.Transition{Picker: catalogProjectPicker(catalogCfg, ref)}
		case catalog.KindWorkspace:
			return ui.Transition{Picker: workspaceEditPicker(catalogCfg, ref.Name, editor, "")}
		default:
			return ui.Transition{Picker: catalogActionPicker(catalogCfg, choice.Option.Name)}
		}
	case screenCatalogList:
		if choice.Action != "" {
			switch choice.Action {
			case "identity-map-project":
				picker := catalogDependencyPicker(catalogCfg, choice.Option.Name, choice.Action)
				picker.Screen = screenCatalogIdentityMap
				return ui.Transition{Picker: picker}
			case "identity-unmap-project":
				return ui.Transition{Picker: catalogIdentityUnmapPicker(catalogCfg, choice.Option.Name)}
			case "project-map-identity":
				picker := catalogDependencyPicker(catalogCfg, choice.Option.Name, choice.Action)
				picker.Screen = screenCatalogProjectMap
				return ui.Transition{Picker: picker}
			case "project-unmap-identity":
				return ui.Transition{Picker: catalogProjectUnmapPicker(catalogCfg, choice.Option.Name)}
			case "kubernetes-set-project":
				picker := catalogDependencyPicker(catalogCfg, choice.Option.Name, choice.Action)
				picker.Screen = screenCatalogKubernetesProject
				return ui.Transition{Picker: picker}
			case "kubernetes-map-identity":
				picker := catalogDependencyPicker(catalogCfg, choice.Option.Name, choice.Action)
				picker.Screen = screenCatalogKubernetesIdentity
				return ui.Transition{Picker: picker}
			case "docker-map-workspace":
				picker := catalogDependencyPicker(catalogCfg, choice.Option.Name, choice.Action)
				picker.Screen = screenCatalogDockerMap
				return ui.Transition{Picker: picker}
			case "docker-unmap-workspace":
				return ui.Transition{Picker: catalogDockerUnmapPicker(catalogCfg, choice.Option.Name)}
			case "workspace-configure":
				ref, err := decodeCatalogRef(choice.Option.Name)
				if err != nil || ref.Kind != catalog.KindWorkspace {
					return catalogPreviewError(fmt.Errorf("invalid workspace selection %q", choice.Option.Name), editor)
				}
				return ui.Transition{Picker: workspaceEditPicker(catalogCfg, ref.Name, editor, "")}
			default:
				return catalogActionTransition(catalogCfg, choice.Action, draft, editor)
			}
		}
		ref, err := decodeCatalogRef(choice.Option.Name)
		if err != nil {
			return ui.Transition{Picker: catalogActionPicker(catalogCfg, choice.Option.Name)}
		}
		switch ref.Kind {
		case catalog.KindIdentity:
			return ui.Transition{Picker: catalogIdentityPicker(catalogCfg, ref)}
		case catalog.KindProject:
			return ui.Transition{Picker: catalogProjectPicker(catalogCfg, ref)}
		case catalog.KindWorkspace:
			return ui.Transition{Picker: workspaceEditPicker(catalogCfg, ref.Name, editor, "")}
		default:
			return ui.Transition{Picker: catalogActionPicker(catalogCfg, choice.Option.Name)}
		}
	case screenCatalogIdentity:
		ref, err := decodeCatalogRef(choice.Option.Name)
		if err == nil && ref.Kind == catalog.KindProject {
			return ui.Transition{Picker: catalogProjectPicker(catalogCfg, ref)}
		}
		return catalogInlineActionTransition(catalogCfg, choice.Option.Name, draft, editor)
	case screenCatalogIdentityMap:
		return catalogPlanTransition(catalogCfg, catalogDraftEntity(draft), "identity-map-project", choice.Option.Name, editor)
	case screenCatalogIdentityUnmap:
		return catalogPlanTransition(catalogCfg, catalogDraftEntity(draft), "identity-remove-project:"+choice.Option.Name, "", editor)
	case screenCatalogProjectMap:
		return catalogPlanTransition(catalogCfg, catalogDraftEntity(draft), "project-map-identity", choice.Option.Name, editor)
	case screenCatalogProjectUnmap:
		return catalogPlanTransition(catalogCfg, catalogDraftEntity(draft), "project-remove-identity:"+choice.Option.Name, choice.Option.Name, editor)
	case screenCatalogKubernetesProject:
		return catalogPlanTransition(catalogCfg, catalogDraftEntity(draft), "kubernetes-set-project", choice.Option.Name, editor)
	case screenCatalogKubernetesIdentity:
		return catalogPlanTransition(catalogCfg, catalogDraftEntity(draft), "kubernetes-map-identity", choice.Option.Name, editor)
	case screenCatalogDockerMap:
		return catalogPlanTransition(catalogCfg, catalogDraftEntity(draft), "docker-map-workspace", choice.Option.Name, editor)
	case screenCatalogDockerUnmap:
		return catalogPlanTransition(catalogCfg, catalogDraftEntity(draft), "docker-remove-workspace:"+choice.Option.Name, "", editor)
	case screenCatalogProject:
		ref, err := decodeCatalogRef(choice.Option.Name)
		if err == nil && ref.Kind == catalog.KindKubernetes {
			return ui.Transition{Picker: catalogActionPicker(catalogCfg, choice.Option.Name)}
		}
		return catalogInlineActionTransition(catalogCfg, choice.Option.Name, draft, editor)
	case ui.ScreenCatalogAction:
		return catalogActionTransition(catalogCfg, choice.Option.Name, draft, editor)
	case ui.ScreenDependencyTarget:
		action := catalogDraftAction(draft)
		if strings.HasPrefix(action, "add-") {
			return catalogAddStep(catalogCfg, choice, draft, editor)
		}
		return catalogPlanTransition(catalogCfg, catalogDraftEntity(draft), action, choice.Option.Name, editor)
	case ui.ScreenConfirm:
		return ui.Transition{Complete: true}

	}
	return ui.Transition{Complete: true}
}
