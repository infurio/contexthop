package resolver

import (
	"slices"
	"sort"

	"github.com/infurio/contexthop/internal/config"
)

// ProjectIdentityAvailable uses observed scope membership when available, with
// existing/manual associations as the fallback for profiles not yet enumerated.
func ProjectIdentityAvailable(cfg config.Config, projectName, identityName string) bool {
	project, ok := cfg.Projects[projectName]
	identity, exists := cfg.Identities[identityName]
	if !ok || !exists || project.Provider != identity.Provider {
		return false
	}
	if slices.Contains(project.ManualIdentities, identityName) {
		return true
	}
	if evidence, exists := cfg.Discovery[identityName]; exists && evidence.Profile != config.DiscoveryProfile(identity) {
		return false
	}
	if scope := cfg.DiscoveryFor(identityName).Projects; scope != nil && scope.ObservedAt != "" {
		return slices.Contains(scope.Resources, project.ProjectID)
	}
	return slices.Contains(project.Identities, identityName)
}

func KubernetesIdentityAvailable(cfg config.Config, targetName, identityName string) bool {
	target, ok := cfg.Kubernetes[targetName]
	if !ok {
		return false
	}
	if target.Project == "" {
		_, exists := cfg.Identities[identityName]
		return exists
	}
	if !ProjectIdentityAvailable(cfg, target.Project, identityName) {
		return false
	}
	if target.Type != "gke" {
		return true
	}
	project := cfg.Projects[target.Project]
	evidence := cfg.DiscoveryFor(identityName)
	if scope, exists := evidence.Clusters[project.ProjectID]; exists && scope.ObservedAt != "" {
		return slices.Contains(scope.Resources, target.Location+"/"+target.Cluster)
	}
	// A deliberately configured access profile can be used before enumeration.
	if target.ManualIdentity == identityName {
		return true
	}
	// Legacy catalogs remain usable until provider discovery supplies evidence.
	if evidence.Projects != nil && evidence.Projects.ObservedAt != "" {
		return false
	}
	for name := range cfg.Identities {
		if scope, exists := cfg.DiscoveryFor(name).Clusters[project.ProjectID]; exists && scope.ObservedAt != "" {
			return false
		}
	}
	return true
}

func EligibleIdentities(cfg config.Config, projectName, targetName string) []string {
	names := []string{}
	for name := range cfg.Identities {
		eligible := ProjectIdentityAvailable(cfg, projectName, name)
		if targetName != "" {
			eligible = KubernetesIdentityAvailable(cfg, targetName, name)
		}
		if eligible {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
