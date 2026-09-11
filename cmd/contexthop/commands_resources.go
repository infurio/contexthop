package main

import (
	"fmt"
	"os"
	"slices"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

func runLink(projectName, identityName string) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	plan, err := catalog.PlanMapIdentity(cfg, projectName, identityName)
	if err != nil {
		return err
	}
	if err := catalog.Apply(path, plan); err != nil {
		return err
	}

	fmt.Printf("Saved manual project association: %s → %s.\n", projectName, identityName)
	return nil
}

func loadConfig() (config.Config, error) {
	path, err := configPath()
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config.Config{}, fmt.Errorf("no configuration at %s; run `chop init`", path)
		}
		return config.Config{}, err
	}
	return cfg, nil
}

func runList() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Destinations) == 0 {
		fmt.Println("No workspaces configured. Open `chop config` to create one.")
		return nil
	}
	names := make([]string, 0, len(cfg.Destinations))
	for name := range cfg.Destinations {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		resolved, err := resolver.Destination(cfg, name)
		if err != nil {
			fmt.Printf("%-24s INVALID  %v\n", name, err)
			continue
		}
		identity, project, kubernetes, docker := "none", "none", "none", "none"
		if resolved.Identity != nil {
			identity = resolved.Identity.Account
		}
		if resolved.Project != nil {
			project = resolved.Project.ProjectID
		}
		if resolved.Kubernetes != nil {
			kubernetes = resolved.KubernetesName
		}
		if resolved.Docker != nil {
			docker = resolved.Docker.Context
		}
		fmt.Printf("%-24s identity=%s project=%s kubernetes=%s docker=%s\n", name, identity, project, kubernetes, docker)
	}
	return nil
}
