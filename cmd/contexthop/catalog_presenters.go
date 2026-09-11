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

func catalogNodeSummary(cfg config.Config, ref catalog.Ref) string {
	counts := map[catalog.Kind]int{}
	for _, related := range catalog.Reverse(cfg, ref).Children {
		counts[related.Node.Ref.Kind]++
	}
	if ref.Kind == catalog.KindIdentity {
		projects := map[string]bool{}
		for name := range cfg.Projects {
			if resolver.ProjectIdentityAvailable(cfg, name, ref.Name) {
				projects[name] = true
			}
		}
		for name, target := range cfg.Kubernetes {
			if projects[target.Project] && resolver.KubernetesIdentityAvailable(cfg, name, ref.Name) {
				counts[catalog.KindKubernetes]++
			}
		}
	}

	var order []catalog.Kind
	switch ref.Kind {
	case catalog.KindIdentity:
		order = []catalog.Kind{catalog.KindProject, catalog.KindKubernetes, catalog.KindWorkspace}
	case catalog.KindProject:
		order = []catalog.Kind{catalog.KindIdentity, catalog.KindKubernetes, catalog.KindWorkspace}
	case catalog.KindKubernetes, catalog.KindDocker:
		order = []catalog.Kind{catalog.KindWorkspace}
	case catalog.KindWorkspace:
		order = []catalog.Kind{catalog.KindIdentity, catalog.KindProject, catalog.KindKubernetes, catalog.KindDocker}
	}
	var parts []string
	if ref.Kind == catalog.KindProject && len(resolver.EligibleIdentities(cfg, ref.Name, "")) == 0 {
		parts = append(parts, "Needs identity")
	}
	if ref.Kind == catalog.KindKubernetes && cfg.Kubernetes[ref.Name].Project == "" {
		parts = append(parts, "No project")
	}
	if ref.Kind == catalog.KindDocker {
		parts = append(parts, "No cloud dependency")
	}
	for _, kind := range order {
		if count := counts[kind]; count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count, catalogCountLabel(kind, count)))
		}
	}
	if len(parts) == 0 {
		if ref.Kind == catalog.KindIdentity {
			return "No projects"
		}
		if ref.Kind == catalog.KindKubernetes || ref.Kind == catalog.KindDocker {
			return "No workspace"
		}
		return "UNMAPPED"
	}
	return strings.Join(parts, " · ")
}

func catalogCountLabel(kind catalog.Kind, count int) string {
	singular, plural := string(kind), string(kind)+"s"
	switch kind {
	case catalog.KindIdentity:
		plural = "identities"
	case catalog.KindKubernetes:
		singular, plural = "Kubernetes", "Kubernetes"
	case catalog.KindDocker:
		singular, plural = "Docker target", "Docker targets"
	}
	if count == 1 {
		return singular
	}
	return plural
}

func catalogNodeDetail(cfg config.Config, ref catalog.Ref) string {
	related := catalog.Reverse(cfg, ref).Children
	parts := make([]string, 0, len(related))
	for _, item := range related {
		parts = append(parts, string(item.Node.Ref.Kind)+":"+item.Node.Label)
	}
	if len(related) == 0 {
		parts = append(parts, "unmapped")
	}
	mapping := "Mappings: " + strings.Join(parts, " · ")
	if ref.Kind == catalog.KindKubernetes && cfg.Kubernetes[ref.Name].Project == "" {
		mapping = "Project: none (standalone)\n" + mapping
	}
	if ref.Kind == catalog.KindDocker {
		mapping = "Cloud dependency: none\n" + mapping
	}
	return catalogMetadata(cfg, ref) + "\n" + mapping + relationshipDetail(cfg, ref)
}

func catalogMetadata(cfg config.Config, ref catalog.Ref) string {
	provenance, verifiedBy, observedAt, endpoint := "", "", "", ""
	switch ref.Kind {
	case catalog.KindIdentity:
		item := cfg.Identities[ref.Name]
		provenance, verifiedBy, observedAt = item.Provenance, item.VerifiedBy, item.ObservedAt
	case catalog.KindProject:
		item := cfg.Projects[ref.Name]
		provenance, verifiedBy, observedAt = item.Provenance, item.VerifiedBy, item.ObservedAt
	case catalog.KindKubernetes:
		item := cfg.Kubernetes[ref.Name]
		provenance, verifiedBy, observedAt = item.Provenance, item.VerifiedBy, item.ObservedAt
		if item.Type == "gke" {
			endpoint = gkeEndpointLabel(item.Endpoint)
		}
	case catalog.KindDocker:
		item := cfg.Docker[ref.Name]
		provenance, verifiedBy, observedAt = item.Provenance, item.VerifiedBy, item.ObservedAt
	case catalog.KindWorkspace:
		item := cfg.Destinations[ref.Name]
		provenance, verifiedBy, observedAt = item.Provenance, item.VerifiedBy, item.ObservedAt
	}
	if provenance == "" {
		provenance = "legacy/unknown"
	}
	parts := []string{"source:" + strings.ToUpper(provenance)}
	if verifiedBy != "" {
		parts = append(parts, "verified:"+strings.ToUpper(verifiedBy))
	}
	if observedAt != "" {
		parts = append(parts, "observed:"+observedAt)
	}
	if endpoint != "" {
		parts = append(parts, "endpoint:"+endpoint)
	}
	if catalogRefHidden(cfg, ref) {
		parts = append(parts, "visibility:HIDDEN")
	}
	return strings.Join(parts, " ")
}

func encodeCatalogRef(ref catalog.Ref) string { return string(ref.Kind) + ":" + ref.Name }

func decodeCatalogRef(value string) (catalog.Ref, error) {
	kind, name, ok := strings.Cut(value, ":")
	if !ok || name == "" {
		return catalog.Ref{}, fmt.Errorf("invalid catalog selection %q", value)
	}
	ref := catalog.Ref{Kind: catalog.Kind(kind), Name: name}
	switch ref.Kind {
	case catalog.KindIdentity, catalog.KindProject, catalog.KindKubernetes, catalog.KindDocker, catalog.KindWorkspace:
		return ref, nil
	default:
		return catalog.Ref{}, fmt.Errorf("unsupported catalog kind %q", kind)
	}
}

func catalogActionPicker(cfg config.Config, encoded string) ui.Picker {
	if encoded == catalogAddSelection {
		return ui.Picker{
			Screen: ui.ScreenCatalogAction, Title: "Catalog › New resource", Dimension: "catalog-action",
			Description: "Choose what to add. Provider enumeration is scoped to the identity you select.",
			Options: []ui.Option{
				{Name: "add-identity", Label: "Identity", Detail: "add a Google account without storing credentials"},
				{Name: "add-project", Label: "Cloud project", Detail: "add manually or from identity-scoped discovery"},
				{Name: "add-kubernetes", Label: "Kubernetes", Detail: "import kubeconfig metadata or add a GKE target"},
				{Name: "add-docker", Label: "Docker", Detail: "add a native Docker context"},
				{Name: "add-workspace", Label: "Workspace", Detail: "create a bundle, then map more components"},
			},
		}
	}
	ref, err := decodeCatalogRef(encoded)
	if err != nil {
		return ui.Picker{Screen: ui.ScreenCatalogAction, Title: "Invalid catalog entry", Description: err.Error(), Dimension: "catalog-action"}
	}
	options := catalogActionOptions(cfg, ref)
	return ui.Picker{
		Screen: ui.ScreenCatalogAction, Title: "Catalog › " + catalogRefLabel(cfg, ref),
		Description: catalogNodeDetail(cfg, ref), Dimension: "catalog-action", Options: options,
	}
}

func catalogActionOptions(cfg config.Config, ref catalog.Ref) []ui.Option {
	if catalogRefHidden(cfg, ref) {
		return []ui.Option{
			{Name: "unhide-entity", Label: "Unhide", Detail: "return this resource to normal catalog lists and mapping choices"},
			{Name: "remove-entity", Label: "Delete from ContextHop", Detail: "remove only the local record; never delete the external resource"},
		}
	}
	options := []ui.Option{}
	switch ref.Kind {
	case catalog.KindIdentity, catalog.KindProject, catalog.KindKubernetes:
		for _, action := range catalogRowActions(cfg, ref) {
			if action.Action == "hide-entity" || action.Action == "remove-entity" {
				continue
			}
			options = append(options, ui.Option{Name: action.Action, Label: action.Label})
		}
	case catalog.KindDocker:
		options = append(options, ui.Option{Name: "docker-map-workspace", Label: "Map to workspace", Detail: "Docker has no cloud identity dependency by default"})
		workspaceNames := make([]string, 0, len(cfg.Destinations))
		for name, workspace := range cfg.Destinations {
			if workspace.Docker == ref.Name {
				workspaceNames = append(workspaceNames, name)
			}
		}
		slices.Sort(workspaceNames)
		for _, name := range workspaceNames {
			options = append(options, ui.Option{Name: "docker-remove-workspace:" + name, Label: "Unmap from workspace", Detail: name})
		}
	case catalog.KindWorkspace:
		for _, kind := range []catalog.Kind{catalog.KindIdentity, catalog.KindProject, catalog.KindKubernetes, catalog.KindDocker} {
			options = append(options, ui.Option{Name: "workspace-set:" + string(kind), Label: "Set " + string(kind), Detail: "map, remap, or remove this workspace component"})
			if component, componentErr := workspaceComponentName(cfg.Destinations[ref.Name], kind); componentErr == nil && component != "" {
				options = append(options, ui.Option{Name: "workspace-remove:" + string(kind), Label: "Unmap " + string(kind), Detail: component})
			}
		}
	}
	if ref.Kind != catalog.KindWorkspace {
		options = append(options, ui.Option{Name: "hide-entity", Label: "Hide", Detail: "keep mappings and discovery updates, but suppress normal catalog choices"})
	}
	options = append(options, ui.Option{Name: "remove-entity", Label: "Delete from ContextHop", Detail: "remove only the local record; never delete the external resource"})
	return options
}

func catalogForgetLabel(kind catalog.Kind) string {
	switch kind {
	case catalog.KindKubernetes:
		return "Forget Kubernetes"
	case catalog.KindDocker:
		return "Forget Docker context"
	default:
		return "Forget " + string(kind)
	}
}

func catalogRefLabel(cfg config.Config, ref catalog.Ref) string {
	switch ref.Kind {
	case catalog.KindIdentity:
		return "Identities › " + cfg.Identities[ref.Name].Account
	case catalog.KindProject:
		return "Projects › " + cfg.Projects[ref.Name].ProjectID
	case catalog.KindKubernetes:
		item := cfg.Kubernetes[ref.Name]
		return "Kubernetes › " + firstNonEmpty(item.Cluster, item.Context, ref.Name)
	case catalog.KindDocker:
		return "Docker › " + cfg.Docker[ref.Name].Context
	case catalog.KindWorkspace:
		return "Workspaces › " + ref.Name
	default:
		return string(ref.Kind) + " › " + ref.Name
	}
}

func catalogActionLabel(action string) string {
	switch {
	case action == "identity-map-project":
		return "Map to project"
	case action == "project-map-identity", action == "kubernetes-map-identity":
		return "Map identity"
	case action == "kubernetes-set-project":
		return "Map project"
	case action == "docker-map-workspace":
		return "Map to workspace"
	case strings.HasPrefix(action, "workspace-set:"):
		return "Set " + strings.TrimPrefix(action, "workspace-set:")
	default:
		return strings.ReplaceAll(action, "-", " ")
	}
}
