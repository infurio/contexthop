package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
)

func TestCredentialOverridesRequireReactivation(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	resolved := resolver.Resolved{Name: "review", IdentityName: "review", Identity: &config.Identity{Provider: "gcp", Account: "review@example.com", CloudSDKConfig: t.TempDir()}}
	prepared, err := session.Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	for _, item := range prepared.Env {
		key, value, _ := strings.Cut(item, "=")
		t.Setenv(key, value)
	}
	for _, key := range []string{"CLOUDSDK_AUTH_ACCESS_TOKEN", "CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE", "CLOUDSDK_AUTH_ACCESS_TOKEN_FILE", "CLOUDSDK_AUTH_DISABLE_CREDENTIALS"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "unexpected-principal")
			equivalent, err := currentContextEquivalent(resolved)
			if err != nil || equivalent {
				t.Fatalf("equivalent = %v, error = %v", equivalent, err)
			}
			refreshed, err := session.Prepare(context.Background(), resolved)
			if err != nil {
				t.Fatal(err)
			}
			defer refreshed.Close()
			for _, binding := range refreshed.Env {
				if strings.HasPrefix(binding, key+"=") {
					t.Fatalf("override survived: %s", binding)
				}
			}
		})
	}
}

func TestReviewCommandHelper(t *testing.T) {
	switch os.Getenv("CONTEXTHOP_TEST_COMMAND_HELPER") {
	case "guard":
		for _, action := range []string{"restore", "reset"} {
			if err := ensureNoActiveSessions(action); err == nil {
				t.Fatalf("%s permitted during exec", action)
			}
		}
		if _, err := os.Stat(os.Getenv("KUBECONFIG")); err != nil {
			t.Fatal(err)
		}
	case "exit":
		os.Args = []string{"chop", "exec", "review", "--", "/bin/sh", "-c", "exit 42"}
		main()
	}
}

func TestExecProtectsSessionAndCleansRegistry(t *testing.T) {
	for _, scenario := range []string{"guard", "failure", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
			t.Setenv("CONTEXTHOP_TEST_COMMAND_HELPER", "guard")
			prepared, err := session.Prepare(context.Background(), resolver.Resolved{Name: "review"})
			if err != nil {
				t.Fatal(err)
			}
			args := []string{os.Args[0], "-test.run=^TestReviewCommandHelper$"}
			if scenario == "failure" {
				args = []string{"/bin/sh", "-c", "exit 42"}
			}
			if scenario == "missing" {
				args = []string{filepath.Join(t.TempDir(), "missing")}
			}
			err = activatePrepared(prepared, args, "review", nil, nil)
			if scenario == "guard" && err != nil {
				t.Fatal(err)
			}
			if scenario != "guard" && err == nil {
				t.Fatal("expected command failure")
			}
			records, err := session.ActiveSessions(-1)
			if err != nil || len(records) != 0 {
				t.Fatalf("registry after command: %v, %v", records, err)
			}
			if _, err := os.Stat(prepared.Directory); !os.IsNotExist(err) {
				t.Fatalf("session not removed: %v", err)
			}
		})
	}
}

func TestExecPreservesChildExitCode(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	t.Setenv("CONTEXTHOP_TEST_COMMAND_HELPER", "exit")
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("CONTEXTHOP_CONFIG", path)
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "default"}
	cfg.Destinations["review"] = config.Destination{Docker: "local"}
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestReviewCommandHelper$")
	output, err := command.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 42 {
		t.Fatalf("exit = %v; output = %s", err, output)
	}
	if strings.Contains(string(output), "chop:") {
		t.Fatalf("child exit reported as activation error: %s", output)
	}
}
