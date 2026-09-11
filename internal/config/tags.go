package config

import (
	"regexp"
	"slices"
	"sort"
)

type Tag struct {
	Color string `yaml:"color"`
}

const DefaultTagColor = "#94a3b8"

var tagColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func ValidTagColor(color string) bool { return tagColorPattern.MatchString(color) }
func (s LabelSet) TagNames() []string {
	names := slices.Clone(s.Tags)
	sort.Strings(names)
	return slices.Compact(names)
}

// Normalize explicit tag assignments and register any missing definitions.
// Provider and legacy labels remain metadata and never create tags.
func (c *Config) MigrateTags() {
	if c.Tags == nil {
		c.Tags = map[string]Tag{}
	}
	migrate := func(s LabelSet) LabelSet {
		s = s.Clone()
		if s.Tags != nil {
			s.TagsConfigured = true
		}
		names := s.TagNames()
		if len(names) > 0 || s.TagsConfigured {
			s.Tags = names
			s.TagsConfigured = true
		}
		for _, name := range names {
			if _, ok := c.Tags[name]; !ok {
				c.Tags[name] = Tag{Color: DefaultTagColor}
			}
		}
		return s
	}
	for name, v := range c.Identities {
		v.LabelSet = migrate(v.LabelSet)
		c.Identities[name] = v
	}
	for name, v := range c.Projects {
		v.LabelSet = migrate(v.LabelSet)
		c.Projects[name] = v
	}
	for name, v := range c.Kubernetes {
		v.LabelSet = migrate(v.LabelSet)
		c.Kubernetes[name] = v
	}
	for name, v := range c.Docker {
		v.LabelSet = migrate(v.LabelSet)
		c.Docker[name] = v
	}
	for name, v := range c.Destinations {
		v.LabelSet = migrate(v.LabelSet)
		c.Destinations[name] = v
	}
}
