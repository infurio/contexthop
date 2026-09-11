package destination

import (
	"fmt"

	"github.com/infurio/contexthop/internal/config"
)

// Model is the common catalog projection for resource tabs and direct commands.
// Dependencies names stay internal; Label and Tags are presentation values.
// Identity ambiguity is resolved by Resolve/IdentitySuggestion, never by rendering.
type Model struct {
	Target
	Label         string
	Tags          []Tag
	Hidden        bool
	CanWebConsole bool
	Dependencies  Dependencies
}
type Tag struct{ Name, Color string }
type Dependencies struct{ Identity, Project, Kubernetes, Docker string }

func Describe(cfg config.Config, target Target) (Model, error) {
	m := Model{Target: target, Label: target.Name, CanWebConsole: HasWebConsole(cfg, target)}
	var labels config.LabelSet
	exists := false
	switch target.Kind {
	case "workspace":
		v, ok := cfg.Destinations[target.Name]
		exists = ok
		labels = v.LabelSet
		m.Dependencies = Dependencies{v.Identity, v.Project, v.Kubernetes, v.Docker}
	case "identity":
		v, ok := cfg.Identities[target.Name]
		exists = ok
		m.Label, m.Hidden, labels = v.Account, v.Hidden, v.LabelSet
	case "project":
		v, ok := cfg.Projects[target.Name]
		exists = ok
		m.Label, m.Hidden, labels = v.ProjectID, v.Hidden, v.LabelSet
		m.Dependencies.Identity = v.PreferredIdentity
	case "kubernetes":
		v, ok := cfg.Kubernetes[target.Name]
		exists = ok
		m.Label, m.Hidden, labels = v.Cluster, v.Hidden, v.LabelSet
		if m.Label == "" {
			m.Label = v.Context
		}
		m.Dependencies.Identity, m.Dependencies.Project = v.PreferredIdentity, v.Project
	case "docker":
		v, ok := cfg.Docker[target.Name]
		exists = ok
		m.Label, m.Hidden, labels = v.Context, v.Hidden, v.LabelSet
	}
	if !exists {
		return Model{}, fmt.Errorf("unknown %s %q", target.Kind, target.Name)
	}
	if m.Label == "" {
		m.Label = target.Name
	}
	for _, name := range labels.TagNames() {
		color := cfg.Tags[name].Color
		if color == "" {
			color = config.DefaultTagColor
		}
		m.Tags = append(m.Tags, Tag{Name: name, Color: color})
	}
	return m, nil
}
