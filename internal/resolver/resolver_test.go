package resolver

import (
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func TestDestinationDerivesSingleIdentity(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example", Identities: []string{"work"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "main", Location: "us-east1"}
	cfg.Destinations["destination"] = config.Destination{Kubernetes: "cluster"}

	resolved, err := Destination(cfg, "destination")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.WorkspaceName != "destination" || resolved.IdentityName != "work" || resolved.ProjectName != "project" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestDestinationRejectsAmbiguousIdentity(t *testing.T) {
	cfg := config.New()
	cfg.Identities["one"] = config.Identity{Provider: "gcp", Account: "one@example.com"}
	cfg.Identities["two"] = config.Identity{Provider: "gcp", Account: "two@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example", Identities: []string{"one", "two"}}
	cfg.Destinations["destination"] = config.Destination{Project: "project"}

	_, err := Destination(cfg, "destination")
	if err == nil || !strings.Contains(err.Error(), "explicit identity") {
		t.Fatalf("Destination() error = %v", err)
	}
}

func TestComponentsAllowsExplicitIdentityWithoutProject(t *testing.T) {
	cfg := config.New()
	cfg.Identities["person"] = config.Identity{Provider: "gcp", Account: "person@example.com"}

	resolved, err := Components(cfg, Selection{Name: "person@example.com", Identity: "person"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.IdentityName != "person" || resolved.Project != nil || resolved.Kubernetes != nil {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestComponentsAllowsProjectWithExplicitNoKubernetes(t *testing.T) {
	cfg := config.New()
	cfg.Identities["person"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "project-id", Identities: []string{"person"}}

	resolved, err := Components(cfg, Selection{Name: "project-id", Identity: "person", Project: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProjectName != "project" || resolved.Kubernetes != nil {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestKubernetesTargetIdentityUsesPhysicalGKECoordinates(t *testing.T) {
	project := config.Project{Provider: "gcp", ProjectID: "project-id"}
	generated := config.Kubernetes{Type: "gke", Project: "project", Location: "us-central1", Cluster: "cluster", Context: "gke_project-id_us-central1_cluster"}
	friendly := generated
	friendly.Context = "Friendly"
	if first, second := KubernetesTargetIdentity(generated, &project), KubernetesTargetIdentity(friendly, &project); first != second {
		t.Fatalf("GKE aliases differ: %q != %q", first, second)
	}
}

func TestSuggestedIdentityUsesExplicitPreferencesBeforeAmbiguity(t *testing.T) {
	cfg := config.New()
	cfg.Identities["first"] = config.Identity{Provider: "gcp", Account: "first@example.com"}
	cfg.Identities["second"] = config.Identity{Provider: "gcp", Account: "second@example.com"}
	cfg.Projects["project"] = config.Project{
		Provider: "gcp", ProjectID: "example", Identities: []string{"first", "second"}, PreferredIdentity: "first",
	}
	cfg.Kubernetes["cluster"] = config.Kubernetes{
		Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1", PreferredIdentity: "second",
	}

	identity, candidates, err := SuggestedIdentity(cfg, "project", "cluster")
	if err != nil {
		t.Fatal(err)
	}
	if identity != "second" || len(candidates) != 2 {
		t.Fatalf("cluster preference = %q, %#v", identity, candidates)
	}
	identity, _, err = SuggestedIdentity(cfg, "project", "")
	if err != nil || identity != "first" {
		t.Fatalf("project preference = %q, %v", identity, err)
	}
}

func TestComponentsAllowsExplicitCompatibleIdentityForUnmappedProject(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example"}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1"}

	resolved, err := Components(cfg, Selection{Name: "cluster", Identity: "work", Kubernetes: "cluster"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.IdentityName != "work" || resolved.ProjectName != "project" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestEverySelectionEntryPointResolvesTheSameContext(t *testing.T) {
	cfg := config.New()
	cfg.Identities["first"] = config.Identity{Provider: "gcp", Account: "first@example.com"}
	cfg.Identities["second"] = config.Identity{Provider: "gcp", Account: "second@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "example", Identities: []string{"first", "second"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{
		Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1", PreferredIdentity: "second",
	}
	cfg.Destinations["saved"] = config.Destination{Identity: "second", Project: "project", Kubernetes: "cluster"}

	selections := []Selection{
		{Name: "identity-first", Identity: "second", Project: "project", Kubernetes: "cluster"},
		{Name: "project-first", Project: "project", Kubernetes: "cluster"},
		{Name: "cluster-first", Kubernetes: "cluster"},
	}
	for _, selection := range selections {
		resolved, err := Components(cfg, selection)
		if err != nil {
			t.Fatalf("%s: %v", selection.Name, err)
		}
		if resolved.IdentityName != "second" || resolved.ProjectName != "project" || resolved.KubernetesName != "cluster" {
			t.Fatalf("%s resolved differently: %#v", selection.Name, resolved)
		}
	}
	workspace, err := Destination(cfg, "saved")
	if err != nil {
		t.Fatal(err)
	}
	if workspace.IdentityName != "second" || workspace.ProjectName != "project" || workspace.KubernetesName != "cluster" {
		t.Fatalf("workspace resolved differently: %#v", workspace)
	}
}

func TestDestinationOnlyRequiresADCWhenWorkspaceOptsIn(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Destinations["cli-only"] = config.Destination{Identity: "work"}
	cfg.Destinations["terraform"] = config.Destination{Identity: "work", ADC: "identity"}
	cliOnly, err := Destination(cfg, "cli-only")
	if err != nil {
		t.Fatal(err)
	}
	terraform, err := Destination(cfg, "terraform")
	if err != nil {
		t.Fatal(err)
	}
	if cliOnly.ADCMode != "" || terraform.ADCMode != "identity" {
		t.Fatalf("ADC modes = %q, %q", cliOnly.ADCMode, terraform.ADCMode)
	}
}
