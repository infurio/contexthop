package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/state"
)

func TestSwitchBackHistoryIsPerShellAndFailureAtomic(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))
	t.Setenv(state.SessionFileEnv, "")
	prepare := func(name string) *Session {
		s, err := Prepare(context.Background(), resolver.Resolved{Name: name, DockerName: name, Docker: &config.Docker{Context: name}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	}
	manifest := func(s *Session) state.Manifest {
		m, err := state.LoadManifest(s.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	a, x := prepare("a"), prepare("x")
	activation := filepath.Join(t.TempDir(), "activate.zsh")
	t.Setenv(state.SessionFileEnv, a.Manifest)
	b := prepare("b")
	if err := b.WriteActivation(activation); err != nil {
		t.Fatal(err)
	}
	if m := manifest(b); m.Previous == nil || m.Previous.Name != "a" {
		t.Fatalf("b history: %#v", m.Previous)
	}
	// A second terminal has its own predecessor, even though recency is shared.
	t.Setenv(state.SessionFileEnv, x.Manifest)
	y := prepare("y")
	if err := y.WriteActivation(activation); err != nil {
		t.Fatal(err)
	}
	if m := manifest(y); m.Previous == nil || m.Previous.Name != "x" {
		t.Fatalf("y history: %#v", m.Previous)
	}
	if m := manifest(b); m.Previous.Name != "a" {
		t.Fatal("other terminal changed b")
	}
	// Toggle back by preparing a fresh A; B becomes the new previous selection.
	t.Setenv(state.SessionFileEnv, b.Manifest)
	back := prepare("a")
	if err := back.WriteActivation(activation); err != nil {
		t.Fatal(err)
	}
	if m := manifest(back); m.Previous == nil || m.Previous.Name != "b" {
		t.Fatal("toggle lost b")
	}
	failed := prepare("failed")
	before, _ := os.ReadFile(b.Manifest)
	if err := failed.WriteActivation(filepath.Join(t.TempDir(), "missing", "activation")); err == nil {
		t.Fatal("expected failure")
	}
	after, _ := os.ReadFile(b.Manifest)
	if string(before) != string(after) {
		t.Fatal("failed activation mutated active history")
	}
	child := prepare("child")
	if m := manifest(child); m.Previous != nil {
		t.Fatal("child inherited parent history")
	}
}

func TestSwitchBackSameSelectionPreservesPrevious(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))
	t.Setenv(state.SessionFileEnv, "")
	prepare := func(name string) *Session {
		s, err := Prepare(context.Background(), resolver.Resolved{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	}
	a := prepare("a")
	t.Setenv(state.SessionFileEnv, a.Manifest)
	b := prepare("b")
	if err := b.WriteActivation(filepath.Join(t.TempDir(), "activation")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.SessionFileEnv, b.Manifest)
	b2 := prepare("b")
	if err := b2.WriteActivation(filepath.Join(t.TempDir(), "activation")); err != nil {
		t.Fatal(err)
	}
	m, err := state.LoadManifest(b2.Manifest)
	if err != nil || m.Previous == nil || m.Previous.Name != "a" {
		t.Fatalf("history: %#v %v", m.Previous, err)
	}
}

func TestWorkspaceProductionPrompt(t *testing.T) {
	m := state.Manifest{WorkspaceName: "Payments production", KubernetesLabel: "payments", Production: true, Expected: state.Component{Kubernetes: "payments", Namespace: "default"}}
	if got := formatPromptPrefix(m, "default", false); got != "[PROD|Payments production|payments|default] " {
		t.Fatal(got)
	}
	if got := formatPromptPrefix(m, "default", true); !strings.Contains(got, "%F{39}") || !strings.Contains(got, "Payments production") {
		t.Fatal(got)
	}
}
