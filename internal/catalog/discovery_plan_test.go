package catalog

import (
	"github.com/infurio/contexthop/internal/config"
	"testing"
)

func TestDiscoveryPreservesSavedCustomizationAndHiddenFlags(t *testing.T) {
	saved := config.New()
	saved.Identities["work"] = config.Identity{Provider: "gcp", Account: "person@example.com"}
	saved.Projects["custom"] = config.Project{Provider: "gcp", ProjectID: "project", Hidden: true, Risk: "production", Identities: []string{"work"}}
	observed := saved.Clone()
	item := observed.Projects["custom"]
	item.Hidden = false
	item.Risk = "sandbox"
	observed.Projects["custom"] = item
	observed.Projects["new"] = config.Project{Provider: "gcp", ProjectID: "new", Identities: []string{"work"}}
	plan := PlanSaveDiscovery(saved, observed)
	if !plan.Valid() {
		t.Fatal(plan.Problems)
	}
	if !plan.Config.Projects["custom"].Hidden || plan.Config.Projects["custom"].Risk != "production" {
		t.Fatal("discovery overwrote customization")
	}
	if len(plan.Changes) != 1 || len(plan.Config.Projects) != 2 {
		t.Fatal("discovery did not add only new records")
	}
}
