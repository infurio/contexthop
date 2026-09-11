package main

import (
	"strings"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

type catalogEditorState struct {
	workspaceReview    bool
	applyImmediately   bool
	savingProject      bool
	selectionWorkspace string // Workspace targeted by the current save plan only.
	plan               *catalog.Plan
	err                error
	workspace          *workspaceEditorDraft
	gke                *gkeEditorDraft
}

type gkeEditorDraft struct {
	Identity   string
	Project    string
	Cluster    string
	Location   string
	Endpoint   string
	Provenance string
	Existing   string
}

type workspaceEditorDraft struct {
	SelectAfterSave bool
	Name            string
	Value           config.Destination
	Editing         catalog.Kind
	Existing        bool
	Notice          string
}

func interactiveFlow(cfg, catalogCfg config.Config, editor *catalogEditorState) ui.FlowFunc {
	return func(choice ui.Choice, draft ui.Draft) ui.Transition {
		if draft == nil {
			draft = ui.Draft{}
		}

		if transition, ok := browserConfigFlow(catalogCfg, choice, draft, editor); ok {
			return transition
		}
		if choice.Screen == "discovery-setup" {
			if choice.Option.Name == "add-identity" {
				tr := catalogAddTransition(catalogCfg, "add-identity", draft, editor)
				tr.DraftUpdates = ui.Draft{ui.ScreenCatalogAction: "add-identity"}
				return tr
			}
			choice.Action = choice.Option.Name
		}
		if tr, ok := localImportFlow(catalogCfg, choice, editor); ok {
			return tr
		}
		if tr, handled := shellMenuFlow(cfg, choice, draft); handled {
			return tr
		}
		if choice.Screen == screenShellMenu && (choice.Option.Name == "default-shell" || choice.Option.Name == "launch-shell" || choice.Option.Name == "apply-shell") {
			choice.Action = choice.Option.Name
		}

		if tr, ok := labelsFlow(catalogCfg, choice, draft, editor); ok {
			return tr
		}
		if choice.Action == "identity-auth" || choice.Option.Name == "identity-auth" {
			name := choice.Option.Name
			if ref, err := decodeCatalogRef(name); err == nil {
				name = ref.Name
			}
			if name == "identity-auth" {
				if ref, err := decodeCatalogRef(catalogDraftEntity(draft)); err == nil {
					name = ref.Name
				}
			}
			return ui.Transition{Picker: providerAuthPicker(cfg, "identity", name, "")}
		}
		if transition, handled := discoveryDialogFlow(cfg, catalogCfg, choice, draft); handled {
			return transition
		}
		if transition, handled := relationshipAction(cfg, catalogCfg, choice, draft, editor); handled {
			return transition
		}
		if transition, handled := selectionWorkspaceFlow(cfg, catalogCfg, choice, draft, editor); handled {
			return transition
		}
		if choice.Action == "follow-shared" {
			return ui.Transition{Complete: true, CompletionChoice: &choice}
		}
		if choice.Screen == screenResolveIdentity || choice.Action == "default-shell" || choice.Action == "launch-shell" || choice.Action == "apply-shell" {
			return launchFlow(cfg, catalogCfg, choice, draft)
		}
		if choice.Action == "add-resource" {
			action := "add-" + string(choice.Screen)
			transition := catalogAddTransition(catalogCfg, action, draft, editor)
			transition.DraftUpdates = ui.Draft{ui.ScreenCatalogAction: action}
			return transition
		}
		if strings.HasPrefix(string(choice.Screen), "catalog-add-") {
			return catalogAddStep(catalogCfg, choice, draft, editor)
		}
		switch choice.Screen {
		case ui.ScreenWorkspace, ui.ScreenDocker, ui.ScreenReuse, ui.ScreenKubernetes, ui.ScreenProject, ui.ScreenIdentity:
			return resourceSelectionFlow(cfg, catalogCfg, choice, draft, editor)
		case screenProjectSearchIdentity, screenProjectSearchResults:
			return projectSearchFlow(cfg, catalogCfg, choice, draft, editor)
		case ui.ScreenCatalogEntity, screenCatalogList, screenCatalogIdentity, screenCatalogIdentityMap,
			screenCatalogIdentityUnmap, screenCatalogProjectMap, screenCatalogProjectUnmap,
			screenCatalogKubernetesProject, screenCatalogKubernetesIdentity, screenCatalogDockerMap,
			screenCatalogDockerUnmap, screenCatalogProject, ui.ScreenCatalogAction, ui.ScreenDependencyTarget, ui.ScreenConfirm:
			return catalogFlow(cfg, catalogCfg, choice, draft, editor)
		case screenProviderAuth:
			return providerAuthTransition(cfg, catalogCfg, choice.Option.Name, editor, draft)
		default:
			return ui.Transition{Complete: true}
		}
	}
}

func resourceBrowserActionTransition(cfg config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) ui.Transition {
	kind := catalog.Kind("")
	switch choice.Screen {
	case ui.ScreenIdentity:
		kind = catalog.KindIdentity
	case ui.ScreenProject:
		kind = catalog.KindProject
	case ui.ScreenKubernetes:
		kind = catalog.KindKubernetes
	case ui.ScreenDocker:
		kind = catalog.KindDocker
	case ui.ScreenWorkspace:
		kind = catalog.KindWorkspace
	}
	encoded := encodeCatalogRef(catalog.Ref{Kind: kind, Name: choice.Option.Name})
	updates := ui.Draft{screenCatalogList: encoded}
	draft[screenCatalogList] = encoded
	var transition ui.Transition
	switch choice.Action {
	case "kubernetes-map":
		target := cfg.Kubernetes[choice.Option.Name]
		if target.Type == "gke" || target.Project == "" {
			choice.Action = "kubernetes-map-identity"
			if target.Type != "gke" {
				choice.Action = "kubernetes-set-project"
			}
			return resourceBrowserActionTransition(cfg, choice, draft, editor)
		}
		transition = catalogActionTransition(cfg, choice.Action, draft, editor)
	case "identity-map-project":
		picker := catalogDependencyPicker(cfg, encoded, choice.Action)
		picker.Screen = screenCatalogIdentityMap
		transition = ui.Transition{Picker: picker}
	case "identity-unmap-project":
		transition = ui.Transition{Picker: catalogIdentityUnmapPicker(cfg, encoded)}
	case "project-map-identity":
		picker := catalogDependencyPicker(cfg, encoded, choice.Action)
		picker.Screen = screenCatalogProjectMap
		transition = ui.Transition{Picker: picker}
	case "project-unmap-identity":
		transition = ui.Transition{Picker: catalogProjectUnmapPicker(cfg, encoded)}
	case "kubernetes-set-project":
		picker := catalogDependencyPicker(cfg, encoded, choice.Action)
		picker.Screen = screenCatalogKubernetesProject
		transition = ui.Transition{Picker: picker}
	case "kubernetes-map-identity":
		picker := catalogDependencyPicker(cfg, encoded, choice.Action)
		picker.Screen = screenCatalogKubernetesIdentity
		transition = ui.Transition{Picker: picker}
	case "docker-map-workspace":
		picker := catalogDependencyPicker(cfg, encoded, choice.Action)
		picker.Screen = screenCatalogDockerMap
		transition = ui.Transition{Picker: picker}
	case "docker-unmap-workspace":
		transition = ui.Transition{Picker: catalogDockerUnmapPicker(cfg, encoded)}
	case "workspace-copy":
		editor.workspace = &workspaceEditorDraft{Name: choice.Option.Name, Value: cfg.Destinations[choice.Option.Name]}
		transition = selectionWorkspaceNamePicker(cfg, editor)
	case "workspace-configure":
		transition = ui.Transition{Picker: workspaceEditPicker(cfg, choice.Option.Name, editor, "")}
	default:
		transition = catalogActionTransition(cfg, choice.Action, draft, editor)
	}
	transition.DraftUpdates = updates
	return transition
}

const screenResolveIdentity ui.Screen = "resolve-selection-identity"

const catalogAddSelection = "\x00__catalog_add__"
const createWorkspaceSelection = "\x00__create_workspace__"
const catalogCategoryPrefix = "\x00__catalog_category__:"
const searchProjectsSelection = "\x00__search_projects__"
const authenticateProjectSearchPrefix = "\x00__authenticate_project_search__:"
const retryProjectSearchPrefix = "\x00__retry_project_search__:"
const authenticateGKEPrefix = "\x00__authenticate_gke__:"
const retryGKEPrefix = "\x00__retry_gke__:"
const providerAuthPrefix = "\x00__provider_auth__:"

const (
	screenProjectSearchIdentity     ui.Screen = "project-search-identity"
	screenProjectSearchResults      ui.Screen = "project-search-results"
	screenCatalogList               ui.Screen = "catalog-list"
	screenCatalogIdentity           ui.Screen = "catalog-identity"
	screenCatalogIdentityMap        ui.Screen = "catalog-identity-map"
	screenCatalogIdentityUnmap      ui.Screen = "catalog-identity-unmap"
	screenCatalogProjectMap         ui.Screen = "catalog-project-map"
	screenCatalogProjectUnmap       ui.Screen = "catalog-project-unmap"
	screenCatalogKubernetesProject  ui.Screen = "catalog-kubernetes-project"
	screenCatalogKubernetesIdentity ui.Screen = "catalog-kubernetes-identity"
	screenCatalogDockerMap          ui.Screen = "catalog-docker-map"
	screenCatalogDockerUnmap        ui.Screen = "catalog-docker-unmap"
	screenCatalogProject            ui.Screen = "catalog-project"
	screenAddIdentityName           ui.Screen = "catalog-add-identity-name"
	screenAddIdentityAccount        ui.Screen = "catalog-add-identity-account"
	screenAddProjectChoice          ui.Screen = "catalog-add-project-choice"
	screenAddProjectID              ui.Screen = "catalog-add-project-id"
	screenAddKubeProject            ui.Screen = "catalog-add-kubernetes-project"
	screenAddKubeIdentity           ui.Screen = "catalog-add-kubernetes-identity"
	screenAddKubeChoice             ui.Screen = "catalog-add-kubernetes-choice"
	screenAddKubeLocation           ui.Screen = "catalog-add-kubernetes-location"
	screenAddKubeCluster            ui.Screen = "catalog-add-kubernetes-cluster"
	screenAddKubeCommand            ui.Screen = "catalog-add-kubernetes-command"
	screenAddKubeEndpoint           ui.Screen = "catalog-add-kubernetes-endpoint"
	screenAddKubeReview             ui.Screen = "catalog-add-kubernetes-review"
	screenProviderAuth              ui.Screen = "provider-auth"
	screenAddKubeconfigPath         ui.Screen = "catalog-add-kubeconfig-path"
	screenAddKubeContext            ui.Screen = "catalog-add-kubeconfig-context"
	screenAddDockerContext          ui.Screen = "catalog-add-docker-context"
	screenAddWorkspaceName          ui.Screen = "catalog-add-workspace-name"
	screenAddWorkspaceKind          ui.Screen = "catalog-add-workspace-kind"
	screenAddWorkspaceTarget        ui.Screen = "catalog-add-workspace-target"
	screenAddWorkspaceBuilder       ui.Screen = "catalog-add-workspace-builder"
	screenAddWorkspaceComponent     ui.Screen = "catalog-add-workspace-component"
	screenAddWorkspaceADC           ui.Screen = "catalog-add-workspace-adc"
	screenAddWorkspaceRisk          ui.Screen = "catalog-add-workspace-risk"
)
