package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/testenv"
)

func TestPromptPrefixConfigPersistsAndRefreshes(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	t.Setenv("CONTEXTHOP_CONFIG", env.Catalog)
	t.Setenv("CONTEXTHOP_SESSION_FILE", "")
	t.Setenv("CONTEXTHOP_PROMPT_PREFIX", "[Acme] ")
	original, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	if got := configuredPromptPrefix(); got != "[Acme] " {
		t.Fatalf("default prefix = %q", got)
	}
	for _, value := range []string{"off", "on", "off"} {
		if err := runConfig([]string{"prompt-prefix", value}); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(env.Catalog)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.PromptPrefix != value || cfg.Clone().PromptPrefix != value {
			t.Fatal("prompt preference lost during persistence or cloning")
		}
		cfg.PromptPrefix = original.PromptPrefix
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
		if err := runPromptPrefixConfig(env.Catalog, args); err == nil {
			t.Fatal("invalid preference accepted")
		}
	}
	after, err := os.ReadFile(env.Catalog)
	if err != nil || string(before) != string(after) {
		t.Fatal("invalid command changed configuration")
	}
	cfg := original.Clone()
	cfg.PromptPrefix = "invalid"
	if cfg.Validate() == nil {
		t.Fatal("invalid YAML preference accepted")
	}
	// Another catalog operation must retain the preference.
	cfg, err = config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := catalog.PlanAddDocker(cfg, "Acme extra", config.Docker{Context: "acme-extra"})
	if err != nil || plan.Config.PromptPrefix != "off" {
		t.Fatal("catalog edit dropped prompt preference")
	}
}
