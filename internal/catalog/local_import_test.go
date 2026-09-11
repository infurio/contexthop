package catalog

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func TestLocalImportReviewsChangesAndRejectsStaleApply(t *testing.T) {
	cfg := config.New()
	cfg.Docker["saved"] = config.Docker{Context: "old", Risk: "production"}
	cfg.Destinations["work"] = config.Destination{Docker: "saved"}
	observed := config.New()
	observed.Docker["imported"] = config.Docker{Context: "new", Provenance: "docker"}
	plan, err := PlanImportLocal(cfg, observed)
	if err != nil || !plan.Valid() || len(plan.Changes) == 0 || len(cfg.Docker) != 1 || plan.Config.Destinations["work"].Docker != "saved" {
		t.Fatal("import did not produce isolated valid plan", err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	changed := cfg.Clone()
	changed.Docker["extra"] = config.Docker{Context: "extra"}
	if err := config.Write(path, changed); err != nil {
		t.Fatal(err)
	}
	if err := Apply(path, plan); !errors.Is(err, ErrStalePlan) {
		t.Fatal("stale import overwrote catalog", err)
	}
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := Apply(path, plan); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(path)
	if err != nil || len(saved.Docker) != 2 || saved.Docker["saved"].LabelSet.Effective(saved.Docker["saved"].Risk)["risk"] != "production" {
		t.Fatal("import lost saved metadata", err)
	}
}
