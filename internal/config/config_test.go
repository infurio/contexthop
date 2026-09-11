package config

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecodeValidConfiguration(t *testing.T) {
	input := `
version: 1
identities:
  work:
    provider: gcp
    account: person@example.com
projects:
  development:
    provider: gcp
    projectId: example-development
    identities: [work]
kubernetes:
  dev-gke:
    type: gke
    project: development
    cluster: dev
    location: us-east1
workspaces:
  dev:
    identity: work
    project: development
    kubernetes: dev-gke
`
	cfg, err := Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if cfg.Destinations["dev"].Kubernetes != "dev-gke" {
		t.Fatalf("unexpected destination: %#v", cfg.Destinations["dev"])
	}
}

func TestEncodeUsesWorkspaceTerminology(t *testing.T) {
	cfg := New()
	cfg.Docker["local"] = Docker{Context: "desktop-linux"}
	cfg.Destinations["local"] = Destination{Docker: "local"}
	var output bytes.Buffer
	if err := Encode(&output, cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "workspaces:") || strings.Contains(output.String(), "\ndestinations:") {
		t.Fatalf("encoded config = %q", output.String())
	}
}

func TestDecodeRejectsIncompatibleDestination(t *testing.T) {
	input := `
version: 1
identities:
  first:
    provider: gcp
    account: first@example.com
  second:
    provider: gcp
    account: second@example.com
projects:
  project:
    provider: gcp
    projectId: example-project
    identities: [first]
destinations:
  invalid:
    identity: second
    project: project
`
	_, err := Decode(strings.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "is not mapped") {
		t.Fatalf("Decode() error = %v, want identity mapping error", err)
	}
}

func TestWorkspaceMayInheritProjectFromKubernetes(t *testing.T) {
	cfg := New()
	cfg.Identities["work"] = Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Projects["project"] = Project{Provider: "gcp", ProjectID: "example-project", Identities: []string{"work"}}
	cfg.Kubernetes["cluster"] = Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1"}
	cfg.Destinations["work"] = Destination{Kubernetes: "cluster"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("workspace should inherit its Kubernetes project's dependencies: %v", err)
	}

	workspace := cfg.Destinations["work"]
	workspace.Project = "different"
	cfg.Projects["different"] = Project{Provider: "gcp", ProjectID: "different", Identities: []string{"work"}}
	cfg.Destinations["work"] = workspace
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("explicitly contradictory project should be rejected: %v", err)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	_, err := Decode(strings.NewReader("version: 1\nunknown: true\n"))
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("Decode() error = %v, want unknown field error", err)
	}
}

func TestWorkspaceADCIsExplicitAndDefaultsToNone(t *testing.T) {
	input := `
version: 1
identities:
  work:
    provider: gcp
    account: person@example.com
projects:
  project:
    provider: gcp
    projectId: example-project
    identities: [work]
workspaces:
  cli-only:
    identity: work
    project: project
  terraform:
    identity: work
    project: project
    adc: identity
`
	cfg, err := Decode(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Destinations["cli-only"].ADC != "" || cfg.Destinations["terraform"].ADC != "identity" {
		t.Fatalf("unexpected ADC modes: %#v", cfg.Destinations)
	}
}

func TestWorkspaceRejectsUnknownADCMode(t *testing.T) {
	cfg := New()
	cfg.Identities["work"] = Identity{Provider: "gcp", Account: "person@example.com"}
	cfg.Destinations["work"] = Destination{Identity: "work", ADC: "automatic"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported ADC mode") {
		t.Fatalf("error = %v", err)
	}
}

func TestSupportedRiskCategoriesIncludeSandbox(t *testing.T) {
	for _, risk := range []string{"sandbox", "development", "production"} {
		cfg := New()
		cfg.Docker["target"] = Docker{Context: "desktop-linux", Risk: risk}
		cfg.Destinations["workspace"] = Destination{Docker: "target", Risk: risk}
		if err := cfg.Validate(); err != nil {
			t.Errorf("risk %q should be valid: %v", risk, err)
		}
	}
	cfg := New()
	cfg.Docker["target"] = Docker{Context: "desktop-linux", Risk: "experimental"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported risk") {
		t.Fatalf("unsupported risk error = %v", err)
	}
}

func TestProvenanceMetadataIsBackwardCompatibleAndValidated(t *testing.T) {
	cfg, err := Decode(strings.NewReader("version: 1\nidentities:\n  old:\n    provider: gcp\n    account: old@example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Identities["old"].Provenance != "" {
		t.Fatalf("legacy provenance = %q", cfg.Identities["old"].Provenance)
	}

	identity := cfg.Identities["old"]
	identity.Provenance = "manual"
	identity.VerifiedBy = "gcp"
	identity.ObservedAt = "2026-09-03T12:30:00Z"
	cfg.Identities["old"] = identity
	var output bytes.Buffer
	if err := Encode(&output, cfg); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"provenance: manual", "verifiedBy: gcp", "observedAt: \"2026-09-03T12:30:00Z\""} {
		if !strings.Contains(output.String(), field) {
			t.Fatalf("encoded metadata missing %q: %s", field, output.String())
		}
	}

	identity.ObservedAt = "yesterday"
	cfg.Identities["old"] = identity
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "invalid observedAt") {
		t.Fatalf("invalid timestamp error = %v", err)
	}

	identity.ObservedAt = ""
	cfg.Identities["old"] = identity
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "without observedAt") {
		t.Fatalf("missing observation error = %v", err)
	}

	identity.VerifiedBy = ""
	identity.ObservedAt = "2026-09-03T12:30:00Z"
	cfg.Identities["old"] = identity
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "without a verifier") {
		t.Fatalf("missing verifier error = %v", err)
	}
}

func TestHiddenVisibilityIsBackwardCompatibleAndPersisted(t *testing.T) {
	cfg, err := Decode(strings.NewReader("version: 1\nidentities:\n  visible:\n    provider: gcp\n    account: visible@example.com\n  hidden:\n    provider: gcp\n    account: hidden@example.com\n    hidden: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Identities["visible"].Hidden || !cfg.Identities["hidden"].Hidden {
		t.Fatalf("decoded visibility = %#v", cfg.Identities)
	}
	var output bytes.Buffer
	if err := Encode(&output, cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "hidden: true") {
		t.Fatalf("encoded visibility missing: %s", output.String())
	}
}

func TestRelationshipPreferencesAndKubernetesAliasesValidate(t *testing.T) {
	cfg := New()
	cfg.Identities["first"] = Identity{Provider: "gcp", Kind: "user", Account: "first@example.com"}
	cfg.Identities["second"] = Identity{Provider: "gcp", Kind: "user", Account: "second@example.com"}
	cfg.Projects["project"] = Project{
		Provider: "gcp", ProjectID: "example", Identities: []string{"first", "second"}, PreferredIdentity: "first",
	}
	cfg.Kubernetes["cluster"] = Kubernetes{
		Type: "gke", Project: "project", Cluster: "cluster", Location: "us-east1",
		ProviderID: "gke:example/us-east1/cluster", AccessID: "sha256:access", PreferredIdentity: "second",
	}
	cfg.KubernetesAliases["cluster"] = []KubernetesAlias{
		{Context: "Friendly", Kubeconfig: "/config"},
		{Context: "gke_example_us-east1_cluster", Kubeconfig: "/config"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	target := cfg.Kubernetes["cluster"]
	target.Endpoint = "dns"
	cfg.Kubernetes["cluster"] = target
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DNS endpoint was rejected: %v", err)
	}
	target.Endpoint = "private-service-connect"
	cfg.Kubernetes["cluster"] = target
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported endpoint") {
		t.Fatalf("invalid endpoint error = %v", err)
	}
	target.Endpoint = ""
	cfg.Kubernetes["cluster"] = target

	project := cfg.Projects["project"]
	project.PreferredIdentity = "missing"
	cfg.Projects["project"] = project
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "prefers identity") {
		t.Fatalf("invalid preference error = %v", err)
	}
}
