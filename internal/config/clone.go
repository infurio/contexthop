package config

import (
	"maps"
	"slices"
)

// Clone returns an independently mutable catalog, including relationship slices.
func (source Config) Clone() Config {
	result := New()
	result.Version = source.Version
	result.PromptPrefix = source.PromptPrefix
	result.Tags = maps.Clone(source.Tags)
	result.Discovery = CloneDiscovery(source.Discovery)
	result.LegacyDestinations = map[string]Destination{}
	for name, item := range source.Identities {
		item.LabelSet = item.LabelSet.Clone()
		result.Identities[name] = item
	}
	for name, item := range source.Projects {
		item.Identities = slices.Clone(item.Identities)
		item.ManualIdentities = slices.Clone(item.ManualIdentities)
		item.LabelSet = item.LabelSet.Clone()
		result.Projects[name] = item
	}
	for name, item := range source.Kubernetes {
		item.LabelSet = item.LabelSet.Clone()
		result.Kubernetes[name] = item
	}
	for name, aliases := range source.KubernetesAliases {
		result.KubernetesAliases[name] = slices.Clone(aliases)
	}
	for name, item := range source.Docker {
		item.LabelSet = item.LabelSet.Clone()
		result.Docker[name] = item
	}
	for name, item := range source.Destinations {
		item.LabelSet = item.LabelSet.Clone()
		result.Destinations[name] = item
	}
	for name, item := range source.LegacyDestinations {
		item.LabelSet = item.LabelSet.Clone()
		result.LegacyDestinations[name] = item
	}
	return result
}
