package selection

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func selectionFixture() config.Config {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Projects["project"] = config.Project{Provider: "gcp", ProjectID: "project-id", Identities: []string{"work"}}
	cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-central1"}
	cfg.Docker["local"] = config.Docker{Context: "local"}
	cfg.Destinations["work"] = config.Destination{Identity: "work", Project: "project", Kubernetes: "cluster", Docker: "local", ADC: "identity"}
	cfg.Destinations["local"] = config.Destination{Docker: "local"}
	return cfg
}

func TestLaunchAndSaveShareADCPolicy(t *testing.T) {
	cfg := selectionFixture()
	before, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{
		{Identity: "work", Project: "project", Kubernetes: "cluster", Docker: "local", Workspace: "work", Source: "work"},
		{Identity: "work", Docker: "local", Source: "work"},
		{Identity: "work", Docker: "local"},
		{Docker: "local", Source: "work"},
		{Docker: "local", Workspace: "local", Source: "local"},
		{Project: "project", Source: "work"},
	} {
		for _, override := range []ADCChoice{ADCDefault, ADCOff, ADCIdentity} {
			request.ADCOverride = override
			launch, err := request.Resolve(cfg)
			if err != nil {
				t.Fatal(request, err)
			}
			saved, err := request.ResolveForSave(cfg, cfg)
			if err != nil {
				t.Fatal(request, err)
			}
			want := ""
			if launch.IdentityName != "" && (override == ADCIdentity || override == ADCDefault && request.Source == "work") {
				want = "identity"
			}
			if launch.ADCMode != want || saved.ADCMode != want {
				t.Fatal("launch/save settings differ", request, launch.ADCMode, saved.ADCMode, want)
			}
		}
	}
	after, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("resolution mutated the catalog")
	}
}

func TestResolutionRejectsInvalidOverridesAndAmbiguousIdentity(t *testing.T) {
	cfg := selectionFixture()
	for _, request := range []Request{{Workspace: "work", ADCOverride: "invalid"}, {Docker: "local", ADCOverride: "invalid"}, {Workspace: "missing"}} {
		if _, err := request.Resolve(cfg); err == nil {
			t.Fatal("invalid request accepted", request)
		}
	}
	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	project := cfg.Projects["project"]
	project.Identities = append(project.Identities, "other")
	cfg.Projects["project"] = project
	if _, err := (Request{Project: "project"}).Resolve(cfg); err == nil {
		t.Fatal("ambiguous context launched without choosing an identity")
	}
}

func TestStagingDependencyAndEscapeContract(t *testing.T) {
	initial := Request{Identity: "work", Project: "project", Kubernetes: "cluster", Docker: "local", Workspace: "work", Source: "work", ADCOverride: ADCIdentity}
	for _, kind := range []Resource{Identity, Project, Kubernetes, Docker, Workspace} {
		staged := initial
		staged.Unstage(kind)
		if staged.Resource(kind) != "" || staged.Workspace != "" || staged.Source != "" || staged.ADCOverride != ADCDefault {
			t.Fatal("explicit unstage retained selection metadata", kind, staged)
		}
		if kind == Identity && (staged.Project != "" || staged.Kubernetes != "") || kind == Project && staged.Kubernetes != "" {
			t.Fatal("unstage retained dependent resources", kind, staged)
		}
		if kind != Docker && kind != Workspace && staged.Docker != initial.Docker {
			t.Fatal("cloud change removed independent Docker selection", kind)
		}
	}
	staged := initial
	for index, kind := range []Resource{Kubernetes, Project, Identity, Docker} {
		if !staged.UnstageNext(true) || staged.Resource(kind) != "" {
			t.Fatal("Escape removed wrong component", kind, staged)
		}
		if index < 3 && staged.Source != "work" {
			t.Fatal("Escape lost workspace provenance too early", kind)
		}
	}
	if staged.HasResources() || staged.Source != "" || staged.UnstageNext(true) {
		t.Fatal("last Escape did not finish clearing", staged)
	}
}
