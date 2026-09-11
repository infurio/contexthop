package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/testenv"
)

// These are the same catalog and provider executables used by VHS tapes.
func TestAcmeRecordingScenario(t *testing.T) {
	e := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(e.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Identities["Acme Engineering"].Account != "alex@acme.example" || cfg.Destinations["Local containers"].Docker != "Local Docker" {
		t.Fatal("recording scenario changed")
	}
	if !strings.HasPrefix(cfg.Identities["Acme Engineering"].CloudSDKConfig, e.Root+string(os.PathSeparator)) {
		t.Fatal("identity escaped fixture")
	}
	resolved, err := resolver.Destination(cfg, "Local containers")
	if err != nil {
		t.Fatal(err)
	}
	if err := runResolvedMode(resolved, []string{"/bin/sh", "-c", `test "$(docker context show)" = desktop-linux`}, false); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("docker", "context", "show")
	output, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) != "default" {
		t.Fatal("child activation changed parent", string(output), err)
	}
}
