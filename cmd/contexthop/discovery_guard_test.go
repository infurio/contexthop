package main

import (
	"context"
	"github.com/infurio/contexthop/internal/session"
	"os"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

func TestManagedDiscoveryCLIAndUI(t *testing.T) {
	t.Setenv("CONTEXTHOP_CONTEXT", "managed")
	t.Setenv("PATH", "")
	t.Setenv("CONTEXTHOP_CONFIG", "/missing/config")
	for _, run := range []func() error{
		func() error { return runDiscover([]string{"--write"}) },
		func() error { return runCatalogDiscovery([]string{"work"}) },
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), "unmanaged shell") {
			t.Fatalf("error = %v", err)
		}
	}
	cfg := config.New()
	check := func(tr ui.Transition) {
		t.Helper()
		if tr.Picker.Screen != "discovery-unavailable" || !strings.Contains(tr.Picker.Description, "unmanaged shell") {
			t.Fatalf("transition = %#v", tr)
		}
		if tr.PersistDiscovery {
			t.Fatal("blocked discovery requested persistence")
		}
	}
	tr, handled := discoveryDialogFlow(cfg, cfg, ui.Choice{Action: "discover-resources"}, ui.Draft{})
	if !handled {
		t.Fatal("discover shortcut not handled")
	}
	check(tr)
	tr, handled = localImportFlow(cfg, ui.Choice{Action: "import-local-contexts"}, nil)
	if !handled {
		t.Fatal("import not handled")
	}
	check(tr)
	check(authenticatedOperation(context.Background(), cfg, "missing", "Discovery", func(string) { t.Fatal("authentication started") }, func(context.Context, func(string)) ui.Transition {
		t.Fatal("discovery started")
		return ui.Transition{}
	}))
	check(runDiscoveryDialog(cfg, cfg, ui.Draft{}, nil))
}

func TestInlineTokenPreventsEquivalentActivation(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	t.Setenv("PATH", "")
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "")
	os.Unsetenv("CLOUDSDK_AUTH_ACCESS_TOKEN")
	resolved := resolver.Resolved{Name: "local", DockerName: "local", Docker: &config.Docker{Context: "local"}}
	prepared, err := session.Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	for _, item := range prepared.Env {
		key, value, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "CONTEXTHOP_") || strings.HasPrefix(key, "CLOUDSDK_") || strings.HasPrefix(key, "GOOGLE_") || strings.HasPrefix(key, "DOCKER_") || key == "KUBECONFIG" {
			t.Setenv(key, value)
		}
	}
	equivalent, err := currentContextEquivalent(resolved)
	if err != nil || !equivalent {
		t.Fatalf("fixture not equivalent: %v, %v", equivalent, err)
	}
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "dummy-token")
	equivalent, err = currentContextEquivalent(resolved)
	if err != nil || equivalent {
		t.Fatalf("equivalent = %v, error = %v", equivalent, err)
	}
}
