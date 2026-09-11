package config

import (
	"maps"
	"slices"
	"sort"
	"strings"
)

// LabelSet keeps imported values separate from explicit local overrides.
type LabelSet struct {
	Tags           []string          `yaml:"tags,omitempty"`
	TagsConfigured bool              `yaml:"tagsConfigured,omitempty"`
	GoogleLabels   map[string]string `yaml:"googleLabels,omitempty"`
	Labels         map[string]string `yaml:"labels,omitempty"`
}

func (s LabelSet) Clone() LabelSet {
	return LabelSet{Tags: slices.Clone(s.Tags), TagsConfigured: s.TagsConfigured, GoogleLabels: maps.Clone(s.GoogleLabels), Labels: maps.Clone(s.Labels)}
}
func (s LabelSet) Effective(legacyRisk string) map[string]string {
	result := map[string]string{}
	maps.Copy(result, s.GoogleLabels)
	if legacyRisk != "" {
		result["risk"] = legacyRisk
	}
	maps.Copy(result, s.Labels)
	return result
}
func (s LabelSet) Summary(legacyRisk string) string {
	values := s.Effective(legacyRisk)
	parts := []string{}
	for k, v := range values {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func (c *Config) MigrateLabels() {
	for name, v := range c.Projects {
		if v.Risk != "" {
			if v.Labels == nil {
				v.Labels = map[string]string{}
			}
			if _, ok := v.Labels["risk"]; !ok {
				v.Labels["risk"] = v.Risk
			}
			v.Risk = ""
			c.Projects[name] = v
		}
	}
	for name, v := range c.Kubernetes {
		if v.Risk != "" {
			if v.Labels == nil {
				v.Labels = map[string]string{}
			}
			if _, ok := v.Labels["risk"]; !ok {
				v.Labels["risk"] = v.Risk
			}
			v.Risk = ""
			c.Kubernetes[name] = v
		}
	}
	for name, v := range c.Docker {
		if v.Risk != "" {
			if v.Labels == nil {
				v.Labels = map[string]string{}
			}
			if _, ok := v.Labels["risk"]; !ok {
				v.Labels["risk"] = v.Risk
			}
			v.Risk = ""
			c.Docker[name] = v
		}
	}
	for name, v := range c.Destinations {
		if v.Risk != "" {
			if v.Labels == nil {
				v.Labels = map[string]string{}
			}
			if _, ok := v.Labels["risk"]; !ok {
				v.Labels["risk"] = v.Risk
			}
			v.Risk = ""
			c.Destinations[name] = v
		}
	}
}
