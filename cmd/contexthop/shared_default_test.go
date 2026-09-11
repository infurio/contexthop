package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
)

func TestSharedShellHelper(t *testing.T) {
	if os.Getenv("CHOP_SHARED_TEST_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			if err := run(args[i+1:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(2)
}

// Real independent Zsh processes keep their own environments across publications.
func TestSharedDefaultAcrossZshTerminals(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh unavailable")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	t.Setenv("CHOP_SHARED_TEST_HELPER", "1")
	// Race-instrumented helper processes otherwise sleep one second at each exit.
	// Keep race reporting enabled without charging that delay to every shell hook.
	t.Setenv("GORACE", strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
	for _, key := range []string{"CONTEXTHOP_SCOPE", "CONTEXTHOP_SHARED_REVISION", "CONTEXTHOP_SESSION_FILE", "CONTEXTHOP_CONTEXT"} {
		t.Setenv(key, "")
	}
	t.Setenv("DOCKER_CONTEXT", "native")
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "native@example.com")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "native-kubeconfig"))
	native := os.Getenv("KUBECONFIG")
	if err := os.WriteFile(native, []byte("native config"), 0600); err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	t.Setenv("TEST_SHARED_EXECUTABLE", executable)
	binary := filepath.Join(t.TempDir(), "chop")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec \"$TEST_SHARED_EXECUTABLE\" -test.run=TestSharedShellHelper -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	type terminal struct {
		in  io.WriteCloser
		out *bufio.Reader
	}
	start := func() terminal {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, zsh, "-f")
		in, _ := cmd.StdinPipe()
		out, _ := cmd.StdoutPipe()
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { in.Close(); _ = cmd.Wait() })
		io.WriteString(in, session.CurrentShellInit(binary)+"\nprint READY\n")
		reader := bufio.NewReader(out)
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "READY" {
			t.Fatalf("shell initialization: %q %v", line, err)
		}
		return terminal{in, reader}
	}
	command := func(shell terminal, script string) string {
		t.Helper()
		// Noninteractive Zsh doesn't run hooks automatically, so invoke the same preexec hook.
		io.WriteString(shell.in, "_chop_sync_default\n"+script+"\nprint END\n")
		var lines []string
		for {
			line, err := shell.out.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			line = strings.TrimSpace(line)
			if line == "END" {
				return strings.Join(lines, "\n")
			}
			lines = append(lines, line)
		}
	}
	publish := func(name string) {
		t.Helper()
		prepared, err := session.Prepare(context.Background(), resolver.Resolved{Name: name, IdentityName: name,
			Identity:   &config.Identity{Provider: "gcp", Account: name + "@example.com", CloudSDKConfig: filepath.Join(os.Getenv("HOME"), name)},
			DockerName: name, Docker: &config.Docker{Context: name}})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Close()
		if err := session.PublishShared(prepared); err != nil {
			t.Fatal(err)
		}
	}
	first, second := start(), start()
	publish("dev")
	show := `print -r -- "$CONTEXTHOP_CONTEXT|$CLOUDSDK_CORE_ACCOUNT|$DOCKER_CONTEXT|$CONTEXTHOP_SCOPE"`
	for _, shell := range []terminal{first, second} {
		if got := command(shell, show); got != "dev|dev@example.com|dev|shared" {
			t.Fatal(got)
		}
	}
	path1 := command(first, `print -r -- "$KUBECONFIG"`)
	path2 := command(second, `print -r -- "$KUBECONFIG"`)
	if path1 == path2 || path1 == native {
		t.Fatal("terminals share kubeconfig")
	}
	// Pin one shell; new defaults affect only followers.
	command(first, `export CONTEXTHOP_SCOPE=local`)
	publish("prod")
	if got := command(first, show); got != "dev|dev@example.com|dev|local" {
		t.Fatal(got)
	}
	if got := command(second, show); got != "prod|prod@example.com|prod|shared" {
		t.Fatal(got)
	}
	third := start()
	if got := command(third, show); got != "prod|prod@example.com|prod|shared" {
		t.Fatal(got)
	}
	if got := command(first, "chop shared use\n"+show); got != "prod|prod@example.com|prod|shared" {
		t.Fatal(got)
	}
	if got := command(second, "chop shared clear\n"+show); got != "|native@example.com|native|shared" {
		t.Fatal(got)
	}
	if got := command(first, show); got != "|native@example.com|native|shared" {
		t.Fatal(got)
	}
	// A pinned terminal can resume following even after the default was cleared.
	command(first, `export CONTEXTHOP_SCOPE=local DOCKER_CONTEXT=pinned`)
	if got := command(first, "chop default follow\n"+show); got != "|native@example.com|native|shared" {
		t.Fatal(got)
	}
	publish("dev")
	if got := command(third, show); got != "dev|dev@example.com|dev|shared" {
		t.Fatal(got)
	}
	remove, err := session.ShellInit("zsh")
	if err != nil {
		t.Fatal(err)
	}
	if got := command(third, remove+"\n"+show); got != "|native@example.com|native|" {
		t.Fatal(got)
	}
	data, err := os.ReadFile(native)
	if err != nil || string(data) != "native config" {
		t.Fatal("native kubeconfig changed")
	}
}

func TestApplySharedWithoutIntegrationExplainsHowToFollow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	t.Setenv(session.ActivationFileEnv, "")
	before := os.Getenv("CONTEXTHOP_CONTEXT")
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = original; output.Close() }()
	err = runSelectionLaunch(resolver.Resolved{Name: "shared-fixture", Docker: &config.Docker{Context: "fixture"}}, "default-shell")
	os.Stdout = original
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Shared config saved", "This terminal is unchanged", "shell-init zsh --in-place"} {
		if !strings.Contains(string(data), want) {
			t.Fatal(string(data))
		}
	}
	shared, err := session.SharedConfig()
	if err != nil || shared == nil || shared.Destination != "shared-fixture" {
		t.Fatal(shared, err)
	}
	if os.Getenv("CONTEXTHOP_CONTEXT") != before {
		t.Fatal("changed unmanaged terminal")
	}
}
