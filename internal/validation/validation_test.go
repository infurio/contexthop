package validation

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"encoding/pem"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/kubetarget"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/state"
)

func TestValidateChecksSelectedProviders(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeCommand(t, bin, "gcloud", `
if [ "$1" = config ]; then printf '%s\n' "$CLOUDSDK_CORE_ACCOUNT"; exit 0; fi
if [ "$1" = projects ]; then printf '%s\n' "$CLOUDSDK_CORE_PROJECT"; exit 0; fi
exit 1
`)
	writeCommand(t, bin, "kubectl", `
case "$*" in
  *"get namespace payments"*) printf 'namespace/payments\n' ;;
  *) exit 1 ;;
esac
`)
	writeCommand(t, bin, "docker", `
[ "$DOCKER_CONTEXT" = desktop-linux ] || exit 1
printf '28.0\n'
`)

	resolved := resolver.Resolved{
		IdentityName:   "person",
		Identity:       &config.Identity{Provider: "gcp", Account: "person@example.com"},
		ProjectName:    "work",
		Project:        &config.Project{Provider: "gcp", ProjectID: "example-project"},
		KubernetesName: "work-cluster",
		Kubernetes:     &config.Kubernetes{Namespace: "payments"},
		DockerName:     "desktop",
		Docker:         &config.Docker{Context: "desktop-linux"},
	}
	environment := append(os.Environ(),
		"PATH="+bin,
		"CLOUDSDK_CORE_ACCOUNT=person@example.com",
		"CLOUDSDK_CORE_PROJECT=example-project",
		"DOCKER_CONTEXT=desktop-linux",
	)
	var progress []string
	result, err := Validate(context.Background(), resolved, environment, func(message string) {
		progress = append(progress, message)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	if len(progress) < 1 || !strings.Contains(progress[0], "Google project, Kubernetes, Docker") {
		t.Fatalf("progress = %v", progress)
	}
	if len(result.Checks) != 3 || result.Duration <= 0 {
		t.Fatalf("timings = %#v, total = %s", result.Checks, result.Duration)
	}
}

func TestValidateTreatsForbiddenNamespaceAsWarning(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeCommand(t, bin, "kubectl", `
printf 'Error from server (Forbidden): namespaces is forbidden\n' >&2
exit 1
`)
	resolved := resolver.Resolved{
		KubernetesName: "restricted",
		Kubernetes:     &config.Kubernetes{Namespace: "payments"},
	}
	result, err := Validate(context.Background(), resolved, append(os.Environ(), "PATH="+bin), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "not permitted") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	if len(result.WarningDetails) != 1 {
		t.Fatalf("warning details = %#v", result.WarningDetails)
	}
	warning := result.WarningDetails[0]
	if warning.Check != CheckKubernetes || warning.Kind != FailureAuthorization || warning.Scope != "namespace" {
		t.Fatalf("warning = %#v", warning)
	}
}

func TestValidateRejectsMissingNamespace(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeCommand(t, bin, "kubectl", `
exit 0
`)
	resolved := resolver.Resolved{
		KubernetesName: "work",
		Kubernetes:     &config.Kubernetes{Namespace: "missing"},
	}
	_, err := Validate(context.Background(), resolved, append(os.Environ(), "PATH="+bin), nil)
	var validationError *Error
	if err == nil || !errors.As(err, &validationError) {
		t.Fatalf("error = %v", err)
	}
	if validationError.Check != CheckKubernetes || validationError.Kind != FailureNotFound || !strings.Contains(err.Error(), `namespace "missing"`) {
		t.Fatalf("typed error = %#v (%v)", validationError, err)
	}
}

func TestValidateRunsIndependentChecksConcurrently(t *testing.T) {
	bin := t.TempDir()
	markers := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("MARKERS", markers)
	barrier := `
/usr/bin/touch "$MARKERS/${0##*/}"
count=0
while [ ! -f "$MARKERS/gcloud" ] || [ ! -f "$MARKERS/kubectl" ] || [ ! -f "$MARKERS/docker" ]; do
  count=$((count + 1))
  [ "$count" -lt 100 ] || exit 44
  /bin/sleep 0.01
done
`
	writeCommand(t, bin, "gcloud", barrier+`printf '%s\n' "$CLOUDSDK_CORE_PROJECT"`)
	writeCommand(t, bin, "kubectl", barrier+`printf 'namespace/default\n'`)
	writeCommand(t, bin, "docker", barrier+`printf '28.0\n'`)
	resolved := resolver.Resolved{
		Project:        &config.Project{Provider: "gcp", ProjectID: "example-project"},
		KubernetesName: "work", Kubernetes: &config.Kubernetes{Namespace: "default"},
		Docker: &config.Docker{Context: "desktop-linux"},
	}
	environment := append(os.Environ(), "PATH="+bin, "CLOUDSDK_CORE_PROJECT=example-project")
	result, err := Validate(context.Background(), resolved, environment, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Checks) != 3 {
		t.Fatalf("checks = %#v", result.Checks)
	}
}

func TestValidateUsesOneKubernetesRequest(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "kubectl.log")
	t.Setenv("PATH", bin)
	t.Setenv("KUBECTL_LOG", log)
	writeCommand(t, bin, "kubectl", `printf '%s\n' "$*" >> "$KUBECTL_LOG"; printf 'namespace/default\n'`)
	resolved := resolver.Resolved{KubernetesName: "work", Kubernetes: &config.Kubernetes{Namespace: "default"}}
	if _, err := Validate(context.Background(), resolved, append(os.Environ(), "PATH="+bin), nil); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 1 || strings.Contains(lines[0], "--raw=/version") || !strings.Contains(lines[0], "get namespace default") {
		t.Fatalf("kubectl calls = %q", contents)
	}
}

func TestValidateDoesNotSpawnRedundantIdentityCheck(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "gcloud.log")
	t.Setenv("PATH", bin)
	t.Setenv("GCLOUD_LOG", log)
	writeCommand(t, bin, "gcloud", `printf '%s\n' "$*" >> "$GCLOUD_LOG"; printf '%s\n' "$CLOUDSDK_CORE_PROJECT"`)
	resolved := resolver.Resolved{
		Identity: &config.Identity{Provider: "gcp", Account: "person@example.com"},
		Project:  &config.Project{Provider: "gcp", ProjectID: "example-project"},
	}
	environment := append(os.Environ(), "PATH="+bin, "CLOUDSDK_CORE_ACCOUNT=person@example.com", "CLOUDSDK_CORE_PROJECT=example-project")
	if _, err := Validate(context.Background(), resolved, environment, nil); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "config get-value") || strings.Count(strings.TrimSpace(string(contents)), "\n") != 0 {
		t.Fatalf("gcloud calls = %q", contents)
	}
	if !strings.Contains(string(contents), "projects describe example-project") {
		t.Fatalf("project validation missing: %q", contents)
	}
}

func TestValidateHonorsParentCancellation(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeCommand(t, bin, "docker", `exec /bin/sleep 2`)
	resolved := resolver.Resolved{Docker: &config.Docker{Context: "desktop-linux"}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := Validate(ctx, resolved, append(os.Environ(), "PATH="+bin), nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	var validationError *Error
	if !errors.As(err, &validationError) || validationError.Kind != FailureTimeout {
		t.Fatalf("typed error = %#v", validationError)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}

func TestValidateCancelsSiblingChecksAfterFailure(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeCommand(t, bin, "gcloud", `printf 'permission denied\n' >&2; exit 1`)
	writeCommand(t, bin, "docker", `exec /bin/sleep 2`)
	resolved := resolver.Resolved{
		Project: &config.Project{Provider: "gcp", ProjectID: "example-project"},
		Docker:  &config.Docker{Context: "desktop-linux"},
	}
	started := time.Now()
	_, err := Validate(context.Background(), resolved, append(os.Environ(), "PATH="+bin), nil)
	if err == nil || !strings.Contains(err.Error(), "authorization") {
		t.Fatalf("error = %v", err)
	}
	var validationError *Error
	if !errors.As(err, &validationError) || validationError.Check != CheckProject || validationError.Kind != FailureAuthorization {
		t.Fatalf("typed error = %#v", validationError)
	}
	// The race detector can add substantial process-startup latency on loaded
	// CI hosts. Keep this comfortably below the sibling's two-second sleep
	// while avoiding a timing-only failure around the old one-second boundary.
	if elapsed := time.Since(started); elapsed >= 1500*time.Millisecond {
		t.Fatalf("sibling cancellation took %s", elapsed)
	}
}

func TestValidationTimeoutIsBoundedAcrossConcurrentChecks(t *testing.T) {
	previousTimeout := checkTimeout
	checkTimeout = 100 * time.Millisecond
	defer func() { checkTimeout = previousTimeout }()
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeCommand(t, bin, "gcloud", `exec /bin/sleep 2`)
	writeCommand(t, bin, "kubectl", `exec /bin/sleep 2`)
	writeCommand(t, bin, "docker", `exec /bin/sleep 2`)
	resolved := resolver.Resolved{
		Project:        &config.Project{Provider: "gcp", ProjectID: "example-project"},
		KubernetesName: "work", Kubernetes: &config.Kubernetes{Namespace: "default"},
		Docker: &config.Docker{Context: "desktop-linux"},
	}
	started := time.Now()
	_, err := Validate(context.Background(), resolved, append(os.Environ(), "PATH="+bin), nil)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("concurrent timeout multiplied: %s", elapsed)
	}
}

func TestFailureClassification(t *testing.T) {
	cases := map[FailureKind]error{
		FailureAuthentication: errors.New("server returned Unauthorized"),
		FailureAuthorization:  errors.New(`ResponseError: code=403, message=Required "container.clusters.get" permission(s)`),
		FailureNetwork:        errors.New("dial tcp: connection refused"),
		FailureNotFound:       errors.New("resource NotFound"),
		FailureTimeout:        errors.New("kubectl timed out after 8s"),
		FailureCanceled:       context.Canceled,
	}
	for expected, err := range cases {
		if observed := failureKind(err); observed != expected {
			t.Errorf("failureKind(%q) = %q, want %q", err, observed, expected)
		}
	}
}

func TestAuthorizationErrorIsConciseAndActionable(t *testing.T) {
	cause := errors.New(`gcloud: exit status 1: ResponseError: code=403, message=Required "container.clusters.get" permission(s) for a cluster`)
	err := &Error{Check: CheckFingerprint, Kind: failureKind(cause), Subject: `GKE cluster "example-sites-dev"`, Cause: cause}
	message := err.Error()
	for _, expected := range []string{"authorization", "container.clusters.get", "choose an identity with access"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message %q does not contain %q", message, expected)
		}
	}
	if strings.Contains(message, "exit status") || strings.Contains(message, "ResponseError") {
		t.Fatalf("message contains raw provider output: %q", message)
	}
}

func TestValidationErrorProvidesNetworkRecovery(t *testing.T) {
	err := &Error{Check: CheckKubernetes, Kind: FailureNetwork, Subject: "Kubernetes target", Cause: errors.New("connection refused")}
	if message := err.Error(); !strings.Contains(message, "VPN") || !strings.Contains(message, "DNS") {
		t.Fatalf("message = %q", message)
	}
}

func TestValidateRejectsWrongIdentity(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	writeCommand(t, bin, "gcloud", `printf 'someone-else@example.com\n'`)
	resolved := resolver.Resolved{
		IdentityName: "person",
		Identity:     &config.Identity{Provider: "gcp", Account: "person@example.com"},
	}
	_, err := Validate(context.Background(), resolved, append(os.Environ(), "PATH="+bin), nil)
	var validationError *Error
	if err == nil || !errors.As(err, &validationError) || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("error = %v", err)
	}
	if validationError.Check != CheckIdentity || validationError.Kind != FailureIdentityMismatch {
		t.Fatalf("typed error = %#v", validationError)
	}
}

func TestClassifiedErrorSupportsErrorsAsAndIs(t *testing.T) {
	cause := context.DeadlineExceeded
	err := classifiedError(CheckDocker, `Docker context "desktop"`, "reachability", cause)
	var validationError *Error
	if !errors.As(err, &validationError) {
		t.Fatalf("errors.As(%v) = false", err)
	}
	if validationError.Check != CheckDocker || validationError.Kind != FailureTimeout || validationError.Operation != "reachability" {
		t.Fatalf("typed error = %#v", validationError)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(%v, DeadlineExceeded) = false", err)
	}
}

func TestValidateClassifiesMissingTool(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	resolved := resolver.Resolved{Docker: &config.Docker{Context: "desktop-linux"}}
	_, err := Validate(context.Background(), resolved, append(os.Environ(), "PATH="+bin), nil)
	var validationError *Error
	if err == nil || !errors.As(err, &validationError) {
		t.Fatalf("error = %v", err)
	}
	if validationError.Check != CheckDocker || validationError.Kind != FailureMissingTool {
		t.Fatalf("typed error = %#v", validationError)
	}
}

func TestValidateRejectsTamperedStagedControlPlaneBeforeAPIRequest(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "kubectl.log")
	t.Setenv("PATH", bin)
	t.Setenv("KUBECTL_LOG", log)
	writeCommand(t, bin, "kubectl", `printf called >> "$KUBECTL_LOG"; printf 'namespace/default\n'`)
	environment := managedKubeEnvironment(t, "https://expected.example", validationCertificate(t, 1))
	kubeconfig := environmentValue(environment, "KUBECONFIG")
	writeValidationKubeconfig(t, kubeconfig, "https://other.example", validationCertificate(t, 1))
	resolved := resolver.Resolved{KubernetesName: "work", Kubernetes: &config.Kubernetes{Type: "kubeconfig", Namespace: "default"}}
	_, err := Validate(context.Background(), resolved, environment, nil)
	var validationError *Error
	if err == nil || !errors.As(err, &validationError) || validationError.Check != CheckFingerprint || validationError.Kind != FailureTargetMismatch {
		t.Fatalf("error = %#v (%v)", validationError, err)
	}
	if _, statErr := os.Stat(log); !os.IsNotExist(statErr) {
		t.Fatalf("Kubernetes API was contacted before fingerprint validation: %v", statErr)
	}
}

func TestValidateGKEControlPlaneAgainstProviderTruth(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "gcloud.log")
	t.Setenv("PATH", bin)
	t.Setenv("GCLOUD_LOG", log)
	ca := validationCertificate(t, 1)
	providerJSON, err := json.Marshal(map[string]any{
		"name": "cluster-one", "location": "us-east1",
		"selfLink": "/projects/project-one/locations/us-east1/clusters/cluster-one",
		"endpoint": "api.example", "masterAuth": map[string]string{"clusterCaCertificate": base64.StdEncoding.EncodeToString(ca)},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeCommand(t, bin, "gcloud", `printf '%s\n' "$*" >> "$GCLOUD_LOG"; printf '%s\n' "$GKE_RESPONSE"`)
	writeCommand(t, bin, "kubectl", `printf 'namespace/default\n'`)
	environment := managedKubeEnvironment(t, "https://api.example", ca)
	environment = append(environment, "PATH="+bin, "GKE_RESPONSE="+string(providerJSON), "CLOUDSDK_CORE_ACCOUNT=person@example.com", "CLOUDSDK_CORE_PROJECT=project-one")
	resolved := resolver.Resolved{
		Identity:       &config.Identity{Provider: "gcp", Account: "person@example.com"},
		Project:        &config.Project{Provider: "gcp", ProjectID: "project-one"},
		KubernetesName: "work", Kubernetes: &config.Kubernetes{Type: "gke", Cluster: "cluster-one", Location: "us-east1", Namespace: "default"},
	}
	if _, err := Validate(context.Background(), resolved, environment, nil); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "container clusters describe cluster-one --project project-one --location us-east1 --account person@example.com") || strings.Contains(string(contents), "projects describe") {
		t.Fatalf("gcloud calls = %q", contents)
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(context.Background(), resolved, environment, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("cached target repeated gcloud validation: %v", err)
	}
}

func TestValidateRejectsGKEProviderCAMismatch(t *testing.T) {
	bin := t.TempDir()
	kubectlLog := filepath.Join(t.TempDir(), "kubectl.log")
	t.Setenv("PATH", bin)
	t.Setenv("KUBECTL_LOG", kubectlLog)
	stagedCA := validationCertificate(t, 1)
	providerCA := validationCertificate(t, 2)
	providerJSON, _ := json.Marshal(map[string]any{
		"name": "cluster-one", "location": "us-east1", "endpoint": "api.example",
		"masterAuth": map[string]string{"clusterCaCertificate": base64.StdEncoding.EncodeToString(providerCA)},
	})
	writeCommand(t, bin, "gcloud", `printf '%s\n' "$GKE_RESPONSE"`)
	writeCommand(t, bin, "kubectl", `printf called >> "$KUBECTL_LOG"; printf 'namespace/default\n'`)
	environment := managedKubeEnvironment(t, "https://api.example", stagedCA)
	environment = append(environment, "PATH="+bin, "GKE_RESPONSE="+string(providerJSON), "CLOUDSDK_CORE_ACCOUNT=person@example.com", "CLOUDSDK_CORE_PROJECT=project-one")
	resolved := resolver.Resolved{
		Identity:       &config.Identity{Provider: "gcp", Account: "person@example.com"},
		Project:        &config.Project{Provider: "gcp", ProjectID: "project-one"},
		KubernetesName: "work", Kubernetes: &config.Kubernetes{Type: "gke", Cluster: "cluster-one", Location: "us-east1", Namespace: "default"},
	}
	_, err := Validate(context.Background(), resolved, environment, nil)
	var validationError *Error
	if err == nil || !errors.As(err, &validationError) || validationError.Check != CheckFingerprint || validationError.Kind != FailureTargetMismatch {
		t.Fatalf("error = %#v (%v)", validationError, err)
	}
	if _, statErr := os.Stat(kubectlLog); !os.IsNotExist(statErr) {
		t.Fatalf("Kubernetes API was contacted for mismatched provider truth: %v", statErr)
	}
}

func TestGKEProviderEndpointCandidatesIncludePublicPrivateAndDNS(t *testing.T) {
	var provider gkeCluster
	provider.Endpoint = "public.example"
	provider.PrivateClusterConfig.PrivateEndpoint = "private.example"
	provider.ControlPlaneEndpointsConfig.DNSEndpointConfig.Endpoint = "dns.example"
	provider.ControlPlaneEndpointsConfig.IPEndpointsConfig.PublicEndpoint = "new-public.example"
	provider.ControlPlaneEndpointsConfig.IPEndpointsConfig.PrivateEndpoint = "new-private.example"
	ca := []byte("cluster-ca")
	got := gkeProviderEndpoints(provider, ca)
	var endpoints []string
	for _, endpoint := range got {
		endpoints = append(endpoints, endpoint.Endpoint)
	}
	if strings.Join(endpoints, ",") != "public.example,private.example,dns.example,new-public.example,new-private.example" {
		t.Fatalf("endpoints = %v", endpoints)
	}
	if len(got[0].CA) == 0 || len(got[1].CA) == 0 || len(got[2].CA) != 0 || len(got[3].CA) == 0 || len(got[4].CA) == 0 {
		t.Fatalf("endpoint trust = %#v", got)
	}
}

func TestValidateGKEDNSEndpointUsesSystemRoots(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	clusterCA := validationCertificate(t, 1)
	providerJSON, _ := json.Marshal(map[string]any{
		"name": "cluster-one", "location": "us-east1",
		"masterAuth":                  map[string]string{"clusterCaCertificate": base64.StdEncoding.EncodeToString(clusterCA)},
		"controlPlaneEndpointsConfig": map[string]any{"dnsEndpointConfig": map[string]string{"endpoint": "dns.gke.goog"}},
	})
	writeCommand(t, bin, "gcloud", `printf '%s\n' "$GKE_RESPONSE"`)
	writeCommand(t, bin, "kubectl", `printf 'namespace/default\n'`)
	environment := managedKubeEnvironment(t, "https://dns.gke.goog", nil)
	environment = append(environment, "PATH="+bin, "GKE_RESPONSE="+string(providerJSON), "CLOUDSDK_CORE_ACCOUNT=person@example.com", "CLOUDSDK_CORE_PROJECT=project-one")
	resolved := resolver.Resolved{
		Identity:       &config.Identity{Provider: "gcp", Account: "person@example.com"},
		Project:        &config.Project{Provider: "gcp", ProjectID: "project-one"},
		KubernetesName: "work", Kubernetes: &config.Kubernetes{Type: "gke", Cluster: "cluster-one", Location: "us-east1", Namespace: "default"},
	}
	if _, err := Validate(context.Background(), resolved, environment, nil); err != nil {
		t.Fatal(err)
	}
}

func TestValidateClassifiesDeletedGKEClusterWithoutCallingKubernetes(t *testing.T) {
	bin := t.TempDir()
	kubectlLog := filepath.Join(t.TempDir(), "kubectl.log")
	t.Setenv("PATH", bin)
	t.Setenv("KUBECTL_LOG", kubectlLog)
	ca := validationCertificate(t, 1)
	writeCommand(t, bin, "gcloud", `printf 'NOT_FOUND: cluster was not found\n' >&2; exit 1`)
	writeCommand(t, bin, "kubectl", `printf called >> "$KUBECTL_LOG"; printf 'namespace/default\n'`)
	environment := managedKubeEnvironment(t, "https://api.example", ca)
	environment = append(environment, "PATH="+bin, "CLOUDSDK_CORE_ACCOUNT=person@example.com", "CLOUDSDK_CORE_PROJECT=project-one")
	resolved := resolver.Resolved{
		Identity:       &config.Identity{Provider: "gcp", Account: "person@example.com"},
		Project:        &config.Project{Provider: "gcp", ProjectID: "project-one"},
		KubernetesName: "work", Kubernetes: &config.Kubernetes{Type: "gke", Cluster: "cluster-one", Location: "us-east1", Namespace: "default"},
	}
	_, err := Validate(context.Background(), resolved, environment, nil)
	var validationError *Error
	if err == nil || !errors.As(err, &validationError) || validationError.Check != CheckFingerprint || validationError.Kind != FailureNotFound {
		t.Fatalf("error = %#v (%v)", validationError, err)
	}
	if _, statErr := os.Stat(kubectlLog); !os.IsNotExist(statErr) {
		t.Fatalf("Kubernetes API was contacted after provider reported deletion: %v", statErr)
	}
}

func managedKubeEnvironment(t *testing.T, server string, ca []byte) []string {
	t.Helper()
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	directory := t.TempDir()
	kubeconfig := filepath.Join(directory, "kubeconfig")
	writeValidationKubeconfig(t, kubeconfig, server, ca)
	fingerprint, err := kubetarget.FingerprintFile(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(directory, "session.json")
	manifest := state.Manifest{
		Version: 1, SessionID: "test-session", KubernetesControlPlaneFingerprint: fingerprint.Digest,
		KubernetesSourceRevision: "source-revision",
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return append(os.Environ(), "KUBECONFIG="+kubeconfig, state.SessionFileEnv+"="+manifestPath)
}

func writeValidationKubeconfig(t *testing.T, path, server string, ca []byte) {
	t.Helper()
	contents := "apiVersion: v1\nkind: Config\ncurrent-context: work\ncontexts:\n- name: work\n  context:\n    cluster: cluster\nclusters:\n- name: cluster\n  cluster:\n    server: " + server + "\n    certificate-authority-data: " + base64.StdEncoding.EncodeToString(ca) + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func validationCertificate(t *testing.T, serial int64) []byte {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = byte(serial)
	privateKey := ed25519.NewKeyFromSeed(seed)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Unix(4102444800, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(strings.NewReader(strings.Repeat("x", 64)), template, template, privateKey.Public(), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func writeCommand(t *testing.T, directory, name, body string) {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}
