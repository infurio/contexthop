package catalog

import (
	"bytes"
	"github.com/infurio/contexthop/internal/config"
	"slices"
	"testing"
)

func TestSharedTagsReuseRecolourAndRemove(t *testing.T) {
	cfg := config.New()
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p"}
	cfg.Docker["d"] = config.Docker{Context: "local"}
	first, err := PlanTag(cfg, Ref{KindProject, "p"}, "personal", "#a78bfa", "assign")
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanTag(first.Config, Ref{KindDocker, "d"}, "personal", "", "assign")
	if err != nil {
		t.Fatal(err)
	}
	third, err := PlanTag(second.Config, Ref{KindProject, "p"}, "personal", "#60a5fa", "color")
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Config.Tags) != 1 || third.Config.Tags["personal"].Color != "#60a5fa" || third.Config.Docker["d"].Tags[0] != "personal" {
		t.Fatal(third.Config)
	}
	if first.Config.Tags["personal"].Color != "#a78bfa" || len(cfg.Tags) != 0 {
		t.Fatal("mutated earlier snapshot")
	}
	removed, err := PlanTag(third.Config, Ref{KindProject, "p"}, "personal", "", "remove")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Config.Projects["p"].TagNames()) != 0 || len(removed.Config.Docker["d"].TagNames()) != 1 {
		t.Fatal("removed tag from wrong scope")
	}
	var buf bytes.Buffer
	if err := config.Encode(&buf, removed.Config); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projects["p"].TagNames()) != 0 {
		t.Fatal("empty assignments did not survive save")
	}
}
func TestExplicitTagsStayRemovedAfterDiscovery(t *testing.T) {
	cfg := config.New()
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", LabelSet: config.LabelSet{GoogleLabels: map[string]string{"environment": "personal", "team": "personal"}}}
	assigned, err := PlanTag(cfg, Ref{KindProject, "p"}, "personal", "#a78bfa", "assign")
	if err != nil {
		t.Fatal(err)
	}
	cfg = assigned.Config
	plan, err := PlanTag(cfg, Ref{KindProject, "p"}, "personal", "", "remove")
	if err != nil {
		t.Fatal(err)
	}
	observed := plan.Config.Clone()
	p := observed.Projects["p"]
	p.GoogleLabels["environment"] = "production"
	observed.Projects["p"] = p
	refreshed := PlanSaveDiscovery(plan.Config, observed)
	if len(refreshed.Config.Projects["p"].TagNames()) != 0 {
		t.Fatal("discovery overwrote explicit assignment")
	}
}
func TestTagRejectsInvalidColour(t *testing.T) {
	cfg := config.New()
	cfg.Docker["d"] = config.Docker{Context: "local"}
	if _, err := PlanTag(cfg, Ref{KindDocker, "d"}, "personal", "bad", "assign"); err == nil {
		t.Fatal("invalid colour accepted")
	}
}

func TestDiscoveryDoesNotCreateTagsFromGoogleMetadata(t *testing.T) {
	saved := config.New()
	observed := config.New()
	observed.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", LabelSet: config.LabelSet{GoogleLabels: map[string]string{"firebase": "enabled"}}}
	observed.Kubernetes["k"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "~/.kube/config", Context: "k", LabelSet: config.LabelSet{GoogleLabels: map[string]string{"goog-terraform-provisioned": "true"}}}
	plan := PlanSaveDiscovery(saved, observed)
	if !plan.Valid() {
		t.Fatal(plan.Problems)
	}
	if len(plan.Config.Tags) != 0 || len(plan.Config.Projects["p"].TagNames()) != 0 || len(plan.Config.Kubernetes["k"].TagNames()) != 0 {
		t.Fatal("discovery generated tags")
	}
	if plan.Config.Projects["p"].GoogleLabels["firebase"] != "enabled" {
		t.Fatal("Google metadata lost")
	}
}

func TestDeleteSharedTagRemovesAllAssignmentsAndSurvivesReload(t *testing.T) {
	cfg := config.New()
	cfg.Tags["obsolete"] = config.Tag{Color: "#123456"}
	cfg.Tags["keep"] = config.Tag{Color: "#abcdef"}
	cfg.Tags["unused"] = config.Tag{Color: "#987654"}
	labels := config.LabelSet{Tags: []string{"obsolete", "keep"}, Labels: map[string]string{"team": "fictional"}}
	cfg.Identities["i"] = config.Identity{Provider: "gcp", Account: "person@example.invalid", LabelSet: labels}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", LabelSet: labels}
	cfg.Kubernetes["k"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/fictional/config", Context: "local", LabelSet: labels}
	cfg.Docker["d"] = config.Docker{Context: "local", LabelSet: labels}
	cfg.Destinations["w"] = config.Destination{Docker: "d", LabelSet: labels}
	plan, err := PlanDeleteTag(cfg, "obsolete")
	if err != nil || !plan.Valid() {
		t.Fatal(err, plan.Problems)
	}
	if len(plan.Changes) != 6 {
		t.Fatal("review must include every affected entity", plan.Changes)
	}
	var buf bytes.Buffer
	if err := config.Encode(&buf, plan.Config); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []Ref{{KindIdentity, "i"}, {KindProject, "p"}, {KindKubernetes, "k"}, {KindDocker, "d"}, {KindWorkspace, "w"}} {
		labels, _ := EntityLabels(loaded, ref)
		if len(labels.Tags) != 1 || labels.Tags[0] != "keep" || labels.Labels["team"] != "fictional" {
			t.Fatal("wrong remaining metadata", ref, labels)
		}
	}
	if _, ok := loaded.Tags["obsolete"]; ok {
		t.Fatal("deleted definition resurrected")
	}
	if len(cfg.Docker["d"].Tags) != 2 || len(cfg.Tags) != 3 {
		t.Fatal("source snapshot mutated")
	}
	unused, err := PlanDeleteTag(loaded, "unused")
	if err != nil || len(unused.Changes) != 1 {
		t.Fatal("unused tag not deletable")
	}
	if _, err := PlanDeleteTag(loaded, "missing"); err == nil {
		t.Fatal("unknown tag accepted")
	}
}

func TestRenameTagPreservesAssignmentsColourAndOtherMetadata(t *testing.T) {
	cfg := config.New()
	cfg.Tags["old"] = config.Tag{Color: "#123456"}
	cfg.Tags["keep"] = config.Tag{Color: "#abcdef"}
	labels := config.LabelSet{Tags: []string{"old", "keep"}, Labels: map[string]string{"team": "fictional"}}
	cfg.Identities["i"] = config.Identity{Provider: "gcp", Account: "person@example.com", LabelSet: labels}
	cfg.Projects["p"] = config.Project{Provider: "gcp", ProjectID: "p", LabelSet: labels}
	cfg.Kubernetes["k"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/fictional/config", Context: "local", LabelSet: labels}
	cfg.Docker["d"] = config.Docker{Context: "local", LabelSet: labels}
	cfg.Destinations["w"] = config.Destination{Docker: "d", LabelSet: labels}
	plan, err := PlanRenameTag(cfg, "old", "renamed")
	if err != nil || !plan.Valid() {
		t.Fatal(err, plan.Problems)
	}
	var buf bytes.Buffer
	if err := config.Encode(&buf, plan.Config); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []Ref{{KindIdentity, "i"}, {KindProject, "p"}, {KindKubernetes, "k"}, {KindDocker, "d"}, {KindWorkspace, "w"}} {
		labels, _ := EntityLabels(saved, ref)
		if len(labels.Tags) != 2 || !slices.Contains(labels.Tags, "renamed") || !slices.Contains(labels.Tags, "keep") || labels.Labels["team"] != "fictional" {
			t.Fatal(ref, labels)
		}
	}
	if saved.Tags["renamed"].Color != "#123456" || len(saved.Tags) != 2 {
		t.Fatal("colour/definitions changed", saved.Tags)
	}
	if _, ok := saved.Tags["old"]; ok {
		t.Fatal("old definition remains")
	}
	if cfg.Docker["d"].Tags[0] != "old" || cfg.Tags["old"].Color != "#123456" {
		t.Fatal("input mutated")
	}
	for _, name := range []string{"", "keep", "old", "bad\nname"} {
		if _, err := PlanRenameTag(cfg, "old", name); err == nil {
			t.Fatal("invalid rename accepted", name)
		}
	}
	if _, err := PlanRenameTag(cfg, "missing", "new"); err == nil {
		t.Fatal("unknown tag accepted")
	}
	cfg.Tags["unused"] = config.Tag{Color: "#abcdef"}
	if p, err := PlanRenameTag(cfg, "unused", "available"); err != nil || len(p.Changes) != 1 {
		t.Fatal("unused tag rename failed", err)
	}
}
