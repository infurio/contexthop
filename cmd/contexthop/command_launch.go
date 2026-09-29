package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/selection"
	"github.com/infurio/contexthop/internal/session"
)

type launchArguments struct {
	request selection.Request
	noLogin bool
	command []string
}

func parseLaunchArguments(args []string, execMode bool) (launchArguments, error) {
	var result launchArguments
	verb := "shell"
	if execMode {
		verb = "exec"
		separator := -1
		for i, arg := range args {
			if arg == "--" {
				separator = i
				break
			}
		}
		if separator < 0 || separator == len(args)-1 {
			return result, fmt.Errorf("usage: chop exec [--no-login] <workspace> | <component flags> -- <command> [args...]")
		}
		result.command, args = args[separator+1:], args[:separator]
	}
	seen := map[string]bool{}
	components := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" && !execMode {
			if result.request.Workspace != "" || i+2 != len(args) {
				return result, fmt.Errorf("chop shell accepts one workspace after --")
			}
			result.request.Workspace = args[i+1]
			break
		}
		if arg == "--no-login" {
			result.noLogin = true
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			if result.request.Workspace != "" {
				return result, fmt.Errorf("chop %s accepts one workspace or component flags", verb)
			}
			result.request.Workspace = arg
			continue
		}
		flag, value, hasValue := strings.Cut(arg, "=")
		var target *string
		switch flag {
		case "--workspace":
			target = &result.request.Workspace
		case "--identity":
			target = &result.request.Identity
		case "--project":
			target = &result.request.Project
		case "--kubernetes":
			target = &result.request.Kubernetes
		case "--docker":
			target = &result.request.Docker
		default:
			return result, fmt.Errorf("unknown chop %s option %q", verb, flag)
		}
		if seen[flag] || flag == "--workspace" && result.request.Workspace != "" {
			return result, fmt.Errorf("%s may be specified only once", flag)
		}
		seen[flag] = true
		components = components || flag != "--workspace"
		if !hasValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return result, fmt.Errorf("%s requires a saved catalog name", flag)
			}
			i++
			value = args[i]
		}
		if value == "" {
			return result, fmt.Errorf("%s requires a saved catalog name; omit the flag for an unset component", flag)
		}
		*target = value
	}
	if components && result.request.Workspace != "" {
		return result, fmt.Errorf("choose a workspace or component flags, not both")
	}
	if !result.request.HasResources() {
		return result, fmt.Errorf("chop %s requires a workspace or at least one component flag", verb)
	}
	return result, nil
}

func runShellArgs(args []string) error { return runLaunchArguments(args, false) }
func runExecArgs(args []string) error  { return runLaunchArguments(args, true) }

func runLaunchArguments(args []string, execMode bool) error {
	options, err := parseLaunchArguments(args, execMode)
	if err != nil {
		return err
	}
	if options.request.Workspace != "" {
		return runInDestinationWithLogin(options.request.Workspace, options.command, options.noLogin)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := discovery.EnrichKubernetesDependencies(ctx, &cfg, ""); err != nil {
		return err
	}
	resolved, err := resolveLaunchComponents(cfg, options.request)
	if err != nil {
		return err
	}
	if !execMode {
		// An explicit shell launch always creates a child, even inside Chop's
		// current-shell integration. Other terminals retain their environment.
		activation, present := os.LookupEnv(session.ActivationFileEnv)
		_ = os.Unsetenv(session.ActivationFileEnv)
		defer func() {
			if present {
				_ = os.Setenv(session.ActivationFileEnv, activation)
			}
		}()
	}
	return runResolvedModeWithLogin(resolved, options.command, !execMode, options.noLogin)
}

func resolveLaunchComponents(cfg config.Config, request selection.Request) (resolver.Resolved, error) {
	// The interactive staging policy follows a selected cluster's project.
	// Explicit CLI flags must instead reject a contradictory project choice.
	if target, ok := cfg.Kubernetes[request.Kubernetes]; ok && target.Project != "" && request.Project != "" && request.Project != target.Project {
		return resolver.Resolved{}, fmt.Errorf("project %q does not match kubernetes target project %q", request.Project, target.Project)
	}
	components, err := request.Components(cfg)
	if err != nil {
		return resolver.Resolved{}, err
	}
	components.Name = componentContextName(cfg, components)
	return resolver.Components(cfg, components)
}
