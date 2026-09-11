package destination

import (
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func TestResolutionOnlyRequestsIdentityForARealChoice(t *testing.T) {
	cfg := config.New()
	cfg.Identities["alias-a"] = config.Identity{Provider: "gcp", Account: "alice@example.invalid"}
	cfg.Identities["alias-b"] = config.Identity{Provider: "gcp", Account: "bob@example.invalid"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "fictional", Identities: []string{"alias-a"}}
	cfg.Kubernetes["cloud"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "fictional", Location: "us-central1"}
	cfg.Kubernetes["local"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/fictional/config", Context: "fictional"}
	cfg.Docker["docker"] = config.Docker{Context: "fictional"}
	cfg.Destinations["workspace"] = config.Destination{Identity: "alias-a", Kubernetes: "cloud"}
	for _, target := range []Target{{"identity", "alias-a"}, {"project", "project"}, {"kubernetes", "cloud"}, {"kubernetes", "local"}, {"docker", "docker"}, {"workspace", "workspace"}} {
		r := Prepare(cfg, target)
		if r.Err != nil || len(r.Candidates) > 0 {
			t.Fatal("unambiguous launch asked a question", target, r)
		}
	}
	p := cfg.Projects["project"]
	p.Identities = append(p.Identities, "alias-b")
	cfg.Projects["project"] = p
	for _, target := range []Target{{"project", "project"}, {"kubernetes", "cloud"}} {
		r := Prepare(cfg, target)
		if r.Err == nil || len(r.Candidates) != 2 {
			t.Fatal("ambiguity was guessed", target, r)
		}
		chosen, err := Resolve(cfg, target, "alias-b")
		if err != nil || chosen.IdentityName != "alias-b" {
			t.Fatal("choice was not honored", chosen, err)
		}
	}
	p.PreferredIdentity = "alias-b"
	cfg.Projects["project"] = p
	for _, target := range []Target{{"project", "project"}, {"kubernetes", "cloud"}} {
		r := Prepare(cfg, target)
		if r.Err != nil || len(r.Candidates) > 0 || r.Resolved.IdentityName != "alias-b" {
			t.Fatal("preference was ignored", r)
		}
	}
	// An explicitly saved workspace must keep its identity despite project preference.
	if r := Prepare(cfg, Target{"workspace", "workspace"}); r.Err != nil || r.Resolved.IdentityName != "alias-a" {
		t.Fatal(r)
	}
}
