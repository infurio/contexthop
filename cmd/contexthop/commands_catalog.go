package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/discovery"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/catalog"
)

func runCatalogDiscovery(args []string) error {
	if err := discovery.RequireUnmanagedShell(); err != nil {
		return err
	}
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: chop config discover <identity> [project-name-or-id]")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	saved := cfg.Clone()
	identity, ok := cfg.Identities[args[0]]
	if !ok {
		return fmt.Errorf("unknown identity %q", args[0])
	}
	if err := ensureIdentityLogin(context.Background(), args[0], identity); err != nil {
		return err
	}
	client, err := catalog.NewDefault()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if len(args) == 1 {
		result, refreshErr := client.RefreshProjects(ctx, identity)
		fmt.Printf("%s: %d projects (last successful %s)\n", strings.ToUpper(string(result.Freshness)), len(result.Projects), formatObservationTime(result.LastSuccess))
		ids := []string{}
		for _, project := range result.Projects {
			ids = append(ids, project.ProjectID)
		}
		catalog.RecordDiscovery(cfg, args[0], "", ids, result.LastSuccess, refreshErr == nil && result.Freshness == catalog.FreshnessLive, refreshErr)
		mergeProjectDiscovery(cfg, args[0], result)
		return errors.Join(refreshErr, saveDiscoveryResult(saved, cfg))
	}
	projectName := args[1]
	project, ok := cfg.Projects[projectName]
	if !ok {
		projectName = configuredProjectName(cfg, args[1])
		project, ok = cfg.Projects[projectName]
		if !ok {
			return fmt.Errorf("unknown project name or ID %q", args[1])
		}
	}
	if mappedIdentityForProject(cfg, projectName, args[0]) == "" {
		return fmt.Errorf("identity %q is not mapped to project %q", args[0], projectName)
	}
	result, refreshErr := client.RefreshClusters(ctx, identity, project.ProjectID)
	fmt.Printf("%s: %d Kubernetes clusters in %s (last successful %s)\n", strings.ToUpper(string(result.Freshness)), len(result.Clusters), project.ProjectID, formatObservationTime(result.LastSuccess))
	ids := []string{}
	for _, cluster := range result.Clusters {
		ids = append(ids, cluster.Location+"/"+cluster.Name)
	}
	catalog.RecordDiscovery(cfg, args[0], project.ProjectID, ids, result.LastSuccess, refreshErr == nil && result.Freshness == catalog.FreshnessLive, refreshErr)
	mergeClusterDiscovery(cfg, projectName, result)
	return errors.Join(refreshErr, saveDiscoveryResult(saved, cfg))
}

func saveDiscoveryResult(saved, observed config.Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	plan := catalog.PlanSaveDiscovery(saved, observed)
	if len(plan.Changes) == 0 {
		return nil
	}
	return catalog.Apply(path, plan)
}

func runCatalogCache(args []string) error {
	if len(args) < 1 || len(args) > 2 || args[0] != "clear" {
		return errors.New("usage: chop config cache clear [identity]")
	}
	client, err := catalog.NewDefault()
	if err != nil {
		return err
	}
	if len(args) == 1 {
		if err := client.Clear(); err != nil {
			return err
		}
		fmt.Println("Cleared catalog metadata cache.")
		return nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	identity, ok := cfg.Identities[args[1]]
	if !ok {
		return fmt.Errorf("unknown identity %q", args[1])
	}
	if err := client.ClearIdentity(identity); err != nil {
		return err
	}
	fmt.Printf("Cleared catalog metadata for %s.\n", identity.Account)
	return nil
}

func formatObservationTime(value time.Time) string {
	if value.IsZero() {
		return "never"
	}
	return value.Local().Format(time.RFC3339)
}
