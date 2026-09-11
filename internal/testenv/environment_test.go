package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewClearsInheritedStateAndRestoresIt(t *testing.T) {
	for _, key := range []string{"CONTEXTHOP_CONTEXT", "CONTEXTHOP_SCOPE", "CONTEXTHOP_SESSION_FILE", "GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG", "DOCKER_CONTEXT", "KUBECONFIG", "AWS_PROFILE", "ZDOTDIR", "XDG_CONFIG_HOME"} {
		t.Setenv(key, "host-value")
	}
	original := os.Environ()
	var root string
	t.Run("isolated", func(t *testing.T) {
		e := New(t, Options{})
		root = e.Root
		for _, key := range []string{"CONTEXTHOP_CONTEXT", "CONTEXTHOP_SCOPE", "CONTEXTHOP_SESSION_FILE", "GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG", "DOCKER_CONTEXT", "KUBECONFIG", "AWS_PROFILE"} {
			if _, ok := os.LookupEnv(key); ok {
				t.Errorf("inherited %s", key)
			}
		}
		for _, key := range []string{"HOME", "ZDOTDIR", "XDG_CONFIG_HOME", "CONTEXTHOP_CONFIG", "CONTEXTHOP_CACHE_DIR"} {
			if !strings.HasPrefix(os.Getenv(key), e.Root+string(os.PathSeparator)) {
				t.Errorf("%s escaped fixture", key)
			}
		}
		if err := exec.Command("gcloud", "auth", "login").Run(); err == nil {
			t.Fatal("default stub allowed provider call")
		}
	})
	// Compare mappings, since environment ordering is not part of restoration.
	asMap := func(env []string) map[string]string {
		m := map[string]string{}
		for _, entry := range env {
			k, v, _ := strings.Cut(entry, "=")
			m[k] = v
		}
		return m
	}
	if !reflect.DeepEqual(asMap(os.Environ()), asMap(original)) {
		t.Fatal("test cleanup did not restore environment")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("fixture root survived test cleanup", err)
	}
}

func TestAcmeScenarioIsDisposableAndRejectsUnknownOperations(t *testing.T) {
	for i := 0; i < 2; i++ {
		e, err := Create(t.TempDir(), Options{Scenario: "acme"})
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := os.ReadFile(e.Catalog)
		if err != nil || strings.Contains(string(catalog), "__FIXTURE__") || !strings.Contains(string(catalog), e.Root+"/gcloud/acme") {
			t.Fatal("catalog not materialized", err)
		}
		cmd := exec.Command(filepath.Join(e.Bin, "gcloud"), "projects", "list", "--format=json")
		cmd.Env = e.Environ()
		output, err := cmd.Output()
		if err != nil || !strings.Contains(string(output), "acme-analytics") {
			t.Fatal(string(output), err)
		}
		for _, name := range []string{"gcloud", "docker", "kubectl"} {
			cmd := exec.Command(filepath.Join(e.Bin, name), "unexpected-operation")
			cmd.Env = e.Environ()
			if err := cmd.Run(); err == nil {
				t.Fatal(name, "accepted unknown operation")
			}
		}
		if err := os.WriteFile(e.Catalog, []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCustomProviderStubOverridesDefault(t *testing.T) {
	e := New(t, Options{})
	if err := os.WriteFile(filepath.Join(e.Bin, "gcloud"), []byte("#!/bin/sh\nprintf custom\n"), 0700); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("gcloud").Output()
	if err != nil || string(out) != "custom" {
		t.Fatal(string(out), err)
	}
}
