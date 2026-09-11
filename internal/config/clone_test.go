package config

import "testing"

func TestCloneOwnsAllCollections(t *testing.T) {
	cfg := New()
	cfg.LegacyDestinations = map[string]Destination{"legacy": {Project: "p"}}
	cfg.Projects["p"] = Project{Identities: []string{"work"}}
	cfg.KubernetesAliases["k"] = []KubernetesAlias{{Context: "original"}}
	cfg.Destinations["workspace"] = Destination{Project: "p"}
	copy := cfg.Clone()
	copy.Projects["p"].Identities[0] = "other"
	copy.KubernetesAliases["k"][0].Context = "changed"
	delete(copy.Destinations, "workspace")
	delete(copy.LegacyDestinations, "legacy")
	if cfg.Projects["p"].Identities[0] != "work" || cfg.KubernetesAliases["k"][0].Context != "original" || len(cfg.Destinations) != 1 || len(cfg.LegacyDestinations) != 1 {
		t.Fatal("clone shares nested collections")
	}
}
