package catalog

import (
	"fmt"
	"slices"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

// RecordDiscovery replaces only a successfully enumerated scope. Errors and
// cached responses cannot remove relationships or claim fresh evidence.
func RecordDiscovery(cfg config.Config, identityName, projectID string, resources []string, observed time.Time, complete bool, refreshErrors ...error) {
	identity, exists := cfg.Identities[identityName]
	if !exists {
		return
	}
	item := cfg.DiscoveryFor(identityName)
	item.Profile = config.DiscoveryProfile(identity)
	if item.Clusters == nil {
		item.Clusters = map[string]config.DiscoveryScope{}
	}
	scope := config.DiscoveryScope{}
	if projectID == "" {
		if item.Projects != nil {
			scope = *item.Projects
		}
	} else {
		scope = item.Clusters[projectID]
	}
	if complete {
		scope.Resources = slices.Clone(resources)
		slices.Sort(scope.Resources)
		scope.Resources = slices.Compact(scope.Resources)
		scope.ObservedAt = observed.UTC().Format(time.RFC3339)
		scope.Stale = false
		scope.Issue = ""
	} else {
		if scope.ObservedAt == "" && !observed.IsZero() {
			scope.Resources = slices.Clone(resources)
			scope.ObservedAt = observed.UTC().Format(time.RFC3339)
		}
		scope.Stale = true
		scope.Issue = config.DiscoveryScanFailed
		if projectID != "" && len(refreshErrors) > 0 && refreshErrors[0] != nil {
			scope.Issue = ClassifyClusterDiscoveryError(refreshErrors[0])
		}
	}
	if projectID == "" {
		item.Projects = &scope
		if complete {
			for id, clusters := range item.Clusters {
				if !slices.Contains(scope.Resources, id) {
					clusters.Stale = true
					item.Clusters[id] = clusters
				}
			}
		}
	} else {
		item.Clusters[projectID] = scope
	}
	cfg.Discovery[identityName] = item
}

func PlanPreferredIdentity(cfg config.Config, ref Ref, identityName string) (Plan, error) {
	after := cfg.Clone()
	var candidates []string
	switch ref.Kind {
	case KindProject:
		item, ok := after.Projects[ref.Name]
		if !ok {
			return Plan{}, unknown(ref.Kind, ref.Name)
		}
		candidates = resolver.EligibleIdentities(cfg, ref.Name, "")
		item.PreferredIdentity = identityName
		after.Projects[ref.Name] = item
	case KindKubernetes:
		item, ok := after.Kubernetes[ref.Name]
		if !ok {
			return Plan{}, unknown(ref.Kind, ref.Name)
		}
		candidates = resolver.EligibleIdentities(cfg, item.Project, ref.Name)
		item.PreferredIdentity = identityName
		after.Kubernetes[ref.Name] = item
	default:
		return Plan{}, fmt.Errorf("%s does not have an identity preference", ref.Kind)
	}
	if identityName != "" && !slices.Contains(candidates, identityName) {
		return Plan{}, fmt.Errorf("identity %q is not available for %s %q; refresh discovery first", identityName, ref.Kind, ref.Name)
	}
	label := identityName
	if label == "" {
		label = "automatic"
	}
	return buildPlan(cfg, after, []Change{{Action: "set", To: ref, Detail: "preferred identity: " + label}}), nil
}
