package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func catalogAddTransition(cfg config.Config, action string, _ ui.Draft, editor *catalogEditorState) ui.Transition {
	switch action {
	case "add-identity":
		picker := catalogInputPicker(screenAddIdentityName, "Add identity", "Name", "work-account", "A stable local name; credentials remain in the provider's isolated store.")
		picker.Input.Validate = catalogNameValidator(func(name string) bool { _, exists := cfg.Identities[name]; return exists })
		return ui.Transition{Picker: picker}
	case "add-project":
		options := formatCatalogComponentOptions(catalog.KindIdentity, identityOptions(cfg, nil))
		options = append(options, ui.Option{Name: "", Label: "No identity yet", Detail: "add manually as unverified and map an identity later"})
		return ui.Transition{Picker: ui.Picker{Screen: ui.ScreenDependencyTarget, Title: "Choose identity", Description: "Project enumeration uses only this identity's isolated gcloud credentials.", Dimension: "catalog-target", Options: options}}
	case "add-kubernetes":
		editor.gke = nil
		return ui.Transition{Picker: ui.Picker{Screen: ui.ScreenDependencyTarget, Title: "Add Kubernetes target", Dimension: "catalog-target", Options: []ui.Option{
			{Name: "gke", Label: "Google Kubernetes Engine", Detail: "enumerate by identity and project, or enter metadata manually"},
			{Name: "kubeconfig", Label: "Import kubeconfig context", Detail: "retain the source path and native context without changing it"},
		}}}
	case "add-docker":
		return ui.Transition{Picker: catalogInputPicker(screenAddDockerContext, "Add Docker context", "Context", "desktop-linux", "Enter an existing native Docker context. No cloud identity is inferred.")}
	case "add-workspace":
		editor.workspace = &workspaceEditorDraft{}
		return ui.Transition{Picker: ui.Picker{
			Screen: screenAddWorkspaceKind, Title: "Create workspace › Choose starting point", Dimension: "catalog-action", HideSearch: true,
			Description: "Choose the resource that best represents this workspace. ContextHop will infer compatible dependencies for review before anything is saved.",
			Options: []ui.Option{
				{Name: string(catalog.KindKubernetes), Label: "Kubernetes", Detail: "start from a cluster or kubeconfig context"},
				{Name: string(catalog.KindProject), Label: "Cloud project", Detail: "start from a project and its mapped identity"},
				{Name: string(catalog.KindDocker), Label: "Docker", Detail: "create a local or remote Docker workspace"},
				{Name: string(catalog.KindIdentity), Label: "Identity only", Detail: "create an account-only workspace"},
			},
		}}
	default:
		return catalogPreviewError(fmt.Errorf("unsupported add action %q", action), editor)
	}
}

func catalogAddStep(cfg config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) ui.Transition {
	action := draft[ui.ScreenCatalogAction]
	switch choice.Screen {
	case ui.ScreenDependencyTarget:
		switch action {
		case "add-project":
			return catalogProjectDiscoveryPicker(cfg, choice.Option.Name)
		case "add-kubernetes":
			if choice.Option.Name == "kubeconfig" {
				return ui.Transition{Picker: catalogInputPicker(screenAddKubeconfigPath, "Import kubeconfig context", "Kubeconfig", "~/.kube/config", "The source is read during activation; ContextHop never changes its current context.")}
			}
			editor.gke = &gkeEditorDraft{}
			return ui.Transition{Picker: ui.Picker{Screen: screenAddKubeIdentity, Title: "Add GKE target › Identity", Description: "Choose the account used to discover the project and fetch cluster credentials. The project mapping is staged until final confirmation.", Dimension: "identity", Options: formatCatalogComponentOptions(catalog.KindIdentity, identityOptions(cfg, nil))}}
		}
	case screenAddIdentityName:
		return ui.Transition{Picker: catalogInputPicker(screenAddIdentityAccount, "Add identity", "Google account", "person@example.com", "Authentication is checked only when the identity is used.")}
	case screenAddIdentityAccount:
		name := draft[screenAddIdentityName]
		identity := config.Identity{Provider: "gcp", Account: choice.Option.Name, CloudSDKConfig: identityCredentialDirectory(name), Provenance: "manual"}
		plan, err := catalog.PlanAddIdentity(cfg, name, identity)
		return catalogPreviewTransition(plan, err, editor)
	case screenAddProjectChoice:
		if choice.Option.Name == "\x00__manual__" {
			return ui.Transition{Picker: catalogInputPicker(screenAddProjectID, "Add project manually", "Project ID", "my-project-123", "The mapping is unverified until provider enumeration or activation succeeds.")}
		}
		return addProjectPlan(cfg, choice.Option.Name, draft[ui.ScreenDependencyTarget], "gcp", editor)
	case screenAddProjectID:
		return addProjectPlan(cfg, choice.Option.Name, draft[ui.ScreenDependencyTarget], "manual", editor)
	case screenAddKubeProject:
		if editor.gke == nil {
			return catalogPreviewError(errors.New("GKE draft was lost; start again"), editor)
		}
		if choice.Option.Name == "\x00__paste__" {
			return ui.Transition{Picker: catalogInputPicker(screenAddKubeCommand, "Add GKE target › Paste command", "Arguments", "example-sites-dev --region us-central1 --project example-project-123456 --dns-endpoint", "Paste a gcloud get-credentials command or just its cluster and flags. Nothing runs until the final confirmation.")}
		}
		editor.gke.Project = choice.Option.Name
		return catalogClusterDiscoveryPicker(cfg, choice.Option.Name, editor.gke.Identity)
	case screenAddKubeIdentity:
		if editor.gke == nil {
			return catalogPreviewError(errors.New("GKE draft was lost; start again"), editor)
		}
		editor.gke.Identity = choice.Option.Name
		return ui.Transition{Picker: gkeProjectPicker(cfg, editor.gke.Identity)}
	case screenAddKubeChoice:
		if editor.gke == nil {
			return catalogPreviewError(errors.New("GKE draft was lost; start again"), editor)
		}
		if strings.HasPrefix(choice.Option.Name, authenticateGKEPrefix) {
			identityName := strings.TrimPrefix(choice.Option.Name, authenticateGKEPrefix)
			return ui.Transition{Picker: providerAuthPicker(cfg, "gke", identityName, editor.gke.Project)}
		}
		if strings.HasPrefix(choice.Option.Name, retryGKEPrefix) {
			return catalogClusterDiscoveryPicker(cfg, editor.gke.Project, editor.gke.Identity)
		}
		if choice.Option.Name == "\x00__manual__" {
			return ui.Transition{Picker: catalogInputPicker(screenAddKubeLocation, "Add GKE target manually", "Location", "us-central1", "Use the provider region or zone.")}
		}
		cluster, location, ok := strings.Cut(choice.Option.Name, "\x00")
		if !ok {
			return catalogPreviewError(errors.New("invalid discovered cluster selection"), editor)
		}
		editor.gke.Cluster, editor.gke.Location, editor.gke.Provenance = cluster, location, "gcp"
		return ui.Transition{Picker: gkeEndpointPicker()}
	case screenAddKubeLocation:
		return ui.Transition{Picker: catalogInputPicker(screenAddKubeCluster, "Add GKE target manually", "Cluster", "cluster-name", "The provider project cannot be remapped after this target is added.")}
	case screenAddKubeCluster:
		if editor.gke == nil {
			return catalogPreviewError(errors.New("GKE draft was lost; start again"), editor)
		}
		editor.gke.Cluster, editor.gke.Location, editor.gke.Provenance = choice.Option.Name, draft[screenAddKubeLocation], "manual"
		return ui.Transition{Picker: gkeEndpointPicker()}
	case screenAddKubeCommand:
		if editor.gke == nil {
			return catalogPreviewError(errors.New("GKE draft was lost; start again"), editor)
		}
		parsed, err := parseGKEArguments(choice.Option.Name)
		if err != nil {
			return catalogPreviewError(err, editor)
		}
		projectName := configuredProjectName(cfg, parsed.ProjectID)
		if projectName == "" {
			return catalogPreviewError(fmt.Errorf("project %q is not in the catalog; add the project first", parsed.ProjectID), editor)
		}
		editor.gke.Project, editor.gke.Cluster, editor.gke.Location = projectName, parsed.Cluster, parsed.Location
		editor.gke.Endpoint, editor.gke.Provenance = parsed.Endpoint, "manual"
		return ui.Transition{Picker: gkeReviewPicker(cfg, editor.gke)}
	case screenAddKubeEndpoint:
		if editor.gke == nil {
			return catalogPreviewError(errors.New("GKE draft was lost; start again"), editor)
		}
		editor.gke.Endpoint = choice.Option.Name
		if editor.gke.Endpoint == "\x00__external__" {
			editor.gke.Endpoint = ""
		}
		return ui.Transition{Picker: gkeReviewPicker(cfg, editor.gke)}
	case screenAddKubeReview:
		if editor.gke == nil {
			return catalogPreviewError(errors.New("GKE draft was lost; start again"), editor)
		}
		if choice.Option.Name == "save" {
			return addGKEPlan(cfg, editor.gke, editor)
		}
	case screenAddKubeconfigPath:
		return ui.Transition{Picker: catalogInputPicker(screenAddKubeContext, "Import kubeconfig context", "Context", "my-context", "The context's underlying cluster is resolved during local discovery and activation.")}
	case screenAddKubeContext:
		return addKubeconfigPlan(cfg, draft[screenAddKubeconfigPath], choice.Option.Name, editor)
	case screenAddDockerContext:
		name := uniqueCatalogName(cfg.Docker, choice.Option.Name)
		plan, err := catalog.PlanAddDocker(cfg, name, config.Docker{Context: choice.Option.Name, Provenance: "manual"})
		return catalogPreviewTransition(plan, err, editor)
	case screenAddWorkspaceKind:
		if editor.workspace == nil {
			editor.workspace = &workspaceEditorDraft{}
		}
		editor.workspace.Editing = catalog.Kind(choice.Option.Name)
		return ui.Transition{Picker: workspaceComponentPicker(cfg, editor.workspace, screenAddWorkspaceTarget)}
	case screenAddWorkspaceTarget:
		if editor.workspace == nil {
			return catalogPreviewError(errors.New("workspace draft was lost; start creation again"), editor)
		}
		setWorkspaceDraftComponent(&editor.workspace.Value, editor.workspace.Editing, choice.Option.Name)
		if editor.workspace.Name == "" {
			editor.workspace.Name = uniqueCatalogName(cfg.Destinations, choice.Option.Name)
		}
		return ui.Transition{ReturnToPrevious: true, Picker: workspaceBuilderPicker(cfg, editor.workspace)}
	case screenAddWorkspaceBuilder:
		if editor.workspace == nil {
			return catalogPreviewError(errors.New("workspace draft was lost; start creation again"), editor)
		}
		switch {
		case choice.Option.Name == "workspace-name":
			if editor.workspace.Existing {
				return ui.Transition{ReturnToPrevious: true, Picker: workspaceBuilderPicker(cfg, editor.workspace)}
			}
			picker := catalogInputPicker(screenAddWorkspaceName, "Create workspace › Name", "New name", editor.workspace.Name, "Current name: "+editor.workspace.Name+". Enter a replacement stable name, or press Esc to keep it.")
			picker.Input.Validate = catalogNameValidator(func(name string) bool { _, exists := cfg.Destinations[name]; return exists })
			return ui.Transition{Picker: picker}
		case strings.HasPrefix(choice.Option.Name, "workspace-set:"):
			editor.workspace.Editing = catalog.Kind(strings.TrimPrefix(choice.Option.Name, "workspace-set:"))
			return ui.Transition{Picker: workspaceComponentPicker(cfg, editor.workspace, screenAddWorkspaceComponent)}
		case choice.Option.Name == "workspace-adc":
			return ui.Transition{Picker: workspaceADCPicker()}
		case choice.Option.Name == "workspace-copy":
			return selectionWorkspaceNamePicker(cfg, editor)
		case choice.Option.Name == "workspace-create":
			workspace := editor.workspace.Value
			workspace.Provenance = "manual"
			plan, err := catalog.PlanAddWorkspace(cfg, editor.workspace.Name, workspace)
			return workspaceEditorPreview(plan, err, editor)
		case choice.Option.Name == "workspace-save":
			plan, err := catalog.PlanUpdateWorkspace(cfg, editor.workspace.Name, editor.workspace.Value)
			return workspaceEditorPreview(plan, err, editor)
		case choice.Option.Name == "workspace-hide":
			plan, err := catalog.PlanSetHidden(cfg, catalog.Ref{Kind: catalog.KindWorkspace, Name: editor.workspace.Name}, true)
			return catalogVisibilityTransition(plan, err, editor)
		case choice.Option.Name == "workspace-delete":
			plan, err := catalog.PlanRemove(cfg, catalog.Ref{Kind: catalog.KindWorkspace, Name: editor.workspace.Name})
			return catalogPreviewTransition(plan, err, editor)
		}
	case screenAddWorkspaceName:
		if editor.workspace == nil {
			return catalogPreviewError(errors.New("workspace draft was lost; start creation again"), editor)
		}
		editor.workspace.Name = choice.Option.Name
		return ui.Transition{ReturnToPrevious: true, Picker: workspaceBuilderPicker(cfg, editor.workspace)}
	case screenAddWorkspaceComponent:
		if editor.workspace == nil {
			return catalogPreviewError(errors.New("workspace draft was lost; start creation again"), editor)
		}
		setWorkspaceDraftComponent(&editor.workspace.Value, editor.workspace.Editing, choice.Option.Name)
		return ui.Transition{ReturnToPrevious: true, Picker: workspaceBuilderPicker(cfg, editor.workspace)}
	case screenAddWorkspaceADC:
		if editor.workspace == nil {
			return catalogPreviewError(errors.New("workspace draft was lost; start creation again"), editor)
		}
		editor.workspace.Value.ADC = choice.Option.Name
		return ui.Transition{ReturnToPrevious: true, Picker: workspaceBuilderPicker(cfg, editor.workspace)}

	}
	return catalogPreviewError(fmt.Errorf("unsupported add step %q", choice.Screen), editor)
}

func catalogInputPicker(screen ui.Screen, title, prompt, placeholder, description string) ui.Picker {
	return ui.Picker{Screen: screen, Title: title, Description: description, Dimension: "catalog-input", Input: &ui.Input{Prompt: prompt, Placeholder: placeholder}}
}

func catalogNameValidator(exists func(string) bool) func(string) error {
	return func(name string) error {
		if err := config.ValidateName(name); err != nil {
			return fmt.Errorf("name %s", err)
		}
		if exists(name) {
			return fmt.Errorf("name %q already exists", name)
		}
		return nil
	}
}

// Keep existing simple directory names compatible; encode display names so path
// separators and punctuation never become filesystem structure. The underscore
// prefix separates encoded names from every legacy identifier.
func identityCredentialDirectory(name string) string {
	simple := name != ""
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			simple = false
			break
		}
	}
	if !simple {
		name = fmt.Sprintf("_name-%x", sha256.Sum256([]byte(name)))
	}
	return "~/.config/contexthop/gcloud/" + name
}
