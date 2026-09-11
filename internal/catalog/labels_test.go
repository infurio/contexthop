package catalog

import (
	"github.com/infurio/contexthop/internal/config"
	"strings"
	"testing"
)

func TestLabelsRefreshOverrideAndReset(t *testing.T) {
	cfg := config.New()
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", LabelSet: config.LabelSet{GoogleLabels: map[string]string{"environment": "dev"}, Labels: map[string]string{"environment": "custom"}}}
	observed := cfg.Clone()
	p := observed.Projects["p"]
	p.GoogleLabels["environment"] = "production"
	p.GoogleLabels["team"] = "payments"
	observed.Projects["p"] = p
	refreshed := PlanSaveDiscovery(cfg, observed).Config
	labels, _ := EntityLabels(refreshed, Ref{KindProject, "p"})
	if labels.Effective("")["environment"] != "custom" || labels.GoogleLabels["environment"] != "production" {
		t.Fatal(labels)
	}
	delete(labels.Labels, "environment")
	plan, err := PlanLabels(refreshed, Ref{KindProject, "p"}, labels)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Config.Projects["p"].Effective("")["environment"] != "production" {
		t.Fatal("reset failed")
	}
	if cfg.Projects["p"].GoogleLabels["environment"] != "dev" || refreshed.Projects["p"].Labels["environment"] != "custom" {
		t.Fatal("snapshot mutated")
	}
}
func TestLegacyRiskMigratesToLabel(t *testing.T) {
	cfg, err := config.Decode(strings.NewReader("version: 1\nprojects:\n  p:\n    provider: gcp\n    projectId: p\n    risk: production\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Projects["p"].Risk != "" || cfg.Projects["p"].Labels["risk"] != "production" {
		t.Fatal(cfg.Projects["p"])
	}
}

func TestIdentityLabelEditorOwnsSnapshot(t *testing.T) {
	cfg := config.New()
	cfg.Identities["i"] = config.Identity{Provider: "gcp", Account: "me@example.com", LabelSet: config.LabelSet{Labels: map[string]string{"team": "old"}}}
	labels, _ := EntityLabels(cfg, Ref{KindIdentity, "i"})
	labels.Labels["team"] = "new"
	if cfg.Identities["i"].Labels["team"] != "old" {
		t.Fatal("editor mutated identity")
	}
}
