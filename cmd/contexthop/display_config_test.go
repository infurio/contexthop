package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/testenv"
)

func TestDisplayConfigPersistsAndRefreshes(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	t.Setenv("CONTEXTHOP_CONFIG", env.Catalog)
	t.Setenv("CONTEXTHOP_SESSION_FILE", "")
	t.Setenv("CONTEXTHOP_PROMPT_PREFIX", "[Acme] ")
	original, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	if got := configuredPromptPrefix(); got != "" {
		t.Fatalf("default prefix = %q", got)
	}
	for _, value := range []string{"off", "prompt", "off"} {
		if err := runConfig([]string{"display", value}); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(env.Catalog)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Display != value || cfg.Clone().Display != value {
			t.Fatal("prompt preference lost during persistence or cloning")
		}
		cfg.Display = original.Display
		if !reflect.DeepEqual(cfg, original) {
			t.Fatal("prompt preference changed catalog resources")
		}
		want := "[Acme] "
		if value == "off" {
			want = ""
		}
		// Repeated calls model separate existing terminals reading the same file.
		for range 2 {
			if got := configuredPromptPrefix(); got != want {
				t.Fatalf("prefix for %s = %q, want %q", value, got, want)
			}
		}
	}
	before, err := os.ReadFile(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"invalid"}, {"on", "off"}} {
		if err := runDisplayConfig(env.Catalog, args); err == nil {
			t.Fatal("invalid preference accepted")
		}
	}
	after, err := os.ReadFile(env.Catalog)
	if err != nil || string(before) != string(after) {
		t.Fatal("invalid command changed configuration")
	}
	cfg := original.Clone()
	cfg.Display = "invalid"
	if cfg.Validate() == nil {
		t.Fatal("invalid YAML preference accepted")
	}
	// Another catalog operation must retain the preference.
	cfg, err = config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := catalog.PlanAddDocker(cfg, "Acme extra", config.Docker{Context: "acme-extra"})
	if err != nil || plan.Config.Display != "off" {
		t.Fatal("catalog edit dropped prompt preference")
	}
}

func TestDisplayModesDefaultAndPersistence(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	t.Setenv("NO_COLOR", "1")
	manifest := state.Manifest{Version: 1, SessionID: "acme-mode", Expected: state.Component{Identity: "alex@acme.example"}}
	data, _ := json.Marshal(manifest)
	path := filepath.Join(env.Root, "session.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.SessionFileEnv, path)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DisplayMode() != "summary" || configuredPromptPrefix() != "" || configuredSummary() != "ContextHop "+version+"\nIdentity: alex@acme.example" {
		t.Fatal("wrong default display")
	}
	for _, mode := range []string{"prompt", "off", "summary"} {
		if err := runConfig([]string{"display", mode}); err != nil {
			t.Fatal(err)
		}
		cfg, err = loadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DisplayMode() != mode || cfg.Clone().DisplayMode() != mode {
			t.Fatal("display mode did not persist")
		}
		if (configuredPromptPrefix() != "") != (mode == "prompt") || (configuredSummary() != "") != (mode == "summary") {
			t.Fatal("mode did not control output")
		}
	}
	if err := runConfig([]string{"display", "invalid"}); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
