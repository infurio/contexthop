// Package destination projects catalog resources into launchable destinations.
// It has no terminal, provider process, authentication or persistence side effects.
package destination

import (
	"fmt"
	"strings"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

type Target struct{ Kind, Name string }

func (t Target) Key() string { return t.Kind + ":" + t.Name }

func Parse(cfg config.Config, name string) (Target, error) {
	exists := func(kind, name string) bool {
		switch kind {
		case "workspace":
			_, ok := cfg.Destinations[name]
			return ok
		case "identity":
			_, ok := cfg.Identities[name]
			return ok
		case "project":
			_, ok := cfg.Projects[name]
			return ok
		case "kubernetes":
			_, ok := cfg.Kubernetes[name]
			return ok
		case "docker":
			_, ok := cfg.Docker[name]
			return ok
		}
		return false
	}
	kind, value, qualified := strings.Cut(name, ":")
	if qualified && exists(kind, value) {
		return Target{kind, value}, nil
	}
	var matches []Target
	for _, kind := range []string{"workspace", "identity", "project", "kubernetes", "docker"} {
		if exists(kind, name) {
			matches = append(matches, Target{kind, name})
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		var choices []string
		for _, match := range matches {
			choices = append(choices, match.Key())
		}
		return Target{}, fmt.Errorf("%q names multiple destinations; use %s", name, strings.Join(choices, " or "))
	}
	return Target{}, fmt.Errorf("unknown destination %q; use chop to search", name)
}

func Resolve(cfg config.Config, target Target, identity string) (resolver.Resolved, error) {
	selection := resolver.Selection{Name: target.Name, Identity: identity}
	switch target.Kind {
	case "workspace":
		return resolver.Destination(cfg, target.Name)
	case "identity":
		selection.Identity = target.Name
	case "project":
		selection.Project = target.Name
	case "kubernetes":
		selection.Kubernetes = target.Name
	case "docker":
		selection.Docker = target.Name
	default:
		return resolver.Resolved{}, fmt.Errorf("unknown destination type %q", target.Kind)
	}
	return resolver.Components(cfg, selection)
}

func IdentitySuggestion(cfg config.Config, target Target) (string, []string, error) {
	if target.Kind == "project" {
		return resolver.SuggestedIdentity(cfg, target.Name, "")
	}
	return resolver.SuggestedIdentity(cfg, cfg.Kubernetes[target.Name].Project, target.Name)
}

// Google Cloud identities have an account home; project-backed targets have
// project dashboards or GKE cluster pages.
func HasWebConsole(cfg config.Config, target Target) bool {
	resolution := Prepare(cfg, target)
	supports := func(r resolver.Resolved) bool {
		return r.Identity != nil && r.Identity.Provider == "gcp" && r.Identity.Account != "" &&
			(r.Project == nil || r.Project.Provider == "gcp" && r.Project.ProjectID != "")
	}
	if resolution.Err == nil {
		return supports(resolution.Resolved)
	}
	// An unresolved account choice still offers Web, using the same chooser as launch.
	for _, identity := range resolution.Candidates {
		if r, err := Resolve(cfg, target, identity); err == nil && supports(r) {
			return true
		}
	}
	return false
}
