package config

import (
	"strings"
	"testing"
)

func TestLabelsNeverBecomeTags(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`version: 1
projects:
  p:
    provider: gcp
    projectId: p
    googleLabels:
      firebase: enabled
      environment: production
    labels:
      team: payments
kubernetes:
  k:
    type: kubeconfig
    context: k
    kubeconfig: ~/.kube/config
    googleLabels:
      goog-terraform-provisioned: "true"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tags) != 0 || len(cfg.Projects["p"].TagNames()) != 0 || len(cfg.Kubernetes["k"].TagNames()) != 0 {
		t.Fatal("metadata generated tags")
	}
	if cfg.Projects["p"].GoogleLabels["firebase"] != "enabled" || cfg.Kubernetes["k"].GoogleLabels["goog-terraform-provisioned"] != "true" {
		t.Fatal("metadata lost")
	}
}
func TestChosenTagsSurviveNormalization(t *testing.T) {
	cfg := New()
	cfg.Tags["enabled"] = Tag{Color: "#a78bfa"}
	cfg.Projects["p"] = Project{LabelSet: LabelSet{Tags: []string{"enabled"}, GoogleLabels: map[string]string{"firebase": "enabled"}}}
	cfg.MigrateTags()
	if cfg.Tags["enabled"].Color != "#a78bfa" || len(cfg.Projects["p"].TagNames()) != 1 {
		t.Fatal("chosen tag was changed")
	}
}
