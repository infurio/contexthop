package catalog

import (
	"reflect"
	"testing"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

func relationshipConfig() config.Config {
	cfg := config.New()
	for _, name := range []string{"a", "b"} {
		cfg.Identities[name] = config.Identity{Provider: "gcp", Account: name + "@example.com", CloudSDKConfig: "/profiles/" + name}
	}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "project-id", Identities: []string{"a", "b"}}
	for _, name := range []string{"one", "two"} {
		cfg.Kubernetes[name] = config.Kubernetes{Type: "gke", Project: "p", Cluster: name, Location: "region"}
	}
	return cfg
}

func TestDiscoveryReconcilesOnlyCompleteIdentityScopes(t *testing.T) {
	cfg := relationshipConfig()
	now := time.Now().UTC().Truncate(time.Second)
	for _, name := range []string{"a", "b"} {
		RecordDiscovery(cfg, name, "", []string{"project-id"}, now, true)
	}
	RecordDiscovery(cfg, "a", "project-id", []string{"region/one"}, now, true)
	RecordDiscovery(cfg, "b", "project-id", []string{"region/two"}, now, true)
	if got := resolver.EligibleIdentities(cfg, "p", "one"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("cluster access leaked: %v", got)
	}
	// A failed refresh with an empty/partial response must retain previous facts.
	RecordDiscovery(cfg, "a", "project-id", nil, time.Time{}, false)
	scope := cfg.DiscoveryFor("a").Clusters["project-id"]
	if !scope.IsStale() || !resolver.KubernetesIdentityAvailable(cfg, "one", "a") {
		t.Fatal("failure discarded last successful scope")
	}
	RecordDiscovery(cfg, "a", "project-id", nil, now.Add(time.Second), true)
	if resolver.KubernetesIdentityAvailable(cfg, "one", "a") || !resolver.KubernetesIdentityAvailable(cfg, "two", "b") {
		t.Fatal("successful empty scope was not reconciled independently")
	}
	RecordDiscovery(cfg, "b", "", nil, now.Add(time.Second), true)
	if resolver.ProjectIdentityAvailable(cfg, "p", "b") || resolver.KubernetesIdentityAvailable(cfg, "two", "b") {
		t.Fatal("removed project still grants cluster availability")
	}
}

func TestDiscoveryProfileIsolationAndClone(t *testing.T) {
	cfg := relationshipConfig()
	RecordDiscovery(cfg, "a", "", []string{"project-id"}, time.Now(), true)
	RecordDiscovery(cfg, "a", "project-id", []string{"region/one"}, time.Now(), true)
	copy := cfg.Clone()
	scope := copy.Discovery["a"]
	scope.Projects.Resources[0] = "changed"
	scope.Clusters["project-id"].Resources[0] = "changed"
	if cfg.Discovery["a"].Projects.Resources[0] != "project-id" || cfg.Discovery["a"].Clusters["project-id"].Resources[0] != "region/one" {
		t.Fatal("clone shares evidence")
	}
	identity := cfg.Identities["a"]
	identity.CloudSDKConfig = "/different-profile"
	cfg.Identities["a"] = identity
	if cfg.DiscoveryFor("a").Projects != nil {
		t.Fatal("reused discovery across credential profiles")
	}
}

func TestDiscoverySavePersistsScopeUpdatesAndPreferences(t *testing.T) {
	saved := relationshipConfig()
	observed := saved.Clone()
	RecordDiscovery(observed, "a", "", []string{"project-id"}, time.Now(), true)
	RecordDiscovery(observed, "a", "project-id", []string{"region/one"}, time.Now(), true)
	p := observed.Kubernetes["one"]
	p.Hidden = true
	observed.Kubernetes["one"] = p
	plan := PlanSaveDiscovery(saved, observed)
	if !plan.Valid() || len(plan.Changes) == 0 || plan.Config.DiscoveryFor("a").Projects == nil {
		t.Fatalf("snapshot-only save failed: %+v", plan)
	}
	if plan.Config.Kubernetes["one"].Hidden {
		t.Fatal("discovery changed user preference")
	}
	preferred, err := PlanPreferredIdentity(plan.Config, Ref{KindKubernetes, "one"}, "a")
	if err != nil || !preferred.Valid() {
		t.Fatalf("preference failed: %v %v", err, preferred.Problems)
	}
	if _, err := PlanPreferredIdentity(plan.Config, Ref{KindKubernetes, "one"}, "b"); err == nil {
		t.Fatal("accepted an unobserved cluster identity")
	}
	cleared, err := PlanPreferredIdentity(preferred.Config, Ref{KindKubernetes, "one"}, "")
	if err != nil || !cleared.Valid() || cleared.Config.Kubernetes["one"].PreferredIdentity != "" {
		t.Fatal("could not clear preference")
	}
}

func TestPlainProjectDeletionAndUnmapping(t *testing.T) {
	cfg := relationshipConfig()
	cfg.Kubernetes = map[string]config.Kubernetes{"plain": {Type: "kubeconfig", Project: "p", Kubeconfig: "/tmp/kube", Context: "plain", PreferredIdentity: "a"}}
	deleted, err := PlanRemove(cfg, Ref{KindProject, "p"})
	if err != nil || deleted.Valid() {
		t.Fatal("accepted dangling kubeconfig project")
	}
	unmap, err := PlanRemoveKubernetesProject(cfg, "plain")
	if err != nil || !unmap.Valid() || unmap.Config.Kubernetes["plain"].PreferredIdentity != "" {
		t.Fatalf("unmap failed: %v %v", err, unmap.Problems)
	}
}

func TestDiscoveryDoesNotChangeExistingWorkspaceActivation(t *testing.T) {
	cfg := relationshipConfig()
	cfg.Destinations["work"] = config.Destination{Identity: "a", Project: "p", Kubernetes: "one"}
	before, err := resolver.Destination(cfg, "work")
	if err != nil {
		t.Fatal(err)
	}
	observed := cfg.Clone()
	RecordDiscovery(observed, "a", "", nil, time.Now(), true)
	plan := PlanSaveDiscovery(cfg, observed)
	if !plan.Valid() || len(plan.Impacts) != 0 {
		t.Fatalf("discovery affected workspace: %+v", plan)
	}
	after, err := resolver.Destination(plan.Config, "work")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("workspace changed: %v", err)
	}
	if _, err := resolver.Components(plan.Config, resolver.Selection{Identity: "a", Project: "p", Kubernetes: "one"}); err == nil {
		t.Fatal("direct selection ignored current discovery")
	}
}

func TestInitialRefreshFailureKeepsLegacyClusterChoices(t *testing.T) {
	cfg := relationshipConfig()
	RecordDiscovery(cfg, "a", "", nil, time.Time{}, false)
	if !resolver.KubernetesIdentityAvailable(cfg, "one", "a") {
		t.Fatal("initial failed refresh removed legacy choice")
	}
}

func TestPreferenceDoesNotInventDiscoveredClusterAccess(t *testing.T) {
	cfg := relationshipConfig()
	target := cfg.Kubernetes["one"]
	target.PreferredIdentity = "a"
	cfg.Kubernetes["one"] = target
	RecordDiscovery(cfg, "a", "", []string{"project-id"}, time.Now(), true)
	if resolver.KubernetesIdentityAvailable(cfg, "one", "a") {
		t.Fatal("preference was treated as discovery evidence")
	}
}
