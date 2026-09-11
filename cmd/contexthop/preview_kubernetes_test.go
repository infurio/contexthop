package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/ui"
)

func writePreviewSource(t *testing.T, namespace string) (string, string) {
	t.Helper()
	data := "apiVersion: v1\nkind: Config\ncurrent-context: Acme-Dev\ncontexts:\n- name: unrelated\n  context:\n    cluster: cluster\n    namespace: unrelated\n- name: Acme-Dev\n  context:\n    cluster: cluster\n    namespace: " + namespace + "\nclusters:\n- name: cluster\n  cluster:\n    server: https://example.invalid\n"
	path := filepath.Join(t.TempDir(), "source.yaml")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func TestPreviewKubernetesSourceNamespace(t *testing.T) {
	t.Setenv("PATH", "") // Preview must not invoke external tools.
	source, _ := writePreviewSource(t, "payments")
	empty, _ := writePreviewSource(t, "")
	invalid := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(invalid, []byte("contexts: ["), 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	for _, tc := range []struct{ name, paths, override, context, want string }{
		{"inherited", source, "", "Acme-Dev", "payments"},
		{"default", empty, "", "Acme-Dev", "default"},
		{"override", source, "reports", "Acme-Dev", "reports"},
		{"merge-first", source + string(os.PathListSeparator) + empty, "", "Acme-Dev", "payments"},
		{"merge-empty-first", empty + string(os.PathListSeparator) + source, "", "Acme-Dev", "default"},
		{"missing-optional", missing + string(os.PathListSeparator) + source, "", "Acme-Dev", "payments"},
		{"missing", missing, "", "Acme-Dev", "source default"},
		{"invalid", invalid, "", "Acme-Dev", "source default"},
		{"unknown-context", source, "", "absent", "source default"},
		{"generated", "", "", "", "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := previewKubernetesNamespace(config.Kubernetes{Kubeconfig: tc.paths, Context: tc.context, Namespace: tc.override})
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	// A changed source should be reflected the next time it is previewed.
	if err := os.WriteFile(source, []byte("contexts:\n- name: Acme-Dev\n  context:\n    namespace: changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := previewKubernetesNamespace(config.Kubernetes{Kubeconfig: source, Context: "Acme-Dev"}); got != "changed" {
		t.Fatal(got)
	}
}

func TestAppliedKubernetesSelectionMatchesReopenedHeader(t *testing.T) {
	for _, namespace := range []string{"", "payments"} {
		t.Run("namespace="+namespace, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			source, data := writePreviewSource(t, namespace)
			t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			t.Setenv("TEST_KUBE_SOURCE", source)
			if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte("#!/bin/sh\n[ \"$1 $2\" = 'config view' ] || exit 90\n/bin/cat \"$TEST_KUBE_SOURCE\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			cfg := config.New()
			cfg.Kubernetes["dev"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: source, Context: "Acme-Dev", Cluster: "gke-acme-dev"}
			cfg.Destinations["Dev workspace"] = config.Destination{Kubernetes: "dev"}
			for _, screen := range []ui.Screen{ui.ScreenWorkspace, ui.ScreenKubernetes} {
				name := "dev"
				if screen == ui.ScreenWorkspace {
					name = "Dev workspace"
				}
				resolved, err := resolveShellSelection(cfg, ui.Draft{screen: name})
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := session.Prepare(context.Background(), resolved)
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.Close()
				for _, entry := range prepared.Env {
					key, value, _ := strings.Cut(entry, "=")
					t.Setenv(key, value)
				}
				snapshot := state.InspectLocal()
				if snapshot.LocalStatus != "LOCAL MATCH" {
					t.Fatalf("fresh context not matched: %s", snapshot.Message)
				}
				preview := nextShellPreview(cfg, screen, ui.Option{Name: name}, ui.Draft{})
				want := snapshot.Observed.Kubernetes + "/" + snapshot.Observed.Namespace
				for _, field := range preview.Fields {
					if field.Label == "k8s" && field.Value != want {
						t.Fatalf("selected %q != active %q", field.Value, want)
					}
				}
				model := ui.NewAppModel(ui.AppOptions{StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, Snapshot: snapshot,
					Pickers:       map[ui.Screen]ui.Picker{screen: {Screen: screen, ResourceBrowser: true, Options: []ui.Option{{Name: name}}}},
					LaunchPreview: func(s ui.Screen, o ui.Option, d ui.Draft) ui.LaunchPreview { return nextShellPreview(cfg, s, o, d) },
				})
				next, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
				view := ansi.Strip(next.(ui.AppModel).View().Content)
				if strings.Count(view, want) < 2 || strings.Contains(view, "source default") {
					t.Fatalf("header mismatch:\n%s", view)
				}
				// A native namespace change must still appear as drift, not be copied into Selected.
				drifted := strings.Replace(data, "namespace: "+namespace+"\nclusters:", "namespace: drifted\nclusters:", 1)
				if err := os.WriteFile(os.Getenv("KUBECONFIG"), []byte(drifted), 0600); err != nil {
					t.Fatal(err)
				}
				if state.InspectLocal().LocalStatus != "CONTEXT-DRIFT" {
					t.Fatal("namespace drift hidden")
				}
				again := nextShellPreview(cfg, screen, ui.Option{Name: name}, ui.Draft{})
				for _, field := range again.Fields {
					if field.Label == "k8s" && field.Value != want {
						t.Fatal("preview inherited session drift")
					}
				}
			}
			original, err := os.ReadFile(source)
			if err != nil || string(original) != data {
				t.Fatal("source modified")
			}
		})
	}
}

func TestPreviewGeneratedGKEContext(t *testing.T) {
	cfg := previewTestConfig()
	preview := nextShellPreview(cfg, ui.ScreenKubernetes, ui.Option{Name: "dev-cluster"}, ui.Draft{ui.ScreenIdentity: "alice"})
	for _, field := range preview.Fields {
		if field.Label == "k8s" && field.Value != "gke_dev-project_us-east1_dev-cluster/payments" {
			t.Fatal(field.Value)
		}
	}
}
