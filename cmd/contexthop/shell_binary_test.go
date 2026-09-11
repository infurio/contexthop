package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/session"
)

func TestHomebrewShellSurvivesUpgradeAndCleanup(t *testing.T) {
	prefix, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeVersion := func(version string) string {
		binary := filepath.Join(prefix, "Cellar", "contexthop", version, "bin", "chop")
		if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(binary, []byte("#!/bin/sh\ncase \"$1\" in _shared-sync|_shell-baseline) exit 0;; esac\nprintf '"+version+"\\n'\n"), 0755); err != nil {
			t.Fatal(err)
		}
		return binary
	}
	old := writeVersion("0.1.1")
	link := filepath.Join(prefix, "bin", "chop")
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, link); err != nil {
		t.Fatal(err)
	}
	stable := stableShellBinary(old, old)
	if stable != link {
		t.Fatalf("binary = %q, want %q", stable, link)
	}
	script := session.CurrentShellInit(stable)
	next := writeVersion("0.1.2")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(next, link); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(filepath.Dir(old))); err != nil {
		t.Fatal(err)
	}
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh unavailable")
	}
	output, err := exec.Command(zsh, "-f", "-c", script+"\nchop version\n").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "0.1.2" {
		t.Fatalf("shell after upgrade: %s, %v", output, err)
	}
}

func TestShellBinaryRejectsUnrelatedPath(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current")
	other := filepath.Join(dir, "chop")
	for _, path := range []string{current, other} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	if got := stableShellBinary(current, "chop"); got != current {
		t.Fatalf("selected unrelated binary %q", got)
	}
}
