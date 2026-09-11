package main

import (
	"slices"
	"time"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func mergeProjectDiscovery(cfg config.Config, identityName string, result catalog.ProjectResult) []ui.Option {
	options := []ui.Option{}
	identity := cfg.Identities[identityName]
	for _, discovered := range result.Projects {
		name := configuredProjectName(cfg, discovered.ProjectID)
		if name == "" {
			name = uniqueCatalogName(cfg.Projects, discovered.ProjectID)
			cfg.Projects[name] = config.Project{Provider: "gcp", ProjectID: discovered.ProjectID, Identities: []string{identityName}, LabelSet: config.LabelSet{GoogleLabels: discovered.Labels}, Provenance: "gcp", VerifiedBy: "gcp", ObservedAt: result.LastSuccess.UTC().Format(time.RFC3339)}
		} else {
			project := cfg.Projects[name]
			project.GoogleLabels = discovered.Labels
			cfg.Projects[name] = project
			if !slices.Contains(project.Identities, identityName) {
				project.Identities = append(project.Identities, identityName)
				cfg.Projects[name] = project
			}
		}
		options = append(options, ui.Option{Name: name, Project: true, ProjectID: discovered.ProjectID, IdentityAccount: identity.Account, Tags: entityTags(cfg, catalog.Ref{Kind: catalog.KindProject, Name: name})})
	}
	return options
}

func mergeClusterDiscovery(cfg config.Config, projectName string, result catalog.ClusterResult) {
	project := cfg.Projects[projectName]
	for _, cluster := range result.Clusters {
		if configuredCluster(cfg, projectName, cluster.Name, cluster.Location) {
			for name, target := range cfg.Kubernetes {
				if target.Project == projectName && target.Cluster == cluster.Name && target.Location == cluster.Location {
					target.GoogleLabels = cluster.Labels
					cfg.Kubernetes[name] = target
				}
			}
			continue
		}
		name := uniqueCatalogName(cfg.Kubernetes, project.ProjectID+"-"+cluster.Name+"-"+cluster.Location)
		cfg.Kubernetes[name] = config.Kubernetes{LabelSet: config.LabelSet{GoogleLabels: cluster.Labels}, Type: "gke", Project: projectName, Cluster: cluster.Name, Location: cluster.Location, Provenance: "gcp", VerifiedBy: "gcp", ObservedAt: result.LastSuccess.UTC().Format(time.RFC3339)}
	}
}
