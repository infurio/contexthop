// Package selection owns the context prepared for launch independently of UI
// navigation, provider operations, and saved catalog mutations.
package selection

import (
	"fmt"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

type ADCChoice string

const (
	ADCDefault  ADCChoice = ""
	ADCOff      ADCChoice = "off"
	ADCIdentity ADCChoice = "identity"
)

// Request separates selected resources and launch settings from dialog fields.
// Workspace selects a complete preset; Source remembers its origin after edits.
type Request struct {
	Identity, Project, Kubernetes, Docker string
	Workspace, Source                     string
	ADCOverride                           ADCChoice
}

func ADCMode(choice ADCChoice) (string, error) {
	switch choice {
	case ADCOff:
		return "", nil
	case ADCIdentity:
		return "identity", nil
	default:
		return "", fmt.Errorf("unknown ADC choice %q", choice)
	}
}

// EffectiveADC applies an explicit override, then enforces the identity
// dependency. Invalid choices remain errors even without an identity.
func EffectiveADC(identity, inherited string, override ADCChoice) (string, error) {
	mode := inherited
	if override != ADCDefault {
		var err error
		mode, err = ADCMode(override)
		if err != nil {
			return "", err
		}
	}
	if identity == "" {
		return "", nil
	}
	return mode, nil
}

func (r Request) SourceWorkspace() string {
	if r.Source != "" {
		return r.Source
	}
	return r.Workspace
}

func (r Request) HasResources() bool {
	return r.Identity != "" || r.Project != "" || r.Kubernetes != "" || r.Docker != "" || r.Workspace != ""
}

// Components resolves dependency defaults and validates explicit identity
// choices, without depending on screen names or temporary search fields.
func (r Request) Components(cfg config.Config) (resolver.Selection, error) {
	return r.components(cfg, cfg)
}

func (r Request) components(cfg, defaults config.Config) (resolver.Selection, error) {
	result := resolver.Selection{Identity: r.Identity, Project: r.Project, Kubernetes: r.Kubernetes, Docker: r.Docker}
	// Validate an override before dependency resolution, as for workspace launches.
	if r.ADCOverride != ADCDefault {
		if _, err := ADCMode(r.ADCOverride); err != nil {
			return resolver.Selection{}, err
		}
	}
	if result.Kubernetes != "" {
		if target := cfg.Kubernetes[result.Kubernetes]; target.Project != "" {
			result.Project = target.Project
		}
	}
	if result.Project != "" {
		project := cfg.Projects[result.Project]
		if result.Identity == "" {
			suggested, candidates, err := resolver.SuggestedIdentity(cfg, result.Project, result.Kubernetes)
			if err != nil {
				return resolver.Selection{}, err
			}
			if suggested == "" && len(candidates) == 0 {
				return resolver.Selection{}, fmt.Errorf("project %q has no discovered identity; select an identity and refresh discovery", project.ProjectID)
			}
			if suggested == "" {
				return resolver.Selection{}, fmt.Errorf("project %q has multiple mapped identities; select one explicitly", project.ProjectID)
			}
			result.Identity = suggested
		} else if len(project.Identities) == 0 {
			identity, ok := cfg.Identities[result.Identity]
			if !ok {
				return resolver.Selection{}, fmt.Errorf("unknown identity %q", result.Identity)
			}
			if identity.Provider != project.Provider {
				return resolver.Selection{}, fmt.Errorf("selected identity provider %q is incompatible with project provider %q", identity.Provider, project.Provider)
			}
		} else if !resolver.ProjectIdentityAvailable(cfg, result.Project, result.Identity) {
			return resolver.Selection{}, fmt.Errorf("selected identity is not mapped to project %q", project.ProjectID)
		}
	}
	var err error
	result.ADC, err = EffectiveADC(result.Identity, defaults.Destinations[r.SourceWorkspace()].ADC, r.ADCOverride)
	return result, err
}

func (r Request) Resolve(cfg config.Config) (resolver.Resolved, error) {
	if r.Workspace == "" {
		components, err := r.Components(cfg)
		if err != nil {
			return resolver.Resolved{}, err
		}
		return resolver.Components(cfg, components)
	}
	workspace, ok := cfg.Destinations[r.Workspace]
	if !ok {
		return resolver.Resolved{}, fmt.Errorf("unknown workspace %q", r.Workspace)
	}
	if r.ADCOverride != ADCDefault {
		if _, err := ADCMode(r.ADCOverride); err != nil {
			return resolver.Resolved{}, err
		}
	}
	inherited := workspace.ADC
	workspace.ADC = ""
	cfg = cfg.Clone()
	cfg.Destinations[r.Workspace] = workspace
	resolved, err := resolver.Destination(cfg, r.Workspace)
	if err != nil {
		return resolver.Resolved{}, err
	}
	resolved.ADCMode, err = EffectiveADC(resolved.IdentityName, inherited, r.ADCOverride)
	return resolved, err
}

// ResolveForSave uses the same selection rules, taking inherited settings from
// the saved catalog while component availability comes from the current view.
func (r Request) ResolveForSave(cfg, saved config.Config) (resolver.Resolved, error) {
	components, err := r.components(cfg, saved)
	if err != nil {
		return resolver.Resolved{}, err
	}
	return resolver.Components(cfg, components)
}
