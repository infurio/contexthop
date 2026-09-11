package appstate

import (
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func fixture() config.Config {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	cfg.Projects["saved"] = config.Project{Provider: "gcp", ProjectID: "saved", Identities: []string{"other"}}
	cfg.Destinations["workspace"] = config.Destination{Project: "saved", Identity: "other"}
	return cfg
}

func TestDiscoveryIsIndependentOfSavedStateAndSnapshots(t *testing.T) {
	s := New(fixture())
	view := s.Selection()
	project := view.Projects["saved"]
	project.Identities = append(project.Identities, "work")
	view.Projects["saved"] = project
	view.Projects["new"] = config.Project{Provider: "gcp", ProjectID: "new", Identities: []string{"work"}}
	s.Observe(view)
	view.Projects["saved"].Identities[0] = "mutated"
	if len(s.Saved().Projects) != 1 || len(s.Saved().Projects["saved"].Identities) != 1 {
		t.Fatal("discovery changed saved state")
	}
	if got := s.Selection().Projects["saved"].Identities; len(got) != 2 || got[0] != "other" {
		t.Fatalf("snapshot aliases caller: %v", got)
	}
	if len(s.Clone().Selection().Destinations) != 1 {
		t.Fatal("snapshot lost workspaces")
	}
	copy := s.Clone()
	copy.SavedChange(config.New())
	if len(s.Selection().Projects) != 2 {
		t.Fatal("clone changed original state")
	}
}

func TestSaveOneProjectPreservesOtherObservations(t *testing.T) {
	s := New(fixture())
	view := s.Selection()
	for _, name := range []string{"one", "two"} {
		view.Projects[name] = config.Project{Provider: "gcp", ProjectID: name, Identities: []string{"work"}}
	}
	s.Observe(view)
	saved := s.Saved()
	saved.Projects["one"] = view.Projects["one"]
	s.SavedChange(saved)
	if len(s.Saved().Projects) != 2 || len(s.Selection().Projects) != 3 {
		t.Fatal("saving one lost other discoveries or persisted them")
	}
}

func TestRemovalIsNotUndoneByOldDiscovery(t *testing.T) {
	s := New(fixture())
	view := s.Selection()
	project := view.Projects["saved"]
	project.Identities = append(project.Identities, "work")
	view.Projects["saved"] = project
	s.Observe(view)
	saved := s.Saved()
	project = saved.Projects["saved"]
	project.Identities = nil
	saved.Projects["saved"] = project
	s.SavedChange(saved)
	if got := s.Selection().Projects["saved"].Identities; len(got) != 1 || got[0] != "work" {
		t.Fatalf("removed mapping resurrected: %v", got)
	}
	delete(saved.Projects, "saved")
	s.SavedChange(saved)
	if _, exists := s.Selection().Projects["saved"]; exists {
		t.Fatal("deleted project resurrected")
	}
}
