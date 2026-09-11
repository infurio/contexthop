package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
)

func relationshipDetail(cfg config.Config, ref catalog.Ref) string {
	parts := []string{}
	preference := ""
	switch ref.Kind {
	case catalog.KindProject:
		preference = cfg.Projects[ref.Name].PreferredIdentity
	case catalog.KindKubernetes:
		preference = cfg.Kubernetes[ref.Name].PreferredIdentity
		if identity := cfg.Kubernetes[ref.Name].ManualIdentity; identity != "" {
			parts = append(parts, "Configured access identity: "+identity)
		}
	}
	if ref.Kind == catalog.KindProject || ref.Kind == catalog.KindKubernetes {
		label := "automatic"
		if preference != "" {
			label = preference
		}
		parts = append(parts, "Preferred identity: "+label)
	}
	names := []string{}
	for name := range cfg.Identities {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		item := cfg.DiscoveryFor(name)
		var scope *config.DiscoveryScope
		switch ref.Kind {
		case catalog.KindIdentity:
			if ref.Name == name {
				scope = item.Projects
			}
		case catalog.KindProject:
			scope = item.Projects
			if clusters, ok := item.Clusters[cfg.Projects[ref.Name].ProjectID]; ok && clusters.Issue != "" {
				parts = append(parts, fmt.Sprintf("Cluster discovery · %s: %s", name, clusters.Issue.Label()))
			}
		case catalog.KindKubernetes:
			target := cfg.Kubernetes[ref.Name]
			if value, exists := item.Clusters[cfg.Projects[target.Project].ProjectID]; exists {
				scope = &value
			}
		}
		if scope == nil {
			continue
		}
		status := "last successful refresh " + scope.ObservedAt
		if scope.IsStale() {
			status = "stale · " + status
		}
		if scope.ObservedAt == "" {
			status = "discovery unavailable; refresh required"
		}
		if scope.Issue != "" {
			status = scope.Issue.Label() + " · " + status
		}
		membership := ""
		if ref.Kind == catalog.KindProject {
			membership = "not found · "
			if slices.Contains(scope.Resources, cfg.Projects[ref.Name].ProjectID) {
				membership = "found · "
			}
		}
		if ref.Kind == catalog.KindKubernetes {
			target := cfg.Kubernetes[ref.Name]
			membership = "not found · "
			if slices.Contains(scope.Resources, target.Location+"/"+target.Cluster) {
				membership = "found · "
			}
		}
		parts = append(parts, fmt.Sprintf("Discovery · %s: %s%s", name, membership, status))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n" + strings.Join(parts, "\n")
}
