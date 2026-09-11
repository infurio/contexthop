package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func TestBuildAndIdentityTreeAreDeterministic(t *testing.T) {
	cfg := testConfig()
	graph := Build(cfg)
	if len(graph.Nodes) != 9 {
		t.Fatalf("nodes = %#v", graph.Nodes)
	}
	wantEdges := []Edge{
		{From: Ref{KindIdentity, "admin"}, To: Ref{KindProject, "prod"}, Relation: IdentityAccess},
		{From: Ref{KindIdentity, "person"}, To: Ref{KindProject, "dev"}, Relation: IdentityAccess},
		{From: Ref{KindProject, "dev"}, To: Ref{KindKubernetes, "dev-cluster"}, Relation: CloudProject},
		{From: Ref{KindProject, "prod"}, To: Ref{KindKubernetes, "prod-cluster"}, Relation: CloudProject},
		{From: Ref{KindProject, "dev"}, To: Ref{KindWorkspace, "dev"}, Relation: UsesComponent},
		{From: Ref{KindKubernetes, "dev-cluster"}, To: Ref{KindWorkspace, "dev"}, Relation: UsesComponent},
		{From: Ref{KindIdentity, "person"}, To: Ref{KindWorkspace, "dev"}, Relation: UsesComponent},
		{From: Ref{KindDocker, "local"}, To: Ref{KindWorkspace, "local"}, Relation: UsesComponent},
	}
	for _, edge := range wantEdges {
		if !slices.Contains(graph.Edges, edge) {
			t.Errorf("missing edge %#v in %#v", edge, graph.Edges)
		}
	}
	tree := IdentityTree(cfg)
	if len(tree) != 2 || tree[0].Node.Ref != (Ref{KindIdentity, "admin"}) || tree[1].Node.Ref != (Ref{KindIdentity, "person"}) {
		t.Fatalf("tree roots = %#v", tree)
	}
	if got := tree[1].Children[0]; got.Node.Ref != (Ref{KindProject, "dev"}) || len(got.Children) != 1 || got.Children[0].Node.Ref != (Ref{KindKubernetes, "dev-cluster"}) {
		t.Fatalf("identity branch = %#v", got)
	}
	if second := Build(cfg); !slices.Equal(graph.Nodes, second.Nodes) || !slices.Equal(graph.Edges, second.Edges) {
		t.Fatal("graph output is not deterministic")
	}
}

func TestGuidedAddPlansUseSharedValidationAndRejectDuplicates(t *testing.T) {
	cfg := testConfig()
	identityPlan, err := PlanAddIdentity(cfg, "new-person", config.Identity{Provider: "gcp", Account: "new@example.com"})
	if err != nil || !identityPlan.Valid() || identityPlan.Config.Identities["new-person"].Account != "new@example.com" {
		t.Fatalf("identity plan = %#v, %v", identityPlan, err)
	}
	if _, err := PlanAddIdentity(cfg, "person", config.Identity{Provider: "gcp", Account: "duplicate@example.com"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate identity error = %v", err)
	}

	projectPlan, err := PlanAddProject(cfg, "new-project", config.Project{Provider: "gcp", ProjectID: "new-project", Identities: []string{"person"}})
	if err != nil || !projectPlan.Valid() {
		t.Fatalf("project plan = %#v, %v", projectPlan, err)
	}
	badProject, err := PlanAddProject(cfg, "bad-project", config.Project{Provider: "gcp", ProjectID: "bad-project", Identities: []string{"missing"}})
	if err != nil || badProject.Valid() || !strings.Contains(strings.Join(badProject.Problems, " "), "unknown identity") {
		t.Fatalf("bad project plan = %#v, %v", badProject, err)
	}

	kubernetesPlan, err := PlanAddKubernetes(cfg, "plain", config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/config", Context: "plain"})
	if err != nil || !kubernetesPlan.Valid() {
		t.Fatalf("Kubernetes plan = %#v, %v", kubernetesPlan, err)
	}
	badGKE, err := PlanAddKubernetes(cfg, "bad-gke", config.Kubernetes{Type: "gke", Project: "dev", Cluster: "cluster"})
	if err != nil || badGKE.Valid() || !strings.Contains(strings.Join(badGKE.Problems, " "), "requires project, cluster, and location") {
		t.Fatalf("bad GKE plan = %#v, %v", badGKE, err)
	}

	dockerPlan, err := PlanAddDocker(cfg, "remote", config.Docker{Context: "remote"})
	if err != nil || !dockerPlan.Valid() {
		t.Fatalf("Docker plan = %#v, %v", dockerPlan, err)
	}
	workspacePlan, err := PlanAddWorkspace(cfg, "new-workspace", config.Destination{Identity: "person", Project: "dev"})
	if err != nil || !workspacePlan.Valid() {
		t.Fatalf("workspace plan = %#v, %v", workspacePlan, err)
	}
	impact := findImpact(t, workspacePlan, "new-workspace")
	if impact.Before.Valid || !impact.After.Valid || impact.After.Project != "dev" {
		t.Fatalf("workspace add impact = %#v", impact)
	}
	if _, err := PlanAddWorkspace(cfg, "dev", config.Destination{Docker: "local"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate workspace error = %v", err)
	}
}

func TestConfigureGKEAtomicallyMapsIdentityAndAddsDNSProfile(t *testing.T) {
	cfg := testConfig()
	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	target := config.Kubernetes{
		Type: "gke", Project: "dev", Cluster: "example-sites-dev", Location: "us-central1",
		ProviderID: "gke:dev-project/us-central1/example-sites-dev", Endpoint: "dns",
	}
	plan, err := PlanConfigureGKE(cfg, "example-sites-dev-dns", target, "other")
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	if !slices.Contains(plan.Config.Projects["dev"].Identities, "other") {
		t.Fatal("identity mapping was not staged")
	}
	got := plan.Config.Kubernetes["example-sites-dev-dns"]
	if got.Endpoint != "dns" || got.PreferredIdentity != "other" {
		t.Fatalf("GKE target = %#v", got)
	}
	if len(plan.Changes) != 2 || plan.Changes[0].Action != "map" || plan.Changes[1].Action != "add" {
		t.Fatalf("changes = %#v", plan.Changes)
	}
	if _, exists := cfg.Kubernetes["example-sites-dev-dns"]; exists || slices.Contains(cfg.Projects["dev"].Identities, "other") {
		t.Fatal("planning mutated its input")
	}
}

func TestConfigureGKEReusesMatchingAccessProfile(t *testing.T) {
	cfg := testConfig()
	cfg.Kubernetes["dns"] = config.Kubernetes{Type: "gke", Project: "dev", Cluster: "cluster", Location: "us-central1", Endpoint: "dns", Kubeconfig: "/existing", Context: "existing"}
	target := config.Kubernetes{Type: "gke", Project: "dev", Cluster: "cluster", Location: "us-central1", Endpoint: "dns"}
	plan, err := PlanConfigureGKE(cfg, "dns", target, "person")
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	got := plan.Config.Kubernetes["dns"]
	if got.Kubeconfig != "/existing" || got.Context != "existing" || got.PreferredIdentity != "person" {
		t.Fatalf("existing access profile was not preserved: %#v", got)
	}
}

func TestPlanUpdateWorkspaceAccumulatesOneAtomicDraft(t *testing.T) {
	cfg := config.New()
	cfg.Identities["person"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["dev"] = config.Project{Provider: "gcp", ProjectID: "dev", Identities: []string{"person"}}
	cfg.Docker["local"] = config.Docker{Context: "desktop-linux"}
	cfg.Destinations["work"] = config.Destination{Identity: "person"}

	updated := cfg.Destinations["work"]
	updated.Project = "dev"
	updated.Docker = "local"
	updated.ADC = "identity"
	updated.Risk = "development"
	plan, err := PlanUpdateWorkspace(cfg, "work", updated)
	if err != nil || !plan.Valid() {
		t.Fatalf("workspace update plan = %#v, %v", plan, err)
	}
	if got := plan.Config.Destinations["work"]; !reflect.DeepEqual(got, updated) {
		t.Fatalf("updated workspace = %#v, want %#v", got, updated)
	}
	joined := ""
	for _, change := range plan.Changes {
		joined += change.Action + " " + change.From.Name + " " + change.To.Name + " " + change.Detail + "\n"
	}
	for _, want := range []string{"map dev work", "map local work", "ADC: automatic → identity", "risk: automatic → development"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("workspace update changes missing %q:\n%s", want, joined)
		}
	}
	if len(plan.Impacts) != 1 || plan.Impacts[0].Workspace != "work" {
		t.Fatalf("workspace update impacts = %#v", plan.Impacts)
	}
}

func TestRemoveDoesNotSilentlyCascadeDependants(t *testing.T) {
	cfg := testConfig()
	plan, err := PlanRemove(cfg, Ref{KindProject, "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() || !strings.Contains(strings.Join(plan.Problems, " "), "unknown project") {
		t.Fatalf("project removal plan = %#v", plan)
	}
	impact := findImpact(t, plan, "dev")
	if !impact.Before.Valid || impact.After.Valid {
		t.Fatalf("impact = %#v", impact)
	}
	if _, exists := plan.Config.Kubernetes["dev-cluster"]; !exists {
		t.Fatal("dependent target was silently deleted")
	}
	if _, exists := plan.Config.Destinations["dev"]; !exists {
		t.Fatal("dependent workspace was silently deleted")
	}

	workspacePlan, err := PlanRemove(cfg, Ref{KindWorkspace, "local"})
	if err != nil || !workspacePlan.Valid() {
		t.Fatalf("workspace removal plan = %#v, %v", workspacePlan, err)
	}
	removed := findImpact(t, workspacePlan, "local")
	if !removed.Before.Valid || removed.After.Name != "" {
		t.Fatalf("workspace removal impact = %#v", removed)
	}

	cfg.Docker["unused"] = config.Docker{Context: "unused"}
	componentPlan, err := PlanRemove(cfg, Ref{KindDocker, "unused"})
	if err != nil || !componentPlan.Valid() {
		t.Fatalf("unreferenced component removal = %#v, %v", componentPlan, err)
	}
}

func TestForgetRemovesDiscoverableEntityAndKubernetesAliases(t *testing.T) {
	cfg := config.New()
	cfg.Identities["unused"] = config.Identity{Provider: "gcp", Account: "unused@example.com"}
	cfg.Kubernetes["unused"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/kubeconfig", Context: "unused"}
	cfg.KubernetesAliases["unused"] = []config.KubernetesAlias{{Context: "unused-alias"}}

	identityPlan, err := PlanForget(cfg, Ref{KindIdentity, "unused"})
	if err != nil || !identityPlan.Valid() {
		t.Fatalf("identity forget plan = %#v, %v", identityPlan, err)
	}
	if _, exists := identityPlan.Config.Identities["unused"]; exists || len(identityPlan.Changes) != 1 || identityPlan.Changes[0].Action != "forget" {
		t.Fatalf("identity forget plan = %#v", identityPlan)
	}

	kubernetesPlan, err := PlanForget(cfg, Ref{KindKubernetes, "unused"})
	if err != nil || !kubernetesPlan.Valid() {
		t.Fatalf("Kubernetes forget plan = %#v, %v", kubernetesPlan, err)
	}
	if _, exists := kubernetesPlan.Config.Kubernetes["unused"]; exists {
		t.Fatal("Kubernetes target was not forgotten")
	}
	if _, exists := kubernetesPlan.Config.KubernetesAliases["unused"]; exists {
		t.Fatal("Kubernetes aliases were not forgotten with their access profile")
	}
	if _, err := PlanForget(cfg, Ref{KindWorkspace, "unused"}); err == nil {
		t.Fatal("user-owned workspace was accepted by forget")
	}
}

func TestHidePreservesResourcesRelationshipsAndCanBeReversed(t *testing.T) {
	cfg := testConfig()
	ref := Ref{KindIdentity, "person"}
	plan, err := PlanSetHidden(cfg, ref, true)
	if err != nil || !plan.Valid() {
		t.Fatalf("hide plan = %#v, %v", plan, err)
	}
	if !plan.Config.Identities["person"].Hidden || len(plan.Changes) != 1 || plan.Changes[0].Action != "hide" {
		t.Fatalf("hidden identity = %#v, changes %#v", plan.Config.Identities["person"], plan.Changes)
	}
	if !slices.Contains(plan.Config.Projects["dev"].Identities, "person") || plan.Config.Destinations["dev"].Identity != "person" {
		t.Fatalf("hide changed relationships: project %#v, workspace %#v", plan.Config.Projects["dev"], plan.Config.Destinations["dev"])
	}
	if len(plan.Impacts) != 0 {
		t.Fatalf("hide changed workspace resolution: %#v", plan.Impacts)
	}

	unhide, err := PlanSetHidden(plan.Config, ref, false)
	if err != nil || !unhide.Valid() || unhide.Config.Identities["person"].Hidden || len(unhide.Changes) != 1 || unhide.Changes[0].Action != "unhide" {
		t.Fatalf("unhide plan = %#v, %v", unhide, err)
	}
}

func TestEveryCatalogResourceCanBeHidden(t *testing.T) {
	cfg := testConfig()
	for _, ref := range []Ref{
		{KindIdentity, "person"},
		{KindProject, "dev"},
		{KindKubernetes, "dev-cluster"},
		{KindDocker, "local"},
		{KindWorkspace, "dev"},
	} {
		plan, err := PlanSetHidden(cfg, ref, true)
		if err != nil || !plan.Valid() || len(plan.Changes) != 1 || plan.Changes[0].Action != "hide" {
			t.Errorf("hide %s = %#v, %v", ref.Kind, plan, err)
		}
	}
}

func TestReverseShowsDependenciesAndDependants(t *testing.T) {
	reverse := Reverse(testConfig(), Ref{KindProject, "dev"})
	want := []Ref{{KindIdentity, "person"}, {KindKubernetes, "dev-cluster"}, {KindWorkspace, "dev"}}
	if len(reverse.Children) != len(want) {
		t.Fatalf("reverse = %#v", reverse)
	}
	for index, child := range reverse.Children {
		if child.Node.Ref != want[index] {
			t.Fatalf("child %d = %#v, want %#v", index, child.Node.Ref, want[index])
		}
	}
}

func TestIdentityMappingHonorsProviderAndPreviewsInvalidRemoval(t *testing.T) {
	cfg := testConfig()
	cfg.Identities["aws"] = config.Identity{Provider: "aws", Account: "arn:example"}
	if _, err := PlanMapIdentity(cfg, "dev", "aws"); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("provider mismatch error = %v", err)
	}

	plan, err := PlanRemoveIdentityMapping(cfg, "dev", "person")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatalf("plan unexpectedly valid: %#v", plan)
	}
	impact := findImpact(t, plan, "dev")
	if !impact.Before.Valid || impact.After.Valid || !strings.Contains(impact.After.Error, "not mapped") {
		t.Fatalf("impact = %#v", impact)
	}
}

func TestMapIdentitySupportsMultipleCompatibleIdentities(t *testing.T) {
	cfg := testConfig()
	cfg.Identities["second"] = config.Identity{Provider: "gcp", Account: "second@example.com"}
	plan, err := PlanMapIdentity(cfg, "dev", "second")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() || !slices.Equal(plan.Config.Projects["dev"].Identities, []string{"person", "second"}) {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Action != "map" {
		t.Fatalf("changes = %#v", plan.Changes)
	}
}

func TestPlansDoNotMutateTheirInput(t *testing.T) {
	cfg := testConfig()
	cfg.Identities["second"] = config.Identity{Provider: "gcp", Account: "second@example.com"}
	plan, err := PlanMapIdentity(cfg, "dev", "second")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(cfg.Projects["dev"].Identities, "second") {
		t.Fatal("planning mutated the input configuration")
	}
	project := plan.Config.Projects["dev"]
	project.Identities[0] = "changed-in-plan"
	plan.Config.Projects["dev"] = project
	if cfg.Projects["dev"].Identities[0] != "person" {
		t.Fatal("plan and input share mutable identity slices")
	}
}

func TestGKEProjectAssociationCannotBeReassignedOrRemoved(t *testing.T) {
	cfg := testConfig()
	if _, err := PlanSetKubernetesProject(cfg, "prod-cluster", "dev"); err == nil || !strings.Contains(err.Error(), "provider-bound") {
		t.Fatalf("reassignment error = %v", err)
	}
	if _, err := PlanRemoveKubernetesProject(cfg, "prod-cluster"); err == nil || !strings.Contains(err.Error(), "cannot remove") {
		t.Fatalf("removal error = %v", err)
	}
}

func TestPlainKubernetesProjectRemapPreviewsCascadingWorkspaceFailure(t *testing.T) {
	cfg := testConfig()
	cfg.Kubernetes["plain"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/config", Context: "plain", Project: "dev"}
	cfg.Destinations["plain"] = config.Destination{Identity: "person", Project: "dev", Kubernetes: "plain"}
	plan, err := PlanSetKubernetesProject(cfg, "plain", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() {
		t.Fatal("contradictory workspace was not rejected")
	}
	impact := findImpact(t, plan, "plain")
	if !impact.Before.Valid || impact.After.Valid || !strings.Contains(strings.Join(plan.Problems, " "), "does not match") {
		t.Fatalf("impact = %#v, problems = %#v", impact, plan.Problems)
	}
	if len(plan.Changes) != 2 || plan.Changes[0].Action != "remove_mapping" || plan.Changes[1].Action != "map" {
		t.Fatalf("changes = %#v", plan.Changes)
	}
}

func TestMappingKubernetesProjectRepairsDependentSparseWorkspace(t *testing.T) {
	cfg := testConfig()
	cfg.Kubernetes["plain"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/config", Context: "plain"}
	cfg.Destinations["plain"] = config.Destination{Kubernetes: "plain"}

	plan, err := PlanSetKubernetesProject(cfg, "plain", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatalf("mapping should repair the inherited workspace dependencies: %v", plan.Problems)
	}
	impact := findImpact(t, plan, "plain")
	if impact.After.Project != "dev" || impact.After.Identity != "person" || !impact.After.Valid {
		t.Fatalf("workspace impact = %#v", impact)
	}
}

func TestWorkspaceRemapDoesNotInferRisk(t *testing.T) {
	cfg := testConfig()
	// Keep the existing identity valid for both projects so the preview isolates
	// component remapping without inferring a risk policy.
	prod := cfg.Projects["prod"]
	prod.Identities = append(prod.Identities, "person")
	cfg.Projects["prod"] = prod
	plan, err := PlanSetWorkspaceComponent(cfg, "dev", KindProject, "prod")
	if err != nil {
		t.Fatal(err)
	}
	// The workspace still points at a dev-owned cluster, so repair that mapping
	// in a second preview before expecting a valid plan.
	if plan.Valid() {
		t.Fatal("project/cluster contradiction should be previewed")
	}
	if impact := findImpact(t, plan, "dev"); impact.After.Valid {
		t.Fatalf("impact = %#v", impact)
	}

	delete(cfg.Destinations, "dev")
	cfg.Destinations["project-only"] = config.Destination{Identity: "person", Project: "dev"}
	plan, err = PlanSetWorkspaceComponent(cfg, "project-only", KindProject, "prod")
	if err != nil || !plan.Valid() {
		t.Fatalf("valid remap = %#v, %v", plan, err)
	}
	impact := findImpact(t, plan, "project-only")
	if impact.Before.Risk != "" || impact.After.Risk != "" {
		t.Fatalf("risk impact = %#v", impact)
	}
}

func TestWorkspaceMappingRejectsUnknownComponentAndEmptyWorkspace(t *testing.T) {
	cfg := testConfig()
	if _, err := PlanSetWorkspaceComponent(cfg, "local", KindDocker, "missing"); err == nil || !strings.Contains(err.Error(), "unknown docker") {
		t.Fatalf("unknown component error = %v", err)
	}
	plan, err := PlanSetWorkspaceComponent(cfg, "local", KindDocker, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Valid() || !strings.Contains(strings.Join(plan.Problems, " "), "contains no components") {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestApplyIsValidatedAtomicBackedUpAndStaleSafe(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.yaml")
	cfg := testConfig()
	delete(cfg.Destinations, "dev")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Identities["second"] = config.Identity{Provider: "gcp", Account: "second@example.com"}
	// Re-write the intended baseline so its semantic revision includes second.
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	baseline, _ := os.ReadFile(path)
	plan, err := PlanMapIdentity(cfg, "dev", "second")
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	if err := Apply(path, plan); err != nil {
		t.Fatal(err)
	}
	applied, err := config.Load(path)
	if err != nil || !slices.Contains(applied.Projects["dev"].Identities, "second") {
		t.Fatalf("applied = %#v, %v", applied, err)
	}
	backups, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) < 2 {
		t.Fatalf("backups = %#v, %v", backups, err)
	}
	foundBaseline := false
	for _, backup := range backups {
		contents, _ := os.ReadFile(backup)
		if string(contents) == string(baseline) {
			foundBaseline = true
		}
	}
	if !foundBaseline || len(original) == 0 {
		t.Fatal("recoverable pre-apply version was not retained")
	}

	stale, err := PlanRemoveIdentityMapping(applied, "dev", "second")
	if err != nil {
		t.Fatal(err)
	}
	changed := applied
	changed.Docker["other"] = config.Docker{Context: "other"}
	if err := config.Write(path, changed); err != nil {
		t.Fatal(err)
	}
	beforeStale, _ := os.ReadFile(path)
	if err := Apply(path, stale); !errors.Is(err, ErrStalePlan) {
		t.Fatalf("stale apply error = %v", err)
	}
	afterStale, _ := os.ReadFile(path)
	if string(beforeStale) != string(afterStale) {
		t.Fatal("stale apply changed configuration")
	}
}

func TestApplyRejectsInvalidPlanWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := testConfig()
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanRemoveIdentityMapping(cfg, "dev", "person")
	if err != nil || plan.Valid() {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	before, _ := os.ReadFile(path)
	if err := Apply(path, plan); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("invalid apply error = %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid apply changed configuration")
	}
}

func findImpact(t *testing.T, plan Plan, workspace string) Impact {
	t.Helper()
	for _, impact := range plan.Impacts {
		if impact.Workspace == workspace {
			return impact
		}
	}
	t.Fatalf("no impact for workspace %q in %#v", workspace, plan.Impacts)
	return Impact{}
}

func testConfig() config.Config {
	cfg := config.New()
	cfg.Identities["person"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Identities["admin"] = config.Identity{Provider: "gcp", Account: "admin@example.com"}
	cfg.Projects["dev"] = config.Project{Provider: "gcp", ProjectID: "example-dev", Identities: []string{"person"}, Risk: "development"}
	cfg.Projects["prod"] = config.Project{Provider: "gcp", ProjectID: "example-prod", Identities: []string{"admin"}, Risk: "production"}
	cfg.Kubernetes["dev-cluster"] = config.Kubernetes{Type: "gke", Project: "dev", Cluster: "dev", Location: "us-east1", Risk: "development"}
	cfg.Kubernetes["prod-cluster"] = config.Kubernetes{Type: "gke", Project: "prod", Cluster: "prod", Location: "us-east1", Risk: "production"}
	cfg.Docker["local"] = config.Docker{Context: "desktop-linux"}
	cfg.Destinations["dev"] = config.Destination{Identity: "person", Project: "dev", Kubernetes: "dev-cluster"}
	cfg.Destinations["local"] = config.Destination{Docker: "local"}
	return cfg
}
