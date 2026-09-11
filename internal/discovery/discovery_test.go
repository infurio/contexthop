package discovery

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func TestParseGKEContext(t *testing.T) {
	project, location, cluster, ok := parseGKEContext("gke_example-project_us-east1_main-cluster")
	if !ok || project != "example-project" || location != "us-east1" || cluster != "main-cluster" {
		t.Fatalf("parseGKEContext() = %q, %q, %q, %v", project, location, cluster, ok)
	}
}

func TestSlug(t *testing.T) {
	if got := slug("GKE_Project/Production"); got != "gke-project-production" {
		t.Fatalf("slug() = %q", got)
	}
}

func TestServiceAccountDetection(t *testing.T) {
	for _, account := range []string{
		"worker@example-project.iam.gserviceaccount.com",
		"123456@developer.gserviceaccount.com",
	} {
		if !isServiceAccount(account) {
			t.Fatalf("isServiceAccount(%q) = false", account)
		}
	}
	if isServiceAccount("person@example.com") {
		t.Fatal("regular Google identity detected as service account")
	}
}

func TestInferProductionRisk(t *testing.T) {
	for _, value := range []string{"EXAMPLE-Prod", "production-gke", "prod-example-sites"} {
		if got := inferRisk(value); got != "production" {
			t.Errorf("inferRisk(%q) = %q", value, got)
		}
	}
	if got := inferRisk("development"); got != "" {
		t.Errorf("inferRisk(development) = %q", got)
	}
	if got := inferRisk("team-sandbox"); got != "sandbox" {
		t.Errorf("inferRisk(team-sandbox) = %q", got)
	}
}

func TestGKEMetadataForFriendlyContextUsesUnderlyingCluster(t *testing.T) {
	var view kubeConfigView
	if err := json.Unmarshal([]byte(`{
		"contexts": [{
			"name": "EXAMPLE-Prod",
			"context": {"cluster": "gke_example-prod-1234_us-central1_acme-fixture-2"}
		}]
	}`), &view); err != nil {
		t.Fatal(err)
	}
	project, location, cluster, ok := gkeMetadataForContext(view, "EXAMPLE-Prod")
	if !ok || project != "example-prod-1234" || location != "us-central1" || cluster != "acme-fixture-2" {
		t.Fatalf("metadata = %q, %q, %q, %v", project, location, cluster, ok)
	}
}

func TestKubeContextSignatureUsesDefinitionsRatherThanLocalNames(t *testing.T) {
	decode := func(input string) kubeConfigView {
		var view kubeConfigView
		if err := json.Unmarshal([]byte(input), &view); err != nil {
			t.Fatal(err)
		}
		return view
	}
	first := decode(`{
		"contexts":[{"name":"Friendly","context":{"cluster":"friendly-cluster","user":"friendly-user","namespace":"payments"}}],
		"clusters":[{"name":"friendly-cluster","cluster":{"server":"https://example","certificate-authority-data":"ca"}}],
		"users":[{"name":"friendly-user","user":{"exec":{"command":"plugin"}}}]
	}`)
	second := decode(`{
		"contexts":[{"name":"Generated","context":{"cluster":"generated-cluster","user":"generated-user","namespace":"payments"}}],
		"clusters":[{"name":"generated-cluster","cluster":{"server":"https://example","certificate-authority-data":"ca"}}],
		"users":[{"name":"generated-user","user":{"exec":{"command":"plugin"}}}]
	}`)
	if left, right := kubeContextSignature(first, "Friendly"), kubeContextSignature(second, "Generated"); left != right {
		t.Fatalf("equivalent definitions differ: %q != %q", left, right)
	}
	second.Contexts[0].Context.Namespace = "another-namespace"
	if left, right := kubeContextSignature(first, "Friendly"), kubeContextSignature(second, "Generated"); left != right {
		t.Fatalf("namespace changed access identity: %q != %q", left, right)
	}
}

func TestImportKubernetesSourcesPreservesConflictingProvenance(t *testing.T) {
	decode := func(input string) kubeConfigView {
		var view kubeConfigView
		if err := json.Unmarshal([]byte(input), &view); err != nil {
			t.Fatal(err)
		}
		return view
	}
	sources := []kubeSourceView{
		{Path: "/configs/team-a.yaml", View: decode(`{
			"contexts":[{"name":"production","context":{"cluster":"shared","user":"person","namespace":"payments"}}],
			"clusters":[{"name":"shared","cluster":{"server":"https://one.example"}}]
		}`)},
		{Path: "/configs/team-b.yaml", View: decode(`{
			"contexts":[{"name":"production","context":{"cluster":"shared","user":"person"}}],
			"clusters":[{"name":"shared","cluster":{"server":"https://two.example"}}]
		}`)},
	}
	cfg := config.New()
	warnings := importKubernetesSources(&cfg, map[string]string{}, sources)
	if len(warnings) != 1 || len(cfg.Kubernetes) != 2 {
		t.Fatalf("warnings = %#v, targets = %#v", warnings, cfg.Kubernetes)
	}
	paths := map[string]bool{}
	for _, target := range cfg.Kubernetes {
		paths[target.Kubeconfig] = true
		if target.Provenance != "kubeconfig" || target.VerifiedBy != "kubeconfig" || target.ObservedAt == "" {
			t.Fatalf("target provenance = %#v", target)
		}
	}
	if !paths["/configs/team-a.yaml"] || !paths["/configs/team-b.yaml"] {
		t.Fatalf("source provenance was not preserved: %#v", paths)
	}
	foundNamespace := false
	for _, target := range cfg.Kubernetes {
		if target.Kubeconfig == "/configs/team-a.yaml" && target.Namespace == "payments" && target.Cluster == "shared" {
			foundNamespace = true
		}
	}
	if !foundNamespace {
		t.Fatal("context namespace and underlying cluster reference were not imported")
	}
}

func TestEnrichKubernetesDependenciesRetainsPhysicalClusterNames(t *testing.T) {
	bin := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	logPath := filepath.Join(t.TempDir(), "kubectl.log")
	t.Setenv("PATH", bin)
	t.Setenv("KUBECTL_LOG", logPath)
	kubectl := filepath.Join(bin, "kubectl")
	script := `#!/bin/sh
printf 'view\n' >> "$KUBECTL_LOG"
printf '%s\n' '{"contexts":[
  {"name":"Friendly Local","context":{"cluster":"local-api","user":"person"}},
  {"name":"Friendly GKE","context":{"cluster":"gke_example-project_us-east1_provider-cluster","user":"person"}}
]}'
`
	if err := os.WriteFile(kubectl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.Projects["work"] = config.Project{Provider: "gcp", ProjectID: "example-project"}
	cfg.Kubernetes["local"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: configPath, Context: "Friendly Local"}
	cfg.Kubernetes["gke"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: configPath, Context: "Friendly GKE"}
	if err := EnrichKubernetesDependencies(context.Background(), &cfg, ""); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Kubernetes["local"].Cluster; got != "local-api" {
		t.Fatalf("plain physical cluster = %q", got)
	}
	gke := cfg.Kubernetes["gke"]
	if gke.Type != "gke" || gke.Cluster != "provider-cluster" || gke.Location != "us-east1" || gke.Project != "work" {
		t.Fatalf("GKE target = %#v", gke)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if calls := strings.Count(string(log), "view\n"); calls != 1 {
		t.Fatalf("shared kubeconfig loaded %d times, want once", calls)
	}
}

func TestEnrichDoesNotReplaceManagedPhysicalClusterWithLocalReference(t *testing.T) {
	bin := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(`#!/bin/sh
printf '%s\n' '{"contexts":[{"name":"Friendly","context":{"cluster":"kubeconfig-local-name","user":"person"}}]}'
`), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.Kubernetes["managed"] = config.Kubernetes{
		Type: "gke", Project: "work", Cluster: "provider-cluster", Location: "us-east1",
		Kubeconfig: configPath, Context: "Friendly",
	}
	if err := EnrichKubernetesDependencies(context.Background(), &cfg, ""); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Kubernetes["managed"].Cluster; got != "provider-cluster" {
		t.Fatalf("managed physical cluster = %q", got)
	}
}

func TestKubernetesSourceRevisionDetectsChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	target := config.Kubernetes{Type: "kubeconfig", Kubeconfig: path, Context: "local"}
	cfg.Kubernetes["local"] = target
	revision := CaptureKubernetesSourceRevisions(cfg)["local"]
	if revision.Err != nil {
		t.Fatal(revision.Err)
	}
	if err := ValidateKubernetesSourceRevision(target, revision); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\ncontexts: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateKubernetesSourceRevision(target, revision); err == nil || !strings.Contains(err.Error(), "changed while") {
		t.Fatalf("revision error = %v", err)
	}
}

func TestKubernetesSourceRevisionTracksMissingOptionalSource(t *testing.T) {
	directory := t.TempDir()
	primary := filepath.Join(directory, "primary.yaml")
	missing := filepath.Join(directory, "optional.yaml")
	if err := os.WriteFile(primary, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := config.Kubernetes{
		Type: "kubeconfig", Kubeconfig: strings.Join([]string{primary, missing}, string(os.PathListSeparator)), Context: "local",
	}
	revision, err := KubernetesSourceRevision(target)
	if err != nil {
		t.Fatalf("missing optional source rejected: %v", err)
	}
	if revision == "" {
		t.Fatal("revision is empty")
	}
	if err := ValidateKubernetesSourceRevision(target, SourceRevision{Value: revision}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(missing, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateKubernetesSourceRevision(target, SourceRevision{Value: revision}); err == nil || !strings.Contains(err.Error(), "changed while") {
		t.Fatalf("new optional source was not detected: %v", err)
	}
}

func TestResolveKubernetesSourceRepairsOldMergedImport(t *testing.T) {
	directory := t.TempDir()
	primary := filepath.Join(directory, "primary.yaml")
	missing := filepath.Join(directory, "deleted.yaml")
	if err := os.WriteFile(primary, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	kubectl := filepath.Join(bin, "kubectl")
	script := `#!/bin/sh
case "$KUBECONFIG" in
  *primary.yaml) printf '%s\n' '{"contexts":[{"name":"acme-sandbox","context":{"cluster":"office","user":"person"}}],"clusters":[{"name":"office","cluster":{"server":"https://office.example"}}]}' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(kubectl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	target := config.Kubernetes{
		Type:       "kubeconfig",
		Kubeconfig: strings.Join([]string{primary, missing}, string(os.PathListSeparator)),
		Context:    "acme-sandbox",
	}
	resolved, err := resolveKubernetesSource(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source.Path != primary {
		t.Fatalf("source = %q", resolved.Source.Path)
	}
}

func TestResolveKubernetesSourceMissingSourceOffersWorkingRecovery(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "deleted.yaml")
	target := config.Kubernetes{
		Type:       "kubeconfig",
		Kubeconfig: missing,
		Context:    "deleted-context",
	}

	_, err := resolveKubernetesSource(context.Background(), target)
	if err == nil {
		t.Fatal("expected missing source error")
	}
	message := err.Error()
	if !strings.Contains(message, missing) {
		t.Fatalf("error does not identify missing source: %v", err)
	}
	if !strings.Contains(message, "restore the missing source") || !strings.Contains(message, "chop catalog edit") {
		t.Fatalf("error does not offer a working recovery: %v", err)
	}
	if strings.Contains(message, "chop init --write") {
		t.Fatalf("error recommends unsupported reconciliation: %v", err)
	}
}

func TestSelectKubernetesSourceStillRejectsConflicts(t *testing.T) {
	decode := func(input string) kubeConfigView {
		var view kubeConfigView
		if err := json.Unmarshal([]byte(input), &view); err != nil {
			t.Fatal(err)
		}
		return view
	}
	sources := []kubeSourceView{
		{Path: "/one.yaml", View: decode(`{"contexts":[{"name":"shared","context":{"cluster":"one"}}],"clusters":[{"name":"one","cluster":{"server":"https://one.example"}}]}`)},
		{Path: "/two.yaml", View: decode(`{"contexts":[{"name":"shared","context":{"cluster":"two"}}],"clusters":[{"name":"two","cluster":{"server":"https://two.example"}}]}`)},
	}
	_, err := selectKubernetesSource("shared", sources)
	if err == nil || !strings.Contains(err.Error(), "conflicting definitions") {
		t.Fatalf("error = %v", err)
	}
}
