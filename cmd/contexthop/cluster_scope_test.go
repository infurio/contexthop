package main

import (
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
	"time"
)

func TestProjectClusterCountMatchesIdentityScope(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "project-id", Identities: []string{"work", "other"}}
	for _, name := range []string{"one", "two", "three"} {
		cfg.Kubernetes[name] = config.Kubernetes{Type: "gke", Project: "p", Cluster: name, Location: "region", Provenance: "kubeconfig"}
	}
	catalog.RecordDiscovery(cfg, "work", "", []string{"project-id"}, time.Now(), true)
	catalog.RecordDiscovery(cfg, "other", "project-id", []string{"region/one", "region/two", "region/three"}, time.Now(), true)
	if got := projectOptions(cfg, "work")[0].KubernetesContext; got != "not scanned" {
		t.Fatalf("project-only discovery falsely advertises clusters: %s", got)
	}
	if got := scopedKubernetesOptions(cfg, "p", "work"); len(got) != 0 {
		t.Fatal("unverified clusters became eligible")
	}
	draft := ui.Draft{ui.ScreenIdentity: "work", ui.ScreenProject: "p"}
	picker := interactiveBrowserPicker(cfg, cfg, ui.ScreenKubernetes, draft)
	if !strings.Contains(picker.Description, "Press d to scan this project") {
		t.Fatal("missing discovery guidance")
	}
	discover, _ := discoveryDialogFlow(cfg, cfg, ui.Choice{Screen: ui.ScreenKubernetes, Action: "discover-resources"}, draft)
	if discover.DraftUpdates[discoverProject] != "p" || discover.DraftUpdates[discoverScope] != "clusters" || discover.DraftUpdates[discoverIdentity] != "work" {
		t.Fatal("empty Kubernetes tab did not discover the selected project's clusters")
	}

	catalog.RecordDiscovery(cfg, "work", "project-id", []string{"region/two"}, time.Now(), true)
	if got := projectOptions(cfg, "work")[0].KubernetesContext; got != "1 cluster" {
		t.Fatalf("wrong identity count: %s", got)
	}
	options := scopedKubernetesOptions(cfg, "p", "work")
	if len(options) != 1 || options[0].Name != "two" {
		t.Fatal("cluster table disagrees with project count")
	}
	catalog.RecordDiscovery(cfg, "work", "project-id", nil, time.Now(), true)
	if got := projectOptions(cfg, "work")[0].KubernetesContext; got != "none" {
		t.Fatalf("empty scan shown as unscanned: %s", got)
	}
}

func TestKubernetesEligibilityCheckedBeforeAliasDeduplication(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Identities: []string{"work"}}
	catalog.RecordDiscovery(cfg, "work", "", []string{"p"}, time.Now(), true)
	cfg.Kubernetes["alias"] = config.Kubernetes{Type: "gke", Project: "p", Cluster: "cluster", Location: "region", Context: "friendly"}
	cfg.Kubernetes["eligible"] = config.Kubernetes{Type: "gke", Project: "p", Cluster: "cluster", Location: "region", ManualIdentity: "work"}
	options := scopedKubernetesOptions(cfg, "p", "work")
	if len(options) != 1 || options[0].Name != "eligible" {
		t.Fatalf("ineligible alias hid eligible context: %#v", options)
	}
	if got := kubernetesCountSummary(cfg, "p", "work"); got != "not scanned" {
		t.Fatalf("manual eligibility misrepresented as discovery: %s", got)
	}
}

func TestClusterDiscoveryStateLifecycle(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", Identities: []string{"work"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "p", Cluster: "cluster", Location: "region"}
	draft := ui.Draft{ui.ScreenIdentity: "work", ui.ScreenProject: "p"}
	check := func(want, notice string) {
		t.Helper()
		if got := kubernetesCountSummary(cfg, "p", "work"); got != want {
			t.Fatalf("state = %q, want %q", got, want)
		}
		picker := interactiveBrowserPicker(cfg, cfg, ui.ScreenKubernetes, draft)
		if !strings.Contains(picker.Description, notice) {
			t.Fatalf("missing guidance %q: %s", notice, picker.Description)
		}
	}
	check("not scanned", "haven't been discovered")
	catalog.RecordDiscovery(cfg, "work", "p", nil, time.Time{}, false)
	check("Scan failed", "discovery failed")
	catalog.RecordDiscovery(cfg, "work", "p", []string{"region/cluster"}, time.Now(), true)
	check("1 cluster", "")
	catalog.RecordDiscovery(cfg, "work", "p", nil, time.Time{}, false)
	check("1 cluster · stale", "out of date")
	if options := scopedKubernetesOptions(cfg, "p", "work"); len(options) != 1 {
		t.Fatal("failed refresh discarded previous results")
	}
	catalog.RecordDiscovery(cfg, "work", "p", []string{"region/cluster"}, time.Now().Add(-25*time.Hour), true)
	check("1 cluster · stale", "out of date")
	catalog.RecordDiscovery(cfg, "work", "p", nil, time.Now(), true)
	check("none", "No clusters were found")
	catalog.RecordDiscovery(cfg, "work", "p", nil, time.Time{}, false)
	check("none · stale", "out of date")
	identity := cfg.Identities["work"]
	identity.Account = "different@example.com"
	cfg.Identities["work"] = identity
	check("not scanned", "haven't been discovered")
}

func TestScopedProjectRowsDisplaySelectedIdentity(t *testing.T) {
	cfg := config.New()
	cfg.Identities["team-b"] = config.Identity{Provider: "gcp", Account: "developer@team-b.example.com"}
	cfg.Identities["team-a"] = config.Identity{Provider: "gcp", Account: "developer@team-a.example.com"}
	cfg.Projects["team-a"] = config.Project{Provider: "gcp", ProjectID: "team-a", Identities: []string{"team-b", "team-a"}}
	for _, name := range []string{"team-b", "team-a"} {
		options := sessionProjectOptions(cfg, name)
		if len(options) != 1 || options[0].IdentityAccount != cfg.Identities[name].Account {
			t.Fatalf("%s scoped rows: %#v", name, options)
		}
	}
	options := projectOptions(cfg, "")
	if len(options) != 1 || options[0].IdentityAccount != "2 identities" {
		t.Fatalf("unfiltered summary changed: %#v", options)
	}
}
