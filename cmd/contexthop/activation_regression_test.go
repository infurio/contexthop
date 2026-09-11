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
	"github.com/infurio/contexthop/internal/ui"
)

func TestRiskLabelsDoNotControlActivation(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "local", LabelSet: config.LabelSet{Labels: map[string]string{"risk": "production"}}}
	draft := ui.Draft{ui.ScreenDocker: "local"}
	resolved, err := resolveShellSelection(cfg, draft)
	if err != nil || resolved.Risk != "" {
		t.Fatal(resolved, err)
	}
	picker := shellMenuPicker(cfg, draft, false)
	found := false
	for _, field := range picker.ContextFields {
		if field.Label == "Risk" {
			found = true
		}
	}
	if found {
		t.Fatal("review exposes operational risk")
	}
	cfg.Docker["local"] = config.Docker{Context: "local", LabelSet: config.LabelSet{GoogleLabels: map[string]string{"risk": "production"}}}
	resolved, err = resolveShellSelection(cfg, draft)
	if err != nil || resolved.Risk != "" {
		t.Fatal("provider label became policy")
	}
}

func TestReviewedWorkspaceRejectsChangedDependencies(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "reviewed"}
	cfg.Destinations["demo"] = config.Destination{Docker: "local"}
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("CONTEXTHOP_CONFIG", path)
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := validateReviewedWorkspace(cfg, "demo", ""); err != nil {
		t.Fatal(err)
	}
	changed := cfg.Clone()
	changed.Docker["local"] = config.Docker{Context: "changed"}
	if err := config.Write(path, changed); err != nil {
		t.Fatal(err)
	}
	if err := validateReviewedWorkspace(cfg, "demo", ""); err == nil {
		t.Fatal("changed dependency was accepted")
	}
}

func TestExplicitSubshellStartsAndPreservesExitStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONTEXTHOP_CACHE_DIR", dir)
	resolved := resolver.Resolved{Name: "test", DockerName: "local", Docker: &config.Docker{Context: "test-local"}}
	prepared, err := session.Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err = prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, item := range prepared.Env {
		key, value, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "CONTEXTHOP_") || strings.HasPrefix(key, "CLOUDSDK_") || strings.HasPrefix(key, "DOCKER_") || strings.HasPrefix(key, "GOOGLE_") || strings.HasPrefix(key, "AWS_") || key == "KUBECONFIG" {
			t.Setenv(key, value)
		}
	}
	t.Setenv(session.ActivationFileEnv, "")
	shell := filepath.Join(dir, "test-shell")
	if err = os.WriteFile(shell, []byte("#!/bin/sh\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	if err = runResolvedMode(resolved, nil, false); err != nil {
		t.Fatal("equivalent apply failed", err)
	}
	err = runResolvedMode(resolved, nil, true)
	var completed *shellExitError
	var exited *exec.ExitError
	if !errors.As(err, &completed) || !errors.As(err, &exited) || exited.ExitCode() != 7 {
		t.Fatal("subshell did not run or lost its status", err)
	}
}

func TestLegacyRiskDoesNotRequireConfirmation(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	resolved := resolver.Resolved{Name: "metadata-only", Risk: "production", Docker: &config.Docker{Context: "local"}}
	if err := runResolved(resolved, []string{"/usr/bin/true"}); err != nil {
		t.Fatal("legacy risk blocked activation", err)
	}
}
