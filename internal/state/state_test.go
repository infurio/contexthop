package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectLocalDetectsManagedDrift(t *testing.T) {
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "observed@example.com")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "observed-project")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))
	manifest := Manifest{
		Version:   1,
		SessionID: "test-session",
		Expected: Component{
			Identity: "expected@example.com",
			Project:  "expected-project",
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SessionFileEnv, path)

	snapshot := InspectLocal()
	if snapshot.LocalStatus != "CONTEXT-DRIFT" {
		t.Fatalf("LocalStatus = %q, want CONTEXT-DRIFT", snapshot.LocalStatus)
	}
	if !strings.Contains(snapshot.Message, "identity expected") {
		t.Fatalf("Message = %q, want identity mismatch", snapshot.Message)
	}
}

func TestInspectLocalUnmanaged(t *testing.T) {
	t.Setenv(SessionFileEnv, "")
	t.Setenv("CLOUDSDK_CONFIG", "")
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))

	snapshot := InspectLocal()
	if snapshot.Managed || snapshot.LocalStatus != "UNMANAGED" {
		t.Fatalf("snapshot = %#v, want unmanaged", snapshot)
	}
}

func TestInspectLocalReadsGcloudAndDockerFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(SessionFileEnv, "")
	t.Setenv("CLOUDSDK_CONFIG", "")
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_CONFIG", filepath.Join(home, ".docker"))
	t.Setenv("KUBECONFIG", filepath.Join(home, "missing-kubeconfig"))

	gcloudDir := filepath.Join(home, ".config", "gcloud")
	if err := os.MkdirAll(filepath.Join(gcloudDir, "configurations"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gcloudDir, "active_config"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gcloudConfig := "[core]\naccount = person@example.com\nproject = example-project\n"
	if err := os.WriteFile(filepath.Join(gcloudDir, "configurations", "config_work"), []byte(gcloudConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".docker"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".docker", "config.json"), []byte(`{"currentContext":"orbstack"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot := InspectLocal()
	if snapshot.Observed.Identity != "person@example.com" || snapshot.Observed.Project != "example-project" || snapshot.Observed.Docker != "orbstack" {
		t.Fatalf("observed = %#v", snapshot.Observed)
	}
}

func TestReadKubeStateIncludesEffectiveNamespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `apiVersion: v1
kind: Config
current-context: work
contexts:
- name: work
  context:
    cluster: work
    namespace: payments
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)
	contextName, namespace := readKubeState()
	if contextName != "work" || namespace != "payments" {
		t.Fatalf("kubernetes state = %q, %q", contextName, namespace)
	}
}

func TestNamespaceMismatchIsDrift(t *testing.T) {
	snapshot := Snapshot{
		Managed:  true,
		Expected: Component{Kubernetes: "work", Namespace: "default"},
		Observed: Component{Kubernetes: "work", Namespace: "payments"},
	}
	mismatches := snapshot.Mismatches()
	if len(mismatches) != 1 || !strings.Contains(mismatches[0], "namespace") {
		t.Fatalf("mismatches = %#v", mismatches)
	}
}

func TestControlPlaneFingerprintMismatchIsDrift(t *testing.T) {
	snapshot := Snapshot{
		Managed:                       true,
		ExpectedKubernetesFingerprint: "k8s-control-plane:v1:expected",
		ObservedKubernetesFingerprint: "k8s-control-plane:v1:observed",
	}
	mismatches := snapshot.Mismatches()
	if len(mismatches) != 1 || !strings.Contains(mismatches[0], "Kubernetes control plane") {
		t.Fatalf("mismatches = %#v", mismatches)
	}
}

func TestManagedDisabledProviderSentinelsDoNotClaimAProvider(t *testing.T) {
	t.Setenv("CONTEXTHOP_CONTEXT", "local")
	t.Setenv("AWS_PROFILE", "contexthop-none")
	t.Setenv("CLOUDSDK_CONFIG", "/tmp/gcloud-disabled")
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/tmp/disabled-google-credentials.json")
	if provider := detectProvider(); provider != "" {
		t.Fatalf("provider = %q", provider)
	}
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "person@example.com")
	if provider := detectProvider(); provider != "gcp" {
		t.Fatalf("provider = %q", provider)
	}
}
