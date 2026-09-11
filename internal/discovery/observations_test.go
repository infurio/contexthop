package discovery

import (
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func TestNormalizeMergesExactKubernetesAliasesAndPrefersFriendlyContext(t *testing.T) {
	access := "same-cluster-and-user-definition"
	observations := Observations{
		Identities:    []IdentityObservation{{Provider: "gcp", Kind: "user", Account: "person@example.com", ObservedAt: "2026-09-04T00:00:00Z"}},
		GCloudConfigs: []GCloudConfigurationObservation{{Name: "team-b", Account: "person@example.com", ProjectID: "team-b-1234", ObservedAt: "2026-09-04T00:00:00Z"}},
		KubernetesContexts: []KubernetesContextObservation{
			{Source: "/config", Context: "Team-B", ClusterReference: "gke_team-b-1234_us-central1-b_team-b-main", UserReference: "gke_team-b-1234_us-central1-b_team-b-main", AccessSignature: access, ClusterSignature: "cluster", GKEProjectID: "team-b-1234", GKELocation: "us-central1-b", GKECluster: "team-b-main", ObservedAt: "2026-09-04T00:00:00Z"},
			{Source: "/config", Context: "gke_team-b-1234_us-central1-b_team-b-main", ClusterReference: "gke_team-b-1234_us-central1-b_team-b-main", UserReference: "gke_team-b-1234_us-central1-b_team-b-main", AccessSignature: access, ClusterSignature: "cluster", GKEProjectID: "team-b-1234", GKELocation: "us-central1-b", GKECluster: "team-b-main", ObservedAt: "2026-09-04T00:00:00Z"},
		},
	}

	result, err := Normalize(observations)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Config.Kubernetes) != 1 {
		t.Fatalf("targets = %#v", result.Config.Kubernetes)
	}
	target := result.Config.Kubernetes["team-b"]
	if target.Type != "gke" || target.Context != "Team-B" || target.Project != "team-b-1234" || target.Cluster != "team-b-main" || target.AccessID == "" || target.ProviderID == "" {
		t.Fatalf("target = %#v", target)
	}
	if aliases := result.Config.KubernetesAliases["team-b"]; len(aliases) != 2 {
		t.Fatalf("aliases = %#v", aliases)
	}
	if len(result.Config.Destinations) != 0 {
		t.Fatalf("discovery created user workspaces: %#v", result.Config.Destinations)
	}
}

func TestNormalizeKeepsNamespaceOnAliasesWithoutSplittingAccessProfile(t *testing.T) {
	base := KubernetesContextObservation{
		Source: "/config", ClusterReference: "gke_example_us-east1_cluster", UserReference: "person",
		AccessSignature: "same-endpoint-tls-and-user", GKEProjectID: "example", GKELocation: "us-east1", GKECluster: "cluster",
		ObservedAt: "2026-09-04T00:00:00Z",
	}
	payments, operations := base, base
	payments.Context, payments.Namespace = "payments", "payments"
	operations.Context, operations.Namespace = "operations", "operations"

	result, err := Normalize(Observations{KubernetesContexts: []KubernetesContextObservation{payments, operations}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Config.Kubernetes) != 1 {
		t.Fatalf("namespace presets split access profiles: %#v", result.Config.Kubernetes)
	}
	var aliases []config.KubernetesAlias
	for name := range result.Config.Kubernetes {
		aliases = result.Config.KubernetesAliases[name]
	}
	if len(aliases) != 2 || aliases[0].Namespace != "operations" || aliases[1].Namespace != "payments" {
		t.Fatalf("namespace aliases = %#v", aliases)
	}
}

func TestReconcileMigratesNamespaceSensitiveAccessIDs(t *testing.T) {
	current := config.New()
	current.Projects["example"] = config.Project{Provider: "gcp", ProjectID: "example"}
	for name, namespace := range map[string]string{"payments": "payments", "operations": "operations"} {
		current.Kubernetes[name] = config.Kubernetes{
			Type: "gke", Project: "example", Cluster: "cluster", Location: "us-east1",
			ProviderID: "gke:example/us-east1/cluster", AccessID: "old-namespace-sensitive-" + name,
			Kubeconfig: "/config", Context: name, Namespace: namespace,
		}
	}
	current.Destinations["work"] = config.Destination{Kubernetes: "payments"}

	observed, err := Normalize(Observations{KubernetesContexts: []KubernetesContextObservation{
		{Source: "/config", Context: "payments", Namespace: "payments", AccessSignature: "normalized-access", GKEProjectID: "example", GKELocation: "us-east1", GKECluster: "cluster", ObservedAt: "2026-09-04T00:00:00Z"},
		{Source: "/config", Context: "operations", Namespace: "operations", AccessSignature: "normalized-access", GKEProjectID: "example", GKELocation: "us-east1", GKECluster: "cluster", ObservedAt: "2026-09-04T00:00:00Z"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reconciled, report, err := Reconcile(current, observed.Config)
	if err != nil {
		t.Fatal(err)
	}
	if len(reconciled.Kubernetes) != 1 || report.AliasesMerged != 1 {
		t.Fatalf("migration result = %#v, report %#v", reconciled.Kubernetes, report)
	}
	workspaceTarget := reconciled.Destinations["work"].Kubernetes
	if _, ok := reconciled.Kubernetes[workspaceTarget]; !ok {
		t.Fatalf("workspace was not remapped: %#v", reconciled.Destinations["work"])
	}
	if aliases := reconciled.KubernetesAliases[workspaceTarget]; len(aliases) != 2 {
		t.Fatalf("migrated aliases = %#v", aliases)
	}
}

func TestReconcileRefreshesHiddenResourcesWithoutUnhidingThem(t *testing.T) {
	observed := config.New()
	observed.Identities["person"] = config.Identity{Provider: "gcp", Kind: "user", Account: "person@example.com", Provenance: "gcp", VerifiedBy: "gcp", ObservedAt: "2026-09-04T00:00:00Z"}
	observed.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example", Identities: []string{"person"}, Provenance: "gcp", VerifiedBy: "gcp", ObservedAt: "2026-09-04T00:00:00Z"}
	observed.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-central1", ProviderID: "gke:example/us-central1/cluster", AccessID: "sha256:access", Provenance: "gcp", VerifiedBy: "gcp", ObservedAt: "2026-09-04T00:00:00Z"}
	observed.Docker["desktop"] = config.Docker{Context: "desktop-linux", Provenance: "docker", VerifiedBy: "docker", ObservedAt: "2026-09-04T00:00:00Z"}

	current := cloneConfig(observed)
	identity := current.Identities["person"]
	identity.Hidden = true
	current.Identities["person"] = identity
	project := current.Projects["project"]
	project.Hidden = true
	current.Projects["project"] = project
	target := current.Kubernetes["cluster"]
	target.Hidden = true
	current.Kubernetes["cluster"] = target
	docker := current.Docker["desktop"]
	docker.Hidden = true
	current.Docker["desktop"] = docker

	reconciled, _, err := Reconcile(current, observed)
	if err != nil {
		t.Fatal(err)
	}
	if !reconciled.Identities["person"].Hidden || !reconciled.Projects["project"].Hidden || !reconciled.Kubernetes["cluster"].Hidden || !reconciled.Docker["desktop"].Hidden {
		t.Fatalf("discovery resurfaced hidden resources: %#v %#v %#v %#v", reconciled.Identities["person"], reconciled.Projects["project"], reconciled.Kubernetes["cluster"], reconciled.Docker["desktop"])
	}
}

func TestDeletedDiscoveredResourceCanBeImportedAgain(t *testing.T) {
	observed := config.New()
	observed.Identities["person"] = config.Identity{Provider: "gcp", Kind: "user", Account: "person@example.com", Provenance: "gcp", VerifiedBy: "gcp", ObservedAt: "2026-09-04T00:00:00Z"}
	current := config.New() // The local record, including any hidden bit, was deleted.

	reconciled, report, err := Reconcile(current, observed)
	if err != nil {
		t.Fatal(err)
	}
	identity, ok := reconciled.Identities["person"]
	if !ok || identity.Hidden || report.New != 1 {
		t.Fatalf("rediscovered identity = %#v, report %#v", identity, report)
	}
}

func TestNormalizeKeepsDistinctAccessProfilesForSamePhysicalCluster(t *testing.T) {
	base := KubernetesContextObservation{
		Source: "/config", ClusterReference: "gke_example_us-east1_cluster",
		GKEProjectID: "example", GKELocation: "us-east1", GKECluster: "cluster",
		ObservedAt: "2026-09-04T00:00:00Z",
	}
	first, second := base, base
	first.Context, first.AccessSignature = "developer", "developer-user"
	second.Context, second.AccessSignature = "administrator", "administrator-user"

	result, err := Normalize(Observations{KubernetesContexts: []KubernetesContextObservation{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Config.Kubernetes) != 2 {
		t.Fatalf("distinct access profiles were collapsed: %#v", result.Config.Kubernetes)
	}
	for _, target := range result.Config.Kubernetes {
		if target.ProviderID != "gke:example/us-east1/cluster" {
			t.Fatalf("provider identity = %q", target.ProviderID)
		}
	}
}

func TestReconcileUpgradesLegacyAliasesAndRemapsWorkspaces(t *testing.T) {
	current := config.New()
	current.Identities["team-b"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	current.Projects["team-b-1234"] = config.Project{Provider: "gcp", ProjectID: "team-b-1234", Identities: []string{"team-b"}}
	current.Kubernetes["team-b"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/config", Context: "Team-B"}
	current.Kubernetes["generated"] = config.Kubernetes{Type: "gke", Project: "team-b-1234", Cluster: "team-b-main", Location: "us-central1-b", Kubeconfig: "/config", Context: "gke_team-b-1234_us-central1-b_team-b-main"}
	current.Destinations["work"] = config.Destination{Identity: "team-b", Project: "team-b-1234", Kubernetes: "generated"}

	observedResult, err := Normalize(Observations{
		Identities:    []IdentityObservation{{Provider: "gcp", Kind: "user", Account: "person@example.com", ObservedAt: "2026-09-04T00:00:00Z"}},
		GCloudConfigs: []GCloudConfigurationObservation{{Account: "person@example.com", ProjectID: "team-b-1234", ObservedAt: "2026-09-04T00:00:00Z"}},
		KubernetesContexts: []KubernetesContextObservation{
			{Source: "/config", Context: "Team-B", ClusterReference: "gke_team-b-1234_us-central1-b_team-b-main", AccessSignature: "same", GKEProjectID: "team-b-1234", GKELocation: "us-central1-b", GKECluster: "team-b-main", ObservedAt: "2026-09-04T00:00:00Z"},
			{Source: "/config", Context: "gke_team-b-1234_us-central1-b_team-b-main", ClusterReference: "gke_team-b-1234_us-central1-b_team-b-main", AccessSignature: "same", GKEProjectID: "team-b-1234", GKELocation: "us-central1-b", GKECluster: "team-b-main", ObservedAt: "2026-09-04T00:00:00Z"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	reconciled, report, err := Reconcile(current, observedResult.Config)
	if err != nil {
		t.Fatal(err)
	}
	if len(reconciled.Kubernetes) != 1 || reconciled.Kubernetes["team-b"].Context != "Team-B" {
		t.Fatalf("reconciled targets = %#v", reconciled.Kubernetes)
	}
	if reconciled.Destinations["work"].Kubernetes != "team-b" {
		t.Fatalf("workspace was not remapped: %#v", reconciled.Destinations["work"])
	}
	if report.AliasesMerged != 1 || len(reconciled.KubernetesAliases["team-b"]) != 2 {
		t.Fatalf("report = %#v, aliases = %#v", report, reconciled.KubernetesAliases["team-b"])
	}

	repeated, repeatedReport, err := Reconcile(reconciled, observedResult.Config)
	if err != nil {
		t.Fatal(err)
	}
	if repeatedReport.Changed() || len(repeated.Kubernetes) != 1 {
		t.Fatalf("reconciliation is not idempotent: %#v", repeatedReport)
	}
}
