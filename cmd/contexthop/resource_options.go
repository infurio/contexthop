package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/destination"
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

func kubernetesOptions(cfg config.Config, projectName string, includeHidden ...bool) []ui.Option {
	return scopedKubernetesOptions(cfg, projectName, "", includeHidden...)
}

func scopedKubernetesOptions(cfg config.Config, projectName, identityName string, includeHidden ...bool) []ui.Option {
	names := make([]string, 0, len(cfg.Kubernetes))
	for name := range cfg.Kubernetes {
		names = append(names, name)
	}
	slices.Sort(names)
	options := make([]ui.Option, 0, len(names))
	physicalTargets := map[string]int{}
	for _, name := range names {
		target := cfg.Kubernetes[name]
		if target.Hidden && !includeHiddenOptions(includeHidden) {
			continue
		}
		if projectName != "" && target.Project != projectName {
			continue
		}
		if identityName != "" && !resolver.KubernetesIdentityAvailable(cfg, name, identityName) {
			continue
		}
		projectID, identity := "", ""
		if project, ok := cfg.Projects[target.Project]; ok {
			projectID = project.ProjectID
			identity = identityChoiceSummary(cfg, resolver.EligibleIdentities(cfg, target.Project, name))
		}
		contextName := target.Context
		if contextName == "" {
			contextName = target.Cluster
		}
		if contextName == "" {
			contextName = name
		}
		contextName = kubernetesContextDisplay(cfg, name, target, contextName)
		option := ui.Option{
			Name: name, Kubernetes: true,
			KubernetesCluster:  kubernetesPhysicalName(name, target),
			KubernetesContext:  contextName,
			KubernetesLocation: target.Location,
			ProjectID:          projectID,
			IdentityAccount:    identity,
			Risk:               target.Risk,
		}
		option.Selection = ui.Draft{ui.ScreenProject: target.Project}
		option.RequiresIdentity = target.Project != "" || target.Type == "gke"
		if option.RequiresIdentity {
			option.IdentityChoices = formatCatalogComponentOptions(catalog.KindIdentity, identityOptions(cfg, resolver.EligibleIdentities(cfg, target.Project, name), true))
			setSelectionIdentity(cfg, destination.Target{Kind: "kubernetes", Name: name}, identityName, &option)
		}
		physicalKey := kubernetesPhysicalKey(cfg, name, target)
		if target.Hidden {
			physicalKey += "\x00hidden\x00" + name
		}
		if index, exists := physicalTargets[physicalKey]; exists {
			current := cfg.Kubernetes[options[index].Name]
			if preferKubernetesAlias(cfg, target, current) {
				options[index] = option
			}
			continue
		}
		physicalTargets[physicalKey] = len(options)
		options = append(options, option)
	}
	slices.SortFunc(options, func(left, right ui.Option) int {
		if byCluster := strings.Compare(strings.ToLower(left.KubernetesCluster), strings.ToLower(right.KubernetesCluster)); byCluster != 0 {
			return byCluster
		}
		return strings.Compare(strings.ToLower(left.KubernetesContext), strings.ToLower(right.KubernetesContext))
	})
	return options
}

func kubernetesOptionsForIdentity(cfg config.Config, identityName string, includeHidden ...bool) []ui.Option {
	return scopedKubernetesOptions(cfg, "", identityName, includeHidden...)
}

func kubernetesPhysicalName(name string, target config.Kubernetes) string {
	if target.Cluster != "" {
		return target.Cluster
	}
	if target.Context != "" {
		return target.Context
	}
	return name
}

func kubernetesContextDisplay(cfg config.Config, name string, target config.Kubernetes, contextName string) string {
	if target.Context == "" {
		return contextName
	}
	duplicates := 0
	for otherName, other := range cfg.Kubernetes {
		if otherName != name && other.Context == target.Context && kubernetesPhysicalKey(cfg, otherName, other) != kubernetesPhysicalKey(cfg, name, target) {
			duplicates++
		}
	}
	if duplicates == 0 {
		return contextName
	}
	source := filepath.Base(target.Kubeconfig)
	if source == "." || source == "" {
		source = target.Kubeconfig
	}
	return contextName + " [" + source + "]"
}

func kubernetesPhysicalKey(cfg config.Config, name string, target config.Kubernetes) string {
	if target.Type != "gke" || target.Project == "" || target.Location == "" || target.Cluster == "" {
		return "target\x00" + name
	}
	project := target.Project
	if configured, ok := cfg.Projects[target.Project]; ok {
		project = configured.ProjectID
	}
	key := strings.Join([]string{"gke", project, target.Location, target.Cluster}, "\x00")
	if target.AccessID != "" {
		key += "\x00access\x00" + target.AccessID
	}
	return key
}

func preferKubernetesAlias(cfg config.Config, candidate, current config.Kubernetes) bool {
	return kubernetesAliasScore(cfg, candidate) > kubernetesAliasScore(cfg, current)
}

func kubernetesAliasScore(cfg config.Config, target config.Kubernetes) int {
	if target.Type != "gke" {
		return 0
	}
	projectID := target.Project
	if project, ok := cfg.Projects[target.Project]; ok {
		projectID = project.ProjectID
	}
	generated := "gke_" + projectID + "_" + target.Location + "_" + target.Cluster
	if target.Context != "" && target.Context != generated {
		return 1
	}
	return 0
}

func dockerOptions(cfg config.Config, includeHidden ...bool) []ui.Option {
	names := make([]string, 0, len(cfg.Docker))
	for name := range cfg.Docker {
		names = append(names, name)
	}
	slices.Sort(names)
	options := make([]ui.Option, 0, len(names))
	for _, name := range names {
		target := cfg.Docker[name]
		if target.Hidden && !includeHiddenOptions(includeHidden) {
			continue
		}
		options = append(options, ui.Option{Name: name, Docker: true, DockerContext: target.Context, Risk: target.Risk})
	}
	return options
}

func noProjectOption(cfg config.Config, identityName string) ui.Option {
	return ui.Option{
		Name: noProjectSelection, EnterLabel: "Launch", Project: true, ProjectID: "No project",
		IdentityAccount: cfg.Identities[identityName].Account, KubernetesContext: "No Kubernetes",
	}
}

func noKubernetesOption(cfg config.Config, projectName, identityName string) ui.Option {
	return ui.Option{
		Name: noKubernetesSelection, EnterLabel: "Launch", Kubernetes: true, KubernetesContext: "No Kubernetes",
		ProjectID: cfg.Projects[projectName].ProjectID, IdentityAccount: cfg.Identities[identityName].Account,
	}
}

func mappedIdentityForProject(cfg config.Config, projectName, identityName string) string {
	if resolver.ProjectIdentityAvailable(cfg, projectName, identityName) {
		return identityName
	}
	return ""
}

func projectIdentitySummary(cfg config.Config, project config.Project) string {
	names := project.Identities
	for name, item := range cfg.Projects {
		if item.ProjectID == project.ProjectID && item.Provider == project.Provider {
			names = resolver.EligibleIdentities(cfg, name, "")
			break
		}
	}
	return identityChoiceSummary(cfg, names)
}

func identityChoiceSummary(cfg config.Config, names []string) string {
	if len(names) == 0 {
		return "not discovered"
	}
	if len(names) == 1 {
		return cfg.Identities[names[0]].Account
	}
	return fmt.Sprintf("%d identities", len(names))
}

func kubernetesCountSummary(cfg config.Config, projectName string, identities ...string) string {
	identityName := ""
	if len(identities) > 0 {
		identityName = identities[0]
	}
	var scope config.DiscoveryScope
	if identityName != "" {
		scope = cfg.DiscoveryFor(identityName).Clusters[cfg.Projects[projectName].ProjectID]
		if scope.Issue == config.DiscoveryGKEDisabled || scope.Issue == config.DiscoveryAccessDenied || scope.Issue == config.DiscoveryBillingDisabled {
			return scope.Issue.Label()
		}
		if scope.ObservedAt == "" {
			if scope.Stale || scope.Issue != "" {
				return "Scan failed"
			}
			return "not scanned"
		}
	}
	count := len(scopedKubernetesOptions(cfg, projectName, identityName))
	label := "none"
	if count == 1 {
		label = "1 cluster"
	} else if count > 1 {
		label = fmt.Sprintf("%d clusters", count)
	}
	if identityName != "" && scope.IsStale() {
		label += " · stale"
	}
	return label
}

func componentContextName(cfg config.Config, selection resolver.Selection) string {
	if target, ok := cfg.Kubernetes[selection.Kubernetes]; ok {
		if target.Context != "" {
			return target.Context
		}
		if target.Cluster != "" {
			return target.Cluster
		}
		return selection.Kubernetes
	}
	if project, ok := cfg.Projects[selection.Project]; ok {
		return project.ProjectID
	}
	if identity, ok := cfg.Identities[selection.Identity]; ok {
		return identity.Account
	}
	if target, ok := cfg.Docker[selection.Docker]; ok {
		return target.Context
	}
	return "context"
}

func shortComponent(dimension string) string {
	switch dimension {
	case "identity":
		return "i"
	case "project":
		return "p"
	case "kubernetes":
		return "k"
	case "docker":
		return "d"
	default:
		return dimension
	}
}

func loadDestinationOptions(dimension string) ([]ui.Option, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if dimension == "" || dimension == "kubernetes" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = discovery.EnrichKubernetesDependencies(ctx, &cfg, "")
		cancel()
	}
	return destinationOptions(cfg, dimension), nil
}

func destinationOptions(cfg config.Config, dimension string, includeHidden ...bool) []ui.Option {
	names := make([]string, 0, len(cfg.Destinations))
	for name := range cfg.Destinations {
		names = append(names, name)
	}
	slices.Sort(names)
	options := make([]ui.Option, 0, len(names))
	for _, name := range names {
		if cfg.Destinations[name].Hidden && !includeHiddenOptions(includeHidden) {
			continue
		}
		resolved, err := resolver.Destination(cfg, name)
		if err != nil {
			if option, ok := unresolvedKubernetesOption(cfg, name, dimension); ok {
				options = append(options, option)
			}
			continue
		}
		if !destinationHasDimension(resolved, dimension) {
			continue
		}
		parts := make([]string, 0, 3)
		if resolved.Project != nil {
			parts = append(parts, resolved.Project.ProjectID)
		}
		if resolved.Kubernetes != nil {
			parts = append(parts, resolved.KubernetesName)
		}
		if resolved.Docker != nil {
			parts = append(parts, resolved.Docker.Context)
		}
		if len(parts) == 0 && resolved.Identity != nil {
			parts = append(parts, resolved.Identity.Account)
		}
		options = append(options, ui.Option{
			Name:               name,
			Summary:            strings.Join(parts, " / "),
			Risk:               resolved.Risk,
			Identity:           resolved.Identity != nil,
			Project:            resolved.Project != nil,
			Kubernetes:         resolved.Kubernetes != nil,
			Docker:             resolved.Docker != nil,
			KubernetesCluster:  kubernetesClusterName(resolved),
			KubernetesContext:  kubernetesContextName(resolved),
			KubernetesLocation: kubernetesLocation(resolved),
			ProjectID:          projectID(resolved),
			IdentityAccount:    identityAccount(resolved),
			DockerContext:      dockerContextName(resolved),
		})
	}
	return options
}

func unresolvedKubernetesOption(cfg config.Config, name, dimension string) (ui.Option, bool) {
	if dimension != "" && dimension != "kubernetes" {
		return ui.Option{}, false
	}
	destination := cfg.Destinations[name]
	if destination.Kubernetes == "" {
		return ui.Option{}, false
	}
	target, ok := cfg.Kubernetes[destination.Kubernetes]
	if !ok {
		return ui.Option{}, false
	}
	projectName := destination.Project
	if projectName == "" {
		projectName = target.Project
	}
	projectValue := ""
	identityValue := ""
	if project, exists := cfg.Projects[projectName]; exists {
		projectValue = project.ProjectID
		switch len(project.Identities) {
		case 0:
			identityValue = "not mapped"
		case 1:
			if identity, exists := cfg.Identities[project.Identities[0]]; exists {
				identityValue = identity.Account
			}
		default:
			identityValue = "select identity"
		}
	}
	contextName := target.Context
	if contextName == "" {
		contextName = target.Cluster
	}
	if contextName == "" {
		contextName = destination.Kubernetes
	}
	return ui.Option{
		Name:               name,
		Summary:            strings.TrimSpace(projectValue + " / " + contextName),
		Kubernetes:         true,
		KubernetesCluster:  kubernetesPhysicalName(destination.Kubernetes, target),
		KubernetesContext:  contextName,
		KubernetesLocation: target.Location,
		ProjectID:          projectValue,
		IdentityAccount:    identityValue,
	}, true
}

func kubernetesClusterName(destination resolver.Resolved) string {
	if destination.Kubernetes == nil {
		return ""
	}
	return kubernetesPhysicalName(destination.KubernetesName, *destination.Kubernetes)
}

func kubernetesContextName(destination resolver.Resolved) string {
	if destination.Kubernetes == nil {
		return ""
	}
	if destination.Kubernetes.Context != "" {
		return destination.Kubernetes.Context
	}
	if destination.Kubernetes.Cluster != "" {
		return destination.Kubernetes.Cluster
	}
	return destination.KubernetesName
}

func kubernetesLocation(destination resolver.Resolved) string {
	if destination.Kubernetes == nil {
		return ""
	}
	return destination.Kubernetes.Location
}

func projectID(destination resolver.Resolved) string {
	if destination.Project == nil {
		return ""
	}
	return destination.Project.ProjectID
}

func identityAccount(destination resolver.Resolved) string {
	if destination.Identity == nil {
		return ""
	}
	return destination.Identity.Account
}

func dockerContextName(destination resolver.Resolved) string {
	if destination.Docker == nil {
		return ""
	}
	return destination.Docker.Context
}

func destinationHasDimension(destination resolver.Resolved, dimension string) bool {
	switch dimension {
	case "identity":
		return destination.Identity != nil
	case "project":
		return destination.Project != nil
	case "kubernetes":
		return destination.Kubernetes != nil
	case "docker":
		return destination.Docker != nil
	default:
		return true
	}
}

// Resource composition shares the launch dependency policy. An explicit
// eligible browsing identity wins over the catalog preference.
func setSelectionIdentity(cfg config.Config, target destination.Target, current string, option *ui.Option) {
	resolved, err := destination.Resolve(cfg, target, current)
	if err != nil || resolved.IdentityName == "" {
		return
	}
	if option.Selection == nil {
		option.Selection = ui.Draft{}
	}
	option.Selection[ui.ScreenIdentity] = resolved.IdentityName
}
