package main

import (
	"errors"
	"fmt"
	"slices"

	"github.com/infurio/contexthop/internal/config"
)

func completionNames(cfg config.Config, kind string) []string {
	names := []string{}
	for name := range cfg.Destinations {
		names = append(names, "workspace:"+name)
	}
	if kind != "shell" && kind != "exec" {
		if kind == "" || kind == "use" {
			for name, identity := range cfg.Identities {
				if !identity.Hidden {
					names = append(names, "identity:"+name)
				}
			}
			for name, docker := range cfg.Docker {
				if !docker.Hidden {
					names = append(names, "docker:"+name)
				}
			}
		}
		if kind == "" || kind == "use" || kind == "console" {
			for name, project := range cfg.Projects {
				if !project.Hidden {
					names = append(names, "project:"+name)
				}
			}
		}
		for name, k := range cfg.Kubernetes {
			if !k.Hidden {
				names = append(names, "kubernetes:"+name)
			}
		}
	} else {
		names = nil
		for name := range cfg.Destinations {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func runCompletion(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: chop _complete [command]")
	}
	kind := ""
	if len(args) > 0 {
		kind = args[0]
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	for _, name := range completionNames(cfg, kind) {
		fmt.Println(name)
	}
	return nil
}
