package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/ui"
)

func catalogProjectDiscoveryPickerAuthenticated(cfg config.Config, identityName string, contexts ...context.Context) ui.Transition {
	options := []ui.Option{{Name: "\x00__manual__", Label: "Enter a project ID manually", Detail: "kept even when listing permission is unavailable"}}
	description := "Manual entry is always available."
	identity, hasIdentity := cfg.Identities[identityName]
	if hasIdentity {
		client, err := catalog.NewDefault()
		if err == nil {
			ctx, cancel := context.WithTimeout(operationContext(contexts), 15*time.Second)
			result, refreshErr := client.RefreshProjects(ctx, identity)
			cancel()
			for _, project := range result.Projects {
				if configuredProjectName(cfg, project.ProjectID) != "" {
					continue
				}
				options = append(options, ui.Option{Name: project.ProjectID, Label: project.ProjectID, Detail: firstNonEmpty(project.Name, string(result.Freshness))})
			}
			description = fmt.Sprintf("%s enumeration for %s", strings.ToUpper(string(result.Freshness)), identity.Account)
			if refreshErr != nil {
				description += ": " + refreshErr.Error()
			}
		}
	}
	return ui.Transition{Picker: ui.Picker{Screen: screenAddProjectChoice, Title: "Select accessible project", Description: description, Dimension: "catalog-target", Options: options}}
}

func catalogClusterDiscoveryPickerAuthenticated(cfg config.Config, projectName, identityName string, contexts ...context.Context) ui.Transition {
	project := cfg.Projects[projectName]
	options := []ui.Option{}
	description := "Manual entry is always available."
	if identity, ok := cfg.Identities[identityName]; ok {
		client, err := catalog.NewDefault()
		if err == nil {
			ctx, cancel := context.WithTimeout(operationContext(contexts), 15*time.Second)
			result, refreshErr := client.RefreshClusters(ctx, identity, project.ProjectID)
			cancel()
			if refreshErr != nil {
				if projectRefreshNeedsAuthentication(refreshErr) {
					description = "Authentication expired for " + identity.Account + ". Reauthenticate its isolated gcloud login, then ContextHop will retry this project automatically."
					options = append(options, ui.Option{Name: authenticateGKEPrefix + identityName, Label: "Reauthenticate " + identity.Account, Detail: "open gcloud login and retry cluster discovery"})
				} else {
					description = "Cluster discovery failed for " + identity.Account + " / " + project.ProjectID + ": " + strings.Join(strings.Fields(refreshErr.Error()), " ")
					options = append(options, ui.Option{Name: retryGKEPrefix + identityName, Label: "Retry cluster discovery", Detail: "try the identity-scoped gcloud request again"})
				}
			}
			for _, cluster := range result.Clusters {
				detail := cluster.Location + "  " + strings.ToUpper(string(result.Freshness))
				if configuredCluster(cfg, projectName, cluster.Name, cluster.Location) {
					detail += " · physical cluster already has an access profile"
				}
				options = append(options, ui.Option{Name: cluster.Name + "\x00" + cluster.Location, Label: cluster.Name, Detail: detail})
			}
			if refreshErr == nil {
				description = fmt.Sprintf("%s enumeration for %s / %s", strings.ToUpper(string(result.Freshness)), identity.Account, project.ProjectID)
			}
		}
	}
	options = append(options, ui.Option{Name: "\x00__manual__", Label: "Enter cluster metadata manually", Detail: "continue without provider enumeration"})
	return ui.Transition{Picker: ui.Picker{Screen: screenAddKubeChoice, Title: "Select accessible GKE cluster", Description: description, Dimension: "catalog-target", Options: options}}
}

func gkeProjectPicker(cfg config.Config, identityName string) ui.Picker {
	identity := cfg.Identities[identityName]
	options := []ui.Option{{Name: "\x00__paste__", Label: "Paste get-credentials command", Detail: "fast path: cluster, project, location, and endpoint flags"}}
	for _, option := range projectOptions(cfg, "") {
		project := cfg.Projects[option.Name]
		if project.Provider != "gcp" {
			continue
		}
		detail := project.ProjectID
		if slices.Contains(project.Identities, identityName) {
			detail += " · identity already mapped"
		} else {
			detail += " · will map " + identity.Account + " on save"
		}
		options = append(options, ui.Option{Name: option.Name, Label: project.ProjectID, Detail: detail, Risk: project.Risk})
	}
	return ui.Picker{Screen: screenAddKubeProject, Title: "Add GKE target › Project", Description: "Choose a known project or paste the command you already have. Any missing identity mapping is included in the same final change.", Dimension: "catalog-target", Options: options}
}

func gkeEndpointPicker() ui.Picker {
	return ui.Picker{Screen: screenAddKubeEndpoint, Title: "Add GKE target › Endpoint", Description: "Choose how gcloud should fetch this cluster's credentials. This is saved as part of the access profile.", Dimension: "catalog-target", HideSearch: true, Options: []ui.Option{
		{Name: "\x00__external__", Label: "External IP (default)", Detail: "standard gcloud endpoint selection"},
		{Name: "dns", Label: "DNS endpoint", Detail: "passes --dns-endpoint to gcloud"},
		{Name: "internal-ip", Label: "Internal IP", Detail: "passes --internal-ip to gcloud"},
	}}
}

func gkeReviewPicker(cfg config.Config, draft *gkeEditorDraft) ui.Picker {
	project := cfg.Projects[draft.Project]
	identity := cfg.Identities[draft.Identity]
	draft.Existing = matchingGKEAccessProfile(cfg, draft)
	targetName := draft.Existing
	status := "new access profile: " + proposedGKEName(cfg, draft)
	if targetName != "" {
		status = "reuse existing access profile: " + targetName
	} else if configuredCluster(cfg, draft.Project, draft.Cluster, draft.Location) {
		status += " · preserves the existing profile for this physical cluster"
	}
	mapping := "already mapped"
	if !slices.Contains(project.Identities, draft.Identity) {
		mapping = "will be mapped on save"
	}
	tags := strings.Join(project.TagNames(), ", ")
	description := fmt.Sprintf("Review the complete access profile before anything is changed.\n\nCluster: %s\nLocation: %s\nProject: %s\nIdentity: %s (%s)\nEndpoint: %s\nProject tags: %s\nResult: %s", draft.Cluster, draft.Location, project.ProjectID, identity.Account, mapping, gkeEndpointLabel(draft.Endpoint), tags, status)
	return ui.Picker{Screen: screenAddKubeReview, Title: "Add GKE target › Review", Description: description, Dimension: "catalog-action", HideSearch: true, Options: []ui.Option{{Name: "save", Label: "Continue", Detail: "show the atomic configuration change for confirmation"}}}
}

type parsedGKEArguments struct {
	Cluster, ProjectID, Location, Endpoint string
}

func parseGKEArguments(value string) (parsedGKEArguments, error) {
	fields := strings.Fields(strings.TrimSpace(value))
	parsed := parsedGKEArguments{}
	for index := 0; index < len(fields); index++ {
		field := fields[index]
		if field == "gcloud" || field == "container" || field == "clusters" || field == "get-credentials" || field == "--quiet" {
			continue
		}
		if field == "--dns-endpoint" {
			if parsed.Endpoint != "" && parsed.Endpoint != "dns" {
				return parsed, errors.New("choose only one of --dns-endpoint and --internal-ip")
			}
			parsed.Endpoint = "dns"
			continue
		}
		if field == "--internal-ip" {
			if parsed.Endpoint != "" && parsed.Endpoint != "internal-ip" {
				return parsed, errors.New("choose only one of --dns-endpoint and --internal-ip")
			}
			parsed.Endpoint = "internal-ip"
			continue
		}
		key, inlineValue, inline := strings.Cut(field, "=")
		if key == "--project" || key == "--location" || key == "--region" || key == "--zone" {
			flagValue := inlineValue
			if !inline {
				index++
				if index >= len(fields) {
					return parsed, fmt.Errorf("%s requires a value", key)
				}
				flagValue = fields[index]
			}
			switch key {
			case "--project":
				parsed.ProjectID = flagValue
			default:
				if parsed.Location != "" && parsed.Location != flagValue {
					return parsed, errors.New("choose only one cluster location")
				}
				parsed.Location = flagValue
			}
			continue
		}
		if strings.HasPrefix(field, "-") {
			return parsed, fmt.Errorf("unsupported gcloud flag %q", field)
		}
		if parsed.Cluster == "" {
			parsed.Cluster = field
			continue
		}
		return parsed, fmt.Errorf("unexpected argument %q", field)
	}
	if parsed.Cluster == "" || parsed.ProjectID == "" || parsed.Location == "" {
		return parsed, errors.New("GKE arguments require a cluster, --project, and --location, --region, or --zone")
	}
	return parsed, nil
}

func addProjectPlan(cfg config.Config, projectID, identityName, provenance string, editor *catalogEditorState) ui.Transition {
	project := config.Project{Provider: "gcp", ProjectID: projectID, Provenance: provenance}
	if provenance == "gcp" {
		project.VerifiedBy, project.ObservedAt = "gcp", time.Now().UTC().Format(time.RFC3339)
	}
	if identityName != "" {
		project.Identities = []string{identityName}
		if provenance == "manual" {
			project.ManualIdentities = []string{identityName}
		}
	}
	plan, err := catalog.PlanAddProject(cfg, uniqueCatalogName(cfg.Projects, projectID), project)
	return catalogPreviewTransition(plan, err, editor)
}

func addGKEPlan(cfg config.Config, draft *gkeEditorDraft, editor *catalogEditorState) ui.Transition {
	projectID := cfg.Projects[draft.Project].ProjectID
	target := config.Kubernetes{Type: "gke", Project: draft.Project, Cluster: draft.Cluster, Location: draft.Location, Endpoint: draft.Endpoint, ProviderID: "gke:" + projectID + "/" + draft.Location + "/" + draft.Cluster, Provenance: draft.Provenance}
	if draft.Provenance == "gcp" {
		target.VerifiedBy, target.ObservedAt = "gcp", time.Now().UTC().Format(time.RFC3339)
	}
	name := draft.Existing
	if name == "" {
		name = proposedGKEName(cfg, draft)
	}
	plan, err := catalog.PlanConfigureGKE(cfg, name, target, draft.Identity)
	return catalogPreviewTransition(plan, err, editor)
}

func matchingGKEAccessProfile(cfg config.Config, draft *gkeEditorDraft) string {
	names := make([]string, 0, len(cfg.Kubernetes))
	for name := range cfg.Kubernetes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target := cfg.Kubernetes[name]
		if target.Type == "gke" && target.Project == draft.Project && target.Cluster == draft.Cluster && target.Location == draft.Location && target.Endpoint == draft.Endpoint {
			return name
		}
	}
	return ""
}

func proposedGKEName(cfg config.Config, draft *gkeEditorDraft) string {
	base := draft.Cluster
	if draft.Endpoint == "dns" {
		base += "-dns"
	} else if draft.Endpoint == "internal-ip" {
		base += "-internal"
	}
	return uniqueCatalogName(cfg.Kubernetes, base)
}

func gkeEndpointLabel(endpoint string) string {
	switch endpoint {
	case "dns":
		return "DNS (--dns-endpoint)"
	case "internal-ip":
		return "internal IP (--internal-ip)"
	default:
		return "external IP (default)"
	}
}

func addKubeconfigPlan(cfg config.Config, source, contextName string, editor *catalogEditorState) ui.Transition {
	name := uniqueCatalogName(cfg.Kubernetes, contextName)
	target := config.Kubernetes{Type: "kubeconfig", Kubeconfig: source, Context: contextName, Cluster: contextName, Provenance: "manual"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := discovery.ValidateKubernetesSource(ctx, target)
	cancel()
	if err != nil {
		return catalogPreviewError(fmt.Errorf("import kubeconfig context: %w", err), editor)
	}
	plan, err := catalog.PlanAddKubernetes(cfg, name, target)
	if err == nil {
		enriched := plan.Config
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = discovery.EnrichKubernetesDependencies(ctx, &enriched, "")
		cancel()
		target = enriched.Kubernetes[name]
		target.VerifiedBy, target.ObservedAt = "kubeconfig", time.Now().UTC().Format(time.RFC3339)
		plan, err = catalog.PlanAddKubernetes(cfg, name, target)
	}
	return catalogPreviewTransition(plan, err, editor)
}

func configuredProjectName(cfg config.Config, projectID string) string {
	for name, project := range cfg.Projects {
		if project.ProjectID == projectID {
			return name
		}
	}
	return ""
}

func configuredCluster(cfg config.Config, project, cluster, location string) bool {
	for _, target := range cfg.Kubernetes {
		if target.Project == project && target.Cluster == cluster && target.Location == location {
			return true
		}
	}
	return false
}

func uniqueCatalogName[V any](items map[string]V, label string) string {
	name := strings.Trim(strings.Map(func(character rune) rune {
		switch {
		case character >= 'A' && character <= 'Z':
			return character + ('a' - 'A')
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			return character
		default:
			return '-'
		}
	}, label), "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		name = "item-" + name
	}
	candidate := name
	for suffix := 2; ; suffix++ {
		if _, exists := items[candidate]; !exists {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", name, suffix)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func catalogPreviewError(err error, editor *catalogEditorState) ui.Transition {
	return catalogPreviewTransition(catalog.Plan{}, err, editor)
}

func catalogDependencyPicker(cfg config.Config, encoded, action string) ui.Picker {
	options := []ui.Option{}
	switch action {
	case "identity-map-project", "kubernetes-set-project":
		options = formatCatalogComponentOptions(catalog.KindProject, projectOptions(cfg, ""))
	case "project-map-identity", "kubernetes-map-identity":
		ref, _ := decodeCatalogRef(encoded)
		projectName := ref.Name
		if action == "kubernetes-map-identity" {
			projectName = cfg.Kubernetes[ref.Name].Project
		}
		for _, option := range compatibleIdentityOptions(cfg, cfg.Projects[projectName].Provider) {
			if !slices.Contains(cfg.Projects[projectName].ManualIdentities, option.Name) {
				options = append(options, option)
			}
		}
		options = formatCatalogComponentOptions(catalog.KindIdentity, options)
	case "docker-map-workspace":
		options = destinationOptions(cfg, "workspace")
	default:
		if strings.HasPrefix(action, "workspace-set:") {
			kind := catalog.Kind(strings.TrimPrefix(action, "workspace-set:"))
			options = catalogComponentOptions(cfg, kind)
			options = append(options, ui.Option{Name: "", Label: "No " + string(kind), Detail: "remove this mapping"})
		}
	}
	ref, err := decodeCatalogRef(encoded)
	title := "Catalog › Select dependency"
	if err == nil {
		title = "Catalog › " + catalogRefLabel(cfg, ref) + " › " + catalogActionLabel(action)
	}
	return ui.Picker{
		Screen: ui.ScreenDependencyTarget, Title: title, Dimension: "catalog-target",
		Description: "Choose an option to save the mapping.", Options: options,
	}
}

func catalogComponentOptions(cfg config.Config, kind catalog.Kind) []ui.Option {
	var options []ui.Option
	switch kind {
	case catalog.KindIdentity:
		options = identityOptions(cfg, nil)
	case catalog.KindProject:
		options = projectOptions(cfg, "")
	case catalog.KindKubernetes:
		options = kubernetesOptions(cfg, "")
	case catalog.KindDocker:
		options = dockerOptions(cfg)
	}
	return formatCatalogComponentOptions(kind, options)
}

func formatCatalogComponentOptions(kind catalog.Kind, options []ui.Option) []ui.Option {
	for index := range options {
		switch kind {
		case catalog.KindIdentity:
			options[index].Label, options[index].Detail = options[index].IdentityAccount, options[index].Name
		case catalog.KindProject:
			options[index].Label, options[index].Detail = options[index].ProjectID, options[index].IdentityAccount
		case catalog.KindKubernetes:
			options[index].Label = options[index].KubernetesCluster
			options[index].Detail = strings.Join(nonEmptyStrings(options[index].ProjectID, options[index].KubernetesLocation, options[index].KubernetesContext, options[index].IdentityAccount), " · ")
		case catalog.KindDocker:
			options[index].Label, options[index].Detail = options[index].DockerContext, options[index].Name
		}
	}
	return options
}

func catalogProjectDiscoveryPicker(cfg config.Config, identityName string) ui.Transition {
	if identityName == "" {
		return catalogProjectDiscoveryPickerAuthenticated(cfg, identityName)
	}
	return authenticatedOperation(context.Background(), cfg, identityName, "Discovery", nil, func(ctx context.Context, _ func(string)) ui.Transition {
		return catalogProjectDiscoveryPickerAuthenticated(cfg, identityName, ctx)
	})
}

func catalogClusterDiscoveryPicker(cfg config.Config, projectName, identityName string) ui.Transition {
	return authenticatedOperation(context.Background(), cfg, identityName, "Discovery", nil, func(ctx context.Context, _ func(string)) ui.Transition {
		return catalogClusterDiscoveryPickerAuthenticated(cfg, projectName, identityName, ctx)
	})
}
