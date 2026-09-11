// Package appstate owns saved resources and the session-only discovery overlay.
// It has no terminal, provider, or filesystem dependencies.
package appstate

import (
	"reflect"
	"slices"

	"github.com/infurio/contexthop/internal/config"
)

// Catalog keeps persisted preferences separate from observations. Callers receive
// copies so a background workflow cannot mutate the state being rendered.
type Catalog struct {
	saved      config.Config
	discovery  map[string]config.IdentityDiscovery
	projects   map[string]config.Project
	kubernetes map[string]config.Kubernetes
	aliases    map[string][]config.KubernetesAlias
}

func New(saved config.Config) Catalog {
	return Catalog{saved: saved.Clone(), projects: map[string]config.Project{}, kubernetes: map[string]config.Kubernetes{}, aliases: map[string][]config.KubernetesAlias{}}
}

func (s Catalog) Saved() config.Config { return s.saved.Clone() }

func (s Catalog) Clone() Catalog {
	copy := New(s.saved)
	copy.Observe(s.Selection())
	return copy
}

// Selection builds a disposable combined view. Observed identities augment a
// saved project's mappings; its saved preferences always take precedence.
func (s Catalog) Selection() config.Config {
	view := s.saved.Clone()
	for name, item := range config.CloneDiscovery(s.discovery) {
		view.Discovery[name] = item
	}
	for name, observed := range s.projects {
		project, saved := view.Projects[name]
		if saved {
			if project.ProjectID != observed.ProjectID || project.Provider != observed.Provider {
				continue
			}
		} else {
			project = observed
			project.Identities = nil
		}
		project.GoogleLabels = observed.LabelSet.Clone().GoogleLabels
		project.LabelSet = project.LabelSet.Clone()
		for _, identity := range observed.Identities {
			if item, exists := view.Identities[identity]; exists && item.Provider == project.Provider && !slices.Contains(project.Identities, identity) {
				project.Identities = append(project.Identities, identity)
			}
		}
		view.Projects[name] = project
	}
	for name, target := range s.kubernetes {
		target.LabelSet = target.LabelSet.Clone()
		view.Kubernetes[name] = target
	}
	for name, aliases := range s.aliases {
		view.KubernetesAliases[name] = slices.Clone(aliases)
	}
	return view
}

// Observe extracts only differences produced by discovery from a working view.
// It never changes the persisted catalog.
func (s *Catalog) Observe(view config.Config) {
	s.discovery = config.CloneDiscovery(view.Discovery)
	s.projects = map[string]config.Project{}
	for name, project := range view.Projects {
		saved, exists := s.saved.Projects[name]
		if !exists || !slices.Equal(project.Identities, saved.Identities) || !reflect.DeepEqual(project.GoogleLabels, saved.GoogleLabels) {
			project.Identities = slices.Clone(project.Identities)
			project.LabelSet = project.LabelSet.Clone()
			s.projects[name] = project
		}
	}
	s.kubernetes = map[string]config.Kubernetes{}
	for name, target := range view.Kubernetes {
		if saved, exists := s.saved.Kubernetes[name]; !exists || !reflect.DeepEqual(saved, target) {
			target.LabelSet = target.LabelSet.Clone()
			s.kubernetes[name] = target
		}
	}
	s.aliases = map[string][]config.KubernetesAlias{}
	for name, aliases := range view.KubernetesAliases {
		if !reflect.DeepEqual(aliases, s.saved.KubernetesAliases[name]) {
			s.aliases[name] = slices.Clone(aliases)
		}
	}
}

// SavedChange installs a successfully persisted catalog. Deleted resources and
// removed mappings must not be resurrected by earlier discovery observations.
func (s *Catalog) SavedChange(saved config.Config) {
	for name, observed := range s.projects {
		before, existed := s.saved.Projects[name]
		after, exists := saved.Projects[name]
		if existed && (!exists || before.ProjectID != after.ProjectID || before.Provider != after.Provider) {
			delete(s.projects, name)
			continue
		}
		for _, identity := range before.Identities {
			if !slices.Contains(after.Identities, identity) {
				observed.Identities = slices.DeleteFunc(slices.Clone(observed.Identities), func(value string) bool { return value == identity })
			}
		}
		s.projects[name] = observed
	}
	for name := range s.kubernetes {
		if !reflect.DeepEqual(s.saved.Kubernetes[name], saved.Kubernetes[name]) {
			delete(s.kubernetes, name)
			delete(s.aliases, name)
		}
	}
	for name := range s.aliases {
		if !reflect.DeepEqual(s.saved.KubernetesAliases[name], saved.KubernetesAliases[name]) || !reflect.DeepEqual(s.saved.Kubernetes[name], saved.Kubernetes[name]) {
			delete(s.aliases, name)
		}
	}
	s.discovery = config.CloneDiscovery(saved.Discovery)
	s.saved = saved.Clone()
	// Remove observations that have become saved and filter deleted identities.
	s.Observe(s.Selection())
}
