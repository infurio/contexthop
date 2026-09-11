package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/destination"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/ui"
)

type launchTarget = destination.Target

var parseLaunchTarget = destination.Parse
var resolveLaunchTarget = destination.Resolve
var launchIdentitySuggestion = destination.IdentitySuggestion
var launchHasWebConsole = destination.HasWebConsole

var errLaunchCancelled = errors.New("selection cancelled")

func resolveLaunchChoice(cfg config.Config, target launchTarget) (resolver.Resolved, error) {
	resolution := destination.Prepare(cfg, target)
	resolved, err := resolution.Resolved, resolution.Err
	if len(resolution.Candidates) == 0 {
		return resolved, err
	}
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return resolved, fmt.Errorf("%w; set a preferred identity or save an explicit workspace in chop config", err)
	}
	picker := launchIdentityPicker(cfg, target, resolution.Candidates)
	picker.Screen = "launch-identity"
	choice, choiceErr := ui.Run(ui.AppOptions{Version: version, ResourceBrowser: true, StartScreen: picker.Screen, Pickers: map[ui.Screen]ui.Picker{picker.Screen: picker}})
	if choiceErr != nil {
		return resolver.Resolved{}, choiceErr
	}
	if !choice.Complete {
		return resolver.Resolved{}, errLaunchCancelled
	}
	return resolveLaunchTarget(cfg, target, choice.Choice.Option.Name)
}

func activateLaunch(resolved resolver.Resolved, mode string) error {
	activation, present := os.LookupEnv(session.ActivationFileEnv)
	if mode == "apply" && activation == "" {
		return errors.New("enable Zsh integration first: eval \"$(chop shell-init zsh --in-place)\"")
	}
	child := mode == "shell" || mode != "apply" && os.Getenv(state.SessionFileEnv) == ""
	if child {
		_ = os.Unsetenv(session.ActivationFileEnv)
		defer func() {
			if present {
				_ = os.Setenv(session.ActivationFileEnv, activation)
			}
		}()
	}
	return runResolvedMode(resolved, nil, child)
}

func runUse(name string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	target, err := parseLaunchTarget(cfg, name)
	if err != nil {
		return err
	}
	resolved, err := resolveLaunchChoice(cfg, target)
	if errors.Is(err, errLaunchCancelled) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := validateLaunchSelection(cfg, target, resolved); err != nil {
		return err
	}
	return activateLaunch(resolved, "use")
}

func validateLaunchSelection(cfg config.Config, target launchTarget, resolved resolver.Resolved) error {
	if target.Kind == "workspace" {
		return validateReviewedWorkspace(cfg, target.Name, "")
	}
	return validateSelectionFresh(cfg, resolver.Selection{Name: resolved.Name, Identity: resolved.IdentityName, Project: resolved.ProjectName, Kubernetes: resolved.KubernetesName, Docker: resolved.DockerName})
}

func runPrevious() error {
	if os.Getenv(session.ActivationFileEnv) == "" {
		return errors.New("switch-back requires the managed Zsh chop function")
	}
	manifest, err := state.LoadManifest(os.Getenv(state.SessionFileEnv))
	if err != nil || manifest.Previous == nil {
		return errors.New("no previous context in this shell; switch once with chop first")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	resolved, err := resolvePrevious(cfg, *manifest.Previous)
	if err != nil {
		return err
	}
	return activateLaunch(resolved, "apply")
}

func resolvePrevious(cfg config.Config, p state.Selection) (resolver.Resolved, error) {
	var resolved resolver.Resolved
	var err error
	if p.Workspace != "" {
		resolved, err = resolver.Destination(cfg, p.Workspace)
		if err != nil || resolved.IdentityName != p.Identity || resolved.ProjectName != p.Project || resolved.KubernetesName != p.Kubernetes || resolved.DockerName != p.Docker || resolved.ADCMode != p.ADC {
			return resolver.Resolved{}, errors.New("previous workspace changed; select it again with chop")
		}
	} else {
		resolved, err = resolver.Components(cfg, resolver.Selection{Name: p.Name, Identity: p.Identity, Project: p.Project, Kubernetes: p.Kubernetes, Docker: p.Docker, ADC: p.ADC})
		if err != nil {
			return resolved, err
		}
	}
	if resolved.IdentityName != p.Identity || resolved.ProjectName != p.Project || resolved.KubernetesName != p.Kubernetes || resolved.DockerName != p.Docker {
		return resolver.Resolved{}, errors.New("previous context dependencies changed; select it again with chop")
	}
	if p.Docker != "" && p.DockerContext != "" && (resolved.Docker == nil || resolved.Docker.Context != p.DockerContext) {
		return resolver.Resolved{}, errors.New("previous Docker context changed; select it again with chop")
	}
	if p.Account != "" && (resolved.Identity == nil || resolved.Identity.Account != p.Account) || p.ProjectID != "" && (resolved.Project == nil || resolved.Project.ProjectID != p.ProjectID) || p.TargetIdentity != "" && (resolved.Kubernetes == nil || resolver.KubernetesTargetIdentity(*resolved.Kubernetes, resolved.Project) != p.TargetIdentity) {
		return resolver.Resolved{}, errors.New("previous context's identity, project or cluster changed; select it again with chop")
	}
	if resolved.Kubernetes != nil {
		resolved.Kubernetes.Namespace = p.Namespace
	}
	return resolved, nil
}
