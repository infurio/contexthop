package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

type discoveryClient interface {
	RefreshProjects(context.Context, config.Identity) (catalog.ProjectResult, error)
	RefreshClusters(context.Context, config.Identity, string) (catalog.ClusterResult, error)
}

func runDiscoveryDialog(cfg, saved config.Config, draft ui.Draft, report func(string), contexts ...context.Context) ui.Transition {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	return authenticatedOperation(ctx, cfg, draft[discoverIdentity], "Discovery", report, func(ctx context.Context, report func(string)) ui.Transition {
		client, err := catalog.NewDefault()
		if err != nil {
			return ui.Transition{ReplaceCurrent: true, Picker: discoveryDialog(cfg, draft, err.Error())}
		}
		return runScopedDiscovery(cfg, saved, draft, report, client, ctx)
	})
}

func runScopedDiscovery(cfg, saved config.Config, draft ui.Draft, report func(string), client discoveryClient, contexts ...context.Context) ui.Transition {
	runCtx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		runCtx = contexts[0]
	}
	identityName := draft[discoverIdentity]
	identity := cfg.Identities[identityName]
	actor := fmt.Sprintf("%s [%s]", identity.Account, identityName)
	if report == nil {
		report = func(string) {}
	}
	details := []string{}
	projectNames := []string{}
	retryNames := []string{}
	completedNames := map[string]bool{}
	freshProjects, cachedProjects, freshClusters, cachedClusters, checked, failed := 0, 0, 0, 0, 0, 0
	disabled, denied, billing := 0, 0, 0
	authFailed := false
	observeAuth := func(err error) {
		if projectRefreshNeedsAuthentication(err) {
			authFailed = true
			cloudauth.RecordStatus(identity, "Sign-in required")
		} else if err == nil && !authFailed {
			cloudauth.RecordStatus(identity, "Authenticated")
		}
	}
	if draft[discoverScope] == "retry" {
		projectNames = strings.Split(draft[discoverRetryProjects], "\x00")
	} else if draft[discoverScope] == "clusters" {
		projectNames = append(projectNames, draft[discoverProject])
	} else {
		report("Discovering projects as " + actor)
		ctx, cancel := context.WithTimeout(runCtx, 15*time.Second)
		result, err := client.RefreshProjects(ctx, identity)
		cancel()
		complete := err == nil && result.Freshness == catalog.FreshnessLive
		ids := []string{}
		for _, project := range result.Projects {
			ids = append(ids, project.ProjectID)
		}
		catalog.RecordDiscovery(cfg, identityName, "", ids, result.LastSuccess, complete)
		mergeProjectDiscovery(cfg, identityName, result)
		if !complete {
			cachedProjects = len(result.Projects)
		} else {
			freshProjects = len(result.Projects)
		}

		observeAuth(err)
		if err != nil {
			if runCtx.Err() == nil {
				failed++
			}
			details = append(details, "Projects: "+err.Error())
		} else if draft[discoverScope] == "all" {
			for _, project := range result.Projects {
				projectNames = append(projectNames, configuredProjectName(cfg, project.ProjectID))
			}
		}
	}
	type clusterWork struct {
		name   string
		result catalog.ClusterResult
		err    error
	}
	jobs := make(chan string, len(projectNames))
	results := make(chan clusterWork, len(projectNames))
	projectIDs := map[string]string{}
	for _, name := range projectNames {
		projectIDs[name] = cfg.Projects[name].ProjectID
		jobs <- name
	}
	close(jobs)
	if len(projectNames) > 0 {
		report(fmt.Sprintf("Discovering as %s · 0/%d projects checked", actor, len(projectNames)))
	}
	var workers sync.WaitGroup
	for range min(3, len(projectNames)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for name := range jobs {
				if runCtx.Err() != nil {
					return
				}
				ctx, cancel := context.WithTimeout(runCtx, 20*time.Second)
				result, err := client.RefreshClusters(ctx, identity, projectIDs[name])
				cancel()
				results <- clusterWork{name, result, err}
			}
		}()
	}
	go func() { workers.Wait(); close(results) }()
	for work := range results {
		completedNames[work.name] = true
		if work.err != nil {
			retryNames = append(retryNames, work.name)
		}
		interrupted := work.err != nil && runCtx.Err() != nil
		if !interrupted {
			checked++
		}
		ids := []string{}
		for _, cluster := range work.result.Clusters {
			ids = append(ids, cluster.Location+"/"+cluster.Name)
		}
		complete := work.err == nil && work.result.Freshness == catalog.FreshnessLive
		if !interrupted {
			catalog.RecordDiscovery(cfg, identityName, projectIDs[work.name], ids, work.result.LastSuccess, complete, work.err)
		}
		mergeClusterDiscovery(cfg, work.name, work.result)
		if complete {
			freshClusters += len(work.result.Clusters)
		} else {
			cachedClusters += len(work.result.Clusters)
		}

		observeAuth(work.err)
		if work.err != nil {
			if !interrupted {
				switch catalog.ClassifyClusterDiscoveryError(work.err) {
				case config.DiscoveryGKEDisabled:
					disabled++
				case config.DiscoveryAccessDenied:
					denied++
				case config.DiscoveryBillingDisabled:
					billing++
				default:
					failed++
				}
			}
			details = append(details, projectIDs[work.name]+": "+work.err.Error())
		} else {
			details = append(details, fmt.Sprintf("%s: %d clusters (fresh)", projectIDs[work.name], len(work.result.Clusters)))
		}
		report(fmt.Sprintf("Discovering as %s · %d/%d projects checked · %d fresh clusters · %d GKE disabled · %d access denied · %d billing disabled · %d failed", actor, checked, len(projectNames), freshClusters, disabled, denied, billing, failed))
	}
	for _, name := range projectNames {
		if !completedNames[name] {
			retryNames = append(retryNames, name)
		}
	}
	slices.Sort(retryNames)
	summary := fmt.Sprintf("Identity: %s", actor)
	if draft[discoverScope] != "clusters" && draft[discoverScope] != "retry" {
		summary += fmt.Sprintf("\nProjects: %d fresh · %d cached", freshProjects, cachedProjects)
	}
	if draft[discoverScope] != "projects" {
		summary += fmt.Sprintf("\nClusters: %d fresh · %d cached\nProject scans: %d checked · %d not scanned", freshClusters, cachedClusters, checked, len(projectNames)-checked)
		if len(projectNames) == 0 && draft[discoverScope] == "all" && (failed > 0 || runCtx.Err() != nil) {
			summary += "\nCluster scan not started because project discovery did not complete."
		}
	}
	summary += fmt.Sprintf("\nGKE disabled: %d\nAccess denied: %d\nBilling disabled: %d\nFailures: %d", disabled, denied, billing, failed)
	title := "Discovery complete"
	if failed > 0 {
		title = "Discovery finished with errors"
		summary += "\nFailed scopes retain their previous results."
	}
	if disabled+denied+billing > 0 || failed > 0 && (freshProjects > 0 || checked > disabled+denied+billing+failed) {
		title = "Discovery partially complete"
	}
	if runCtx.Err() != nil {
		title = "Discovery stopped"
		summary += "\nCompleted results are retained."
	}

	counts := []string{}
	if draft[discoverScope] != "clusters" && draft[discoverScope] != "retry" {
		counts = append(counts, discoveryCount(freshProjects, "project"))
	}
	if draft[discoverScope] != "projects" {
		counts = append(counts, discoveryCount(freshClusters, "cluster"))
	}
	notice := "Discovered " + strings.Join(counts, " and ") + " as " + actor + "."
	if failed+disabled+denied+billing > 0 {
		outcomes := []string{}
		if disabled > 0 {
			outcomes = append(outcomes, fmt.Sprintf("%d GKE disabled", disabled))
		}
		if denied > 0 {
			outcomes = append(outcomes, fmt.Sprintf("%d access denied", denied))
		}
		if billing > 0 {
			outcomes = append(outcomes, fmt.Sprintf("%d billing disabled", billing))
		}
		if failed > 0 {
			outcomes = append(outcomes, discoveryCount(failed, "failed scope"))
		}
		notice = fmt.Sprintf("%s: %s as %s · %s. Previous results retained where available; press o for results.", title, strings.Join(counts, " and "), actor, strings.Join(outcomes, " · "))
	}
	if runCtx.Err() != nil {
		notice = "Discovery stopped as " + actor + ". Completed results retained; press o for results."
	}
	origin := ui.Screen(draft[discoverOrigin])
	switch origin {
	case ui.ScreenWorkspace, ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker:
	default:
		origin = ui.ScreenProject
	}
	picker := interactiveBrowserPicker(cfg, saved, origin, draft)
	picker.Description = notice
	picker.StatusKind = "success"
	if failed > 0 {
		picker.StatusKind = "error"
	}
	if title == "Discovery partially complete" || runCtx.Err() != nil {
		picker.StatusKind = "warning"
	}
	if len(retryNames) > 0 || failed > 0 {
		label := "Retry failed project scans"
		if len(retryNames) == 0 {
			label = "Retry discovery"
		}
		picker.OperationDraft = ui.Draft{discoverIdentity: identityName, discoverScope: draft[discoverScope], discoverProject: draft[discoverProject], discoverOrigin: draft[discoverOrigin], discoverRetryProjects: strings.Join(retryNames, "\x00")}
		picker.OperationActions = []ui.KeyAction{{Key: "R", Label: label, Action: "retry-discovery"}}
	}
	picker.OperationDetails = title + "\n" + summary + "\n\n" + strings.Join(details, "\n")
	return ui.Transition{PersistDiscovery: true, ReturnToPrevious: true, PreservePosition: true, ReplaceCurrent: true, Picker: picker, DraftUpdates: ui.Draft{discoverRetryProjects: strings.Join(retryNames, "\x00")}}
}

func discoveryCount(count int, noun string) string {
	if count != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s", count, noun)
}
