package main

import (
	"context"
	"fmt"
	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
	"time"
)

func discoverBrowserClustersAuthenticated(cfg, saved config.Config, identityName, projectName string, contexts ...context.Context) ui.Transition {
	identity, identityOK := cfg.Identities[identityName]
	project, projectOK := cfg.Projects[projectName]
	if !identityOK || !projectOK || mappedIdentityForProject(cfg, projectName, identityName) == "" {
		return ui.Transition{Picker: ui.Picker{Screen: "discovery-scope", Title: "Select a mapped identity and project", Description: "Use Space to select an identity and one of its projects before discovering Kubernetes clusters.", HideSearch: true, DisableEnter: true}}
	}
	client, err := catalog.NewDefault()
	if err != nil {
		return ui.Transition{Picker: ui.Picker{Screen: "discovery-error", Title: "Discovery unavailable", Description: err.Error(), HideSearch: true, DisableEnter: true}}
	}
	ctx, cancel := context.WithTimeout(operationContext(contexts), 20*time.Second)
	defer cancel()
	result, err := client.RefreshClusters(ctx, identity, project.ProjectID)
	ids := make([]string, 0, len(result.Clusters))
	for _, cluster := range result.Clusters {
		ids = append(ids, cluster.Location+"/"+cluster.Name)
	}
	catalog.RecordDiscovery(cfg, identityName, project.ProjectID, ids, result.LastSuccess, err == nil && result.Freshness == catalog.FreshnessLive, err)
	if projectRefreshNeedsAuthentication(err) {
		cloudauth.RecordStatus(identity, "Sign-in required")
		return ui.Transition{PersistDiscovery: true, Picker: providerAuthPicker(cfg, "browser-clusters", identityName, projectName)}
	}
	if err == nil {
		cloudauth.RecordStatus(identity, "Authenticated")
	}
	mergeClusterDiscovery(cfg, projectName, result)
	picker := interactiveBrowserPicker(cfg, saved, ui.ScreenKubernetes, ui.Draft{ui.ScreenIdentity: identityName, ui.ScreenProject: projectName})
	picker.Description = fmt.Sprintf("Fetched just now · %d Kubernetes clusters.", len(result.Clusters))
	if err != nil {
		picker.Description = "Refresh failed · cached results. " + discoveryFailureNotice(err)
		issue := catalog.ClassifyClusterDiscoveryError(err)
		if issue == config.DiscoveryGKEDisabled || issue == config.DiscoveryAccessDenied || issue == config.DiscoveryBillingDisabled {
			picker.Description = issue.Label() + " · cluster scan unavailable for this account and project. Previous results retained where available."
		}
		picker.OperationDetails = err.Error()
	}
	return ui.Transition{PersistDiscovery: true, ReplaceCurrent: true, Picker: picker, DraftUpdates: ui.Draft{ui.ScreenIdentity: identityName, ui.ScreenProject: projectName, ui.ScreenKubernetes: "", ui.ScreenWorkspace: ""}}
}

func discoverBrowserClusters(cfg, saved config.Config, identityName, projectName string) ui.Transition {
	return authenticatedOperation(context.Background(), cfg, identityName, "Discovery", nil, func(ctx context.Context, _ func(string)) ui.Transition {
		return discoverBrowserClustersAuthenticated(cfg, saved, identityName, projectName, ctx)
	})
}
