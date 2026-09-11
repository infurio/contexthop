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

func TestPrepareRecordsEffectiveNamespace(t *testing.T) {
	for _, tc := range []struct{ name, source, override, want string }{
		{"inherited", "payments", "", "payments"},
		{"default", "", "", "default"},
		{"override", "payments", "reports", "reports"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			fixture := `apiVersion: v1
kind: Config
current-context: work
contexts:
- name: unrelated
  context:
    cluster: cluster
    namespace: unrelated
- name: work
  context:
    cluster: cluster
    namespace: NAMESPACE
clusters:
- name: cluster
  cluster:
    server: https://example.invalid
`
			sourceData := strings.ReplaceAll(fixture, "NAMESPACE", tc.source)
			source := filepath.Join(t.TempDir(), "source.yaml")
			if err := os.WriteFile(source, []byte(sourceData), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("NAMESPACE_SOURCE", sourceData)
			t.Setenv("NAMESPACE_OVERRIDE", strings.ReplaceAll(fixture, "NAMESPACE", tc.override))
			log := filepath.Join(t.TempDir(), "calls")
			t.Setenv("NAMESPACE_LOG", log)
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$NAMESPACE_LOG"
case "$1 $2" in
  'config view') printf '%s' "$NAMESPACE_SOURCE" ;;
  'config set-context') printf '%s' "$NAMESPACE_OVERRIDE" > "$KUBECONFIG" ;;
  *) exit 1 ;;
esac
`
			if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			resolved := resolver.Resolved{Name: "work", KubernetesName: "work", Kubernetes: &config.Kubernetes{Type: "kubeconfig", Kubeconfig: source, Context: "work", Namespace: tc.override}}
			// Both cold materialization and a warm cache must retain the effective namespace.
			for attempt := range 2 {
				prepared, err := Prepare(context.Background(), resolved)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = prepared.Close() })
				manifest, err := state.LoadManifest(prepared.Manifest)
				if err != nil {
					t.Fatal(err)
				}
				if manifest.Expected.Namespace != tc.want {
					t.Fatalf("attempt %d: namespace = %q, want %q", attempt, manifest.Expected.Namespace, tc.want)
				}
				for key, value := range environmentMap(prepared.Env) {
					t.Setenv(key, value)
				}
				if snapshot := state.InspectLocal(); snapshot.LocalStatus != "LOCAL MATCH" {
					t.Fatalf("fresh session reports drift: %s", snapshot.Message)
				}
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(calls), "config view") != 1 {
				t.Fatalf("expected cache hit on second preparation: %s", calls)
			}
			if tc.override != "" && !strings.Contains(string(calls), "--namespace "+tc.override) {
				t.Fatalf("namespace override not applied: %s", calls)
			}
			if data, err := os.ReadFile(source); err != nil || string(data) != sourceData {
				t.Fatalf("source kubeconfig changed: %v", err)
			}
		})
	}
}
