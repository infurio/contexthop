// Package testenv supplies disposable environments for tests and recordings.
// It is not imported by the production application.
package testenv

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed fixtures/acme/catalog.yaml fixtures/acme/bin/*
var fixtures embed.FS

type Options struct {
	Scenario string // Empty installs rejecting provider stubs; "acme" uses fictional responses.
	Binary   string // Optional absolute path to the application built from the checkout.
	Path     string // Optional tooling path after the fixture's provider stubs.
	Delay    string // Optional provider delay for recordings; tests default to zero.
}

type Environment struct {
	Root, Home, Cache, Bin, Catalog string
	Vars                            map[string]string
}

func Create(root string, options Options) (*Environment, error) {
	if options.Scenario != "" && options.Scenario != "acme" {
		return nil, fmt.Errorf("unknown fixture scenario %q", options.Scenario)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	e := &Environment{Root: root, Home: filepath.Join(root, "home"), Cache: filepath.Join(root, "cache"), Bin: filepath.Join(root, "bin"), Catalog: filepath.Join(root, "catalog.yaml")}
	for _, dir := range []string{e.Home, e.Cache, e.Bin, filepath.Join(e.Home, ".config"), filepath.Join(e.Home, ".cache"), filepath.Join(e.Home, ".local", "share")} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"gcloud", "docker", "kubectl"} {
		script := []byte("#!/bin/sh\n# No provider commands are allowed without an explicit fixture.\nexit 1\n")
		if options.Scenario == "acme" {
			script, err = fixtures.ReadFile("fixtures/acme/bin/" + name)
			if err != nil {
				return nil, err
			}
		}
		if err = os.WriteFile(filepath.Join(e.Bin, name), script, 0700); err != nil {
			return nil, err
		}
	}
	if options.Scenario == "acme" {
		catalog, err := fixtures.ReadFile("fixtures/acme/catalog.yaml")
		if err != nil {
			return nil, err
		}
		if err = os.WriteFile(e.Catalog, []byte(strings.ReplaceAll(string(catalog), "__FIXTURE__", root)), 0600); err != nil {
			return nil, err
		}
	}
	if err = os.WriteFile(filepath.Join(e.Home, ".zshrc"), []byte("PROMPT='$ '\nsetopt interactivecomments\n"), 0600); err != nil {
		return nil, err
	}
	toolPath := options.Path
	if toolPath == "" {
		toolPath = "/usr/bin:/bin"
	}
	e.Vars = map[string]string{
		"HOME": e.Home, "ZDOTDIR": e.Home, "PATH": e.Bin + string(os.PathListSeparator) + toolPath,
		"XDG_CONFIG_HOME": filepath.Join(e.Home, ".config"), "XDG_CACHE_HOME": filepath.Join(e.Home, ".cache"), "XDG_DATA_HOME": filepath.Join(e.Home, ".local", "share"),
		"CONTEXTHOP_CONFIG": e.Catalog, "CONTEXTHOP_CACHE_DIR": e.Cache,
		"SHELL": "/bin/zsh", "TERM": "xterm-256color", "COLORTERM": "truecolor", "PS1": "$ ", "BASH_SILENCE_DEPRECATION_WARNING": "1",
	}
	if options.Delay != "" {
		e.Vars["CONTEXTHOP_FIXTURE_DELAY"] = options.Delay
	}
	if options.Binary != "" {
		if !filepath.IsAbs(options.Binary) {
			return nil, fmt.Errorf("fixture binary must be an absolute path")
		}
		info, err := os.Stat(options.Binary)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return nil, fmt.Errorf("fixture binary must be executable")
		}
		if err = os.Symlink(options.Binary, filepath.Join(e.Bin, "chop")); err != nil {
			return nil, err
		}
		e.Vars["CONTEXTHOP_BINARY"] = options.Binary
	}
	return e, nil
}

func (e *Environment) Environ() []string {
	keys := make([]string, 0, len(e.Vars))
	for key := range e.Vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+e.Vars[key])
	}
	return env
}

// Install replaces the process environment and returns its exact restoration.
// Use only for serial test processes; subprocess callers should set Cmd.Env.
func (e *Environment) Install() func() {
	original := os.Environ()
	replaceEnvironment(e.Environ())
	return func() { replaceEnvironment(original) }
}

func replaceEnvironment(env []string) {
	os.Clearenv()
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		_ = os.Setenv(key, value)
	}
}

type TestingT interface {
	Helper()
	TempDir() string
	Setenv(string, string)
	Fatalf(string, ...any)
}

// New installs a fixture for a serial Go test. Setenv registers restoration
// through test cleanup and rejects use in parallel tests. TempDir owns removal.
func New(t TestingT, options Options) *Environment {
	t.Helper()
	e, err := Create(t.TempDir(), options)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
		return nil
	}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		t.Setenv(key, "")
		if err = os.Unsetenv(key); err != nil {
			t.Fatalf("clear fixture environment: %v", err)
		}
	}
	for key, value := range e.Vars {
		t.Setenv(key, value)
	}
	return e
}
