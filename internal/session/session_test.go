package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/state"
)

func TestPrepareDockerOnlyIsFailClosed(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("CONTEXTHOP_CACHE_DIR", cache)
	resolved := resolver.Resolved{
		Name:       "local",
		DockerName: "orbstack",
		Docker:     &config.Docker{Context: "orbstack"},
	}
	prepared, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()

	environment := environmentMap(prepared.Env)
	if environment["DOCKER_CONTEXT"] != "orbstack" {
		t.Fatalf("DOCKER_CONTEXT = %q", environment["DOCKER_CONTEXT"])
	}
	if environment["CLOUDSDK_CORE_PROJECT"] != "" {
		t.Fatalf("CLOUDSDK_CORE_PROJECT = %q", environment["CLOUDSDK_CORE_PROJECT"])
	}
	if !strings.Contains(environment["CLOUDSDK_CONFIG"], "gcloud-disabled") {
		t.Fatalf("CLOUDSDK_CONFIG = %q", environment["CLOUDSDK_CONFIG"])
	}
	manifest, err := state.LoadManifest(environment[state.SessionFileEnv])
	if err != nil {
		t.Fatal(err)
	}
	if manifest.DockerName != "orbstack" || manifest.Expected.Docker != "orbstack" || manifest.Expected.Project != "" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if _, err := os.Stat(filepath.Join(prepared.Directory, "kubeconfig")); err != nil {
		t.Fatalf("disabled kubeconfig: %v", err)
	}
}

func TestPrepareCLIOnlyGCPDisablesAmbientADCAndOverrides(t *testing.T) {
	cache := t.TempDir()
	credentialStore := t.TempDir()
	t.Setenv("CONTEXTHOP_CACHE_DIR", cache)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/wrong/global-adc.json")
	t.Setenv("CLOUDSDK_ACTIVE_CONFIG_NAME", "wrong")
	t.Setenv("CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE", "/wrong/credential.json")
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "dummy-token")
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN_FILE", "/wrong/token")
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "wrong@example.com")
	t.Setenv("CLOUDSDK_BILLING_QUOTA_PROJECT", "wrong-quota-project")
	t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "wrong-quota-project")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "wrong-project")
	t.Setenv("GCLOUD_PROJECT", "wrong-project")
	t.Setenv("DOCKER_HOST", "tcp://wrong.example:2376")
	t.Setenv("AWS_ACCESS_KEY_ID", "wrong")
	resolved := resolver.Resolved{
		Name: "work", IdentityName: "person",
		Identity: &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: credentialStore, ADC: "/identity/adc.json"},
		Project:  &config.Project{Provider: "gcp", ProjectID: "right-project"},
	}
	prepared, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	environment := environmentMap(prepared.Env)
	if !strings.HasSuffix(environment["GOOGLE_APPLICATION_CREDENTIALS"], "/disabled-google-credentials.json") {
		t.Fatalf("ADC was not disabled: %q", environment["GOOGLE_APPLICATION_CREDENTIALS"])
	}
	for _, key := range []string{
		"CLOUDSDK_ACTIVE_CONFIG_NAME", "CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE",
		"CLOUDSDK_AUTH_ACCESS_TOKEN", "CLOUDSDK_AUTH_ACCESS_TOKEN_FILE", "CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT",
		"CLOUDSDK_BILLING_QUOTA_PROJECT", "GOOGLE_CLOUD_QUOTA_PROJECT", "DOCKER_HOST", "AWS_ACCESS_KEY_ID",
	} {
		if _, exists := environment[key]; exists {
			t.Errorf("inherited %s was retained", key)
		}
	}
	if environment["GOOGLE_CLOUD_PROJECT"] != "right-project" || environment["GCLOUD_PROJECT"] != "right-project" {
		t.Fatalf("client-library project aliases = %q, %q", environment["GOOGLE_CLOUD_PROJECT"], environment["GCLOUD_PROJECT"])
	}
	manifest, err := state.LoadManifest(prepared.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ADCMode != "" {
		t.Fatalf("ADC mode = %q", manifest.ADCMode)
	}
}

func TestPrepareADCRequiredUsesIdentityCredential(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	credentialStore := t.TempDir()
	adc := filepath.Join(credentialStore, "selected-adc.json")
	resolved := resolver.Resolved{
		Name: "terraform", IdentityName: "person", ADCMode: "identity",
		Identity: &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: credentialStore, ADC: adc},
	}
	prepared, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	environment := environmentMap(prepared.Env)
	if environment["GOOGLE_APPLICATION_CREDENTIALS"] != adc {
		t.Fatalf("ADC = %q", environment["GOOGLE_APPLICATION_CREDENTIALS"])
	}
	manifest, err := state.LoadManifest(prepared.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ADCMode != "identity" || manifest.Expected.ADC != adc {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestSessionCleanupPreservesPersistentIdentityStore(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	credentialStore := t.TempDir()
	marker := filepath.Join(credentialStore, "login-marker")
	if err := os.WriteFile(marker, []byte("authenticated"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.Resolved{
		Name: "work", IdentityName: "person",
		Identity: &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: credentialStore},
	}
	prepared, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != "authenticated" {
		t.Fatalf("persistent identity store changed: %q, %v", contents, err)
	}
}

func TestPrepareReusesIdentityStoreAcrossProjects(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	credentialStore := t.TempDir()
	identity := &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: credentialStore}

	first, err := Prepare(context.Background(), resolver.Resolved{
		Name: "first", IdentityName: "person", Identity: identity,
		Project: &config.Project{Provider: "gcp", ProjectID: "project-one"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Prepare(context.Background(), resolver.Resolved{
		Name: "second", IdentityName: "person", Identity: identity,
		Project: &config.Project{Provider: "gcp", ProjectID: "project-two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	firstEnvironment := environmentMap(first.Env)
	secondEnvironment := environmentMap(second.Env)
	if firstEnvironment["CLOUDSDK_CONFIG"] != credentialStore || secondEnvironment["CLOUDSDK_CONFIG"] != credentialStore {
		t.Fatalf("credential stores = %q, %q", firstEnvironment["CLOUDSDK_CONFIG"], secondEnvironment["CLOUDSDK_CONFIG"])
	}
	if firstEnvironment["CLOUDSDK_CORE_PROJECT"] == secondEnvironment["CLOUDSDK_CORE_PROJECT"] {
		t.Fatalf("project selection was not session-local: %q", firstEnvironment["CLOUDSDK_CORE_PROJECT"])
	}
}

func TestConfigureZshReappliesManagedEnvironment(t *testing.T) {
	cache := t.TempDir()
	home := t.TempDir()
	t.Setenv("CONTEXTHOP_CACHE_DIR", cache)
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export KUBECONFIG=/wrong/config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.Resolved{Name: "local", DockerName: "orbstack", Docker: &config.Docker{Context: "orbstack"}}
	prepared, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := prepared.ConfigureShell("/bin/zsh", "/opt/homebrew/bin/chop"); err != nil {
		t.Fatal(err)
	}
	environment := environmentMap(prepared.Env)
	contents, err := os.ReadFile(filepath.Join(environment["ZDOTDIR"], ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	if !strings.Contains(text, "source '"+filepath.Join(home, ".zshrc")+"'") || !strings.Contains(text, "export KUBECONFIG=") {
		t.Fatalf("generated .zshrc = %q", text)
	}
	if strings.Index(text, "source ") > strings.Index(text, "export KUBECONFIG=") {
		t.Fatal("ContextHop environment was applied before user configuration")
	}
	for _, expected := range []string{"chop() {", "_chop_refresh_prompt()", "add-zsh-hook precmd _chop_refresh_prompt", "CONTEXTHOP_ACTIVATION_FILE", "command \"$CONTEXTHOP_BINARY\"", "export CONTEXTHOP_BINARY='/opt/homebrew/bin/chop'", "export CONTEXTHOP_PROMPT_PREFIX='[orbstack] '"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated shell missing %q: %q", expected, text)
		}
	}
	if !strings.Contains(text, "unset CLOUDSDK_ACTIVE_CONFIG_NAME") || !strings.Contains(text, "unset DOCKER_HOST") {
		t.Fatal("generated shell does not neutralize inherited provider overrides")
	}
}

func TestSessionLocalZshFunctionAppliesActivationInPlace(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fakeChop := filepath.Join(t.TempDir(), "chop")
	fakeScript := `#!/bin/sh
case "$1" in _shared-sync|_shell-baseline) exit 0;; esac
if [ "$1" = "_activate-session" ]; then exit 0; fi
if [ "$1" = "_prompt" ]; then printf '[%s] ' "$CONTEXTHOP_CONTEXT"; exit 0; fi
printf '%s\n' "export CONTEXTHOP_CONTEXT='next'" > "$CONTEXTHOP_ACTIVATION_FILE"
printf '%s\n' "export DOCKER_CONTEXT='desktop-linux'" >> "$CONTEXTHOP_ACTIVATION_FILE"
printf '%s\n' "export CONTEXTHOP_PROMPT_PREFIX='[next] '" >> "$CONTEXTHOP_ACTIVATION_FILE"
`
	if err := os.WriteFile(fakeChop, []byte(fakeScript), 0o700); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), resolver.Resolved{
		Name: "current", DockerName: "orbstack", Docker: &config.Docker{Context: "orbstack"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := prepared.ConfigureShell(zsh, fakeChop); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(zsh, "-f", "-c", `source "$ZDOTDIR/.zshrc"; chop k; print -r -- "$CONTEXTHOP_CONTEXT|$DOCKER_CONTEXT|$PROMPT"`)
	command.Env = prepared.Env
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run session-local chop: %v: %s", err, output)
	}
	if value := strings.TrimSpace(string(output)); !strings.HasPrefix(value, "next|desktop-linux|[next]") {
		t.Fatalf("switched environment = %q", output)
	}
}

func TestPromptPrefixIgnoresLegacyRisk(t *testing.T) {
	manifest := state.Manifest{
		Destination: "generated-name", KubernetesLabel: "Team-A-Dev", Risk: "production",
		Expected: state.Component{Kubernetes: "gke_project_region_cluster", Namespace: "default"},
	}
	if got := promptPrefix(manifest); got != "[Team-A-Dev|default] " {
		t.Fatalf("prompt prefix = %q", got)
	}
	manifest.Risk = "sandbox"
	if got := promptPrefix(manifest); got != "[Team-A-Dev|default] " {
		t.Fatalf("sandbox prompt prefix = %q", got)
	}
	manifest = state.Manifest{Destination: "local", Expected: state.Component{Docker: "orbstack"}}
	if got := promptPrefix(manifest); got != "[orbstack] " {
		t.Fatalf("Docker prompt prefix = %q", got)
	}
	colored := formatPromptPrefix(state.Manifest{
		KubernetesLabel: "Team-A-Dev", Expected: state.Component{Kubernetes: "native", Namespace: "default"},
	}, "payments", true)
	for _, expected := range []string{"%F{245}[%f", "%F{39}Team-A-Dev%f", "%F{245}|%f", "%F{75}payments%f", "%F{245}]%f"} {
		if !strings.Contains(colored, expected) {
			t.Errorf("colored prompt %q missing %q", colored, expected)
		}
	}
	unsafe := formatPromptPrefix(state.Manifest{KubernetesLabel: "$(touch /tmp/nope)%F{red}", Expected: state.Component{Kubernetes: "native"}}, "default", true)
	if strings.Contains(unsafe, "$(") || !strings.Contains(unsafe, "%%F{red}") {
		t.Fatalf("prompt substitution syntax was not escaped: %q", unsafe)
	}
}

func TestCurrentZshPromptPrefixTracksLocalNamespace(t *testing.T) {
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "session.json")
	manifest, err := json.Marshal(state.Manifest{
		Version: 1, SessionID: "session", KubernetesLabel: "Team-A-Dev",
		Expected: state.Component{Kubernetes: "native", Namespace: "default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	kubeconfig := filepath.Join(directory, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte(`apiVersion: v1
kind: Config
current-context: native
contexts:
- name: native
  context:
    cluster: cluster
    namespace: payments
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.SessionFileEnv, manifestPath)
	t.Setenv("KUBECONFIG", kubeconfig)
	t.Setenv("NO_COLOR", "1")
	if got := CurrentZshPromptPrefix(); got != "[Team-A-Dev|payments] " {
		t.Fatalf("live prompt prefix = %q", got)
	}
}

func TestDetectsGKEAuthenticationDependency(t *testing.T) {
	config := []byte("users:\n- user:\n    exec:\n      command: /opt/google/bin/gke-gcloud-auth-plugin\n")
	if !usesGKEAuthentication(config) {
		t.Fatal("GKE credential plugin was not detected")
	}
	if usesGKEAuthentication([]byte("users: []\n")) {
		t.Fatal("ordinary kubeconfig was detected as GKE")
	}
}

func TestPrepareRecordsKubernetesControlPlaneFingerprint(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	source := filepath.Join(t.TempDir(), "source.yaml")
	kubeconfig := `apiVersion: v1
kind: Config
current-context: work
contexts:
- name: work
  context:
    cluster: cluster
clusters:
- name: cluster
  cluster:
    server: https://api.example
    insecure-skip-tls-verify: true
`
	if err := os.WriteFile(source, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte("#!/bin/sh\nprintf '%s' \"$FAKE_KUBECONFIG\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_KUBECONFIG", kubeconfig)
	prepared, err := Prepare(context.Background(), resolver.Resolved{
		Name: "work", KubernetesName: "work",
		Kubernetes: &config.Kubernetes{Type: "kubeconfig", Kubeconfig: source, Context: "work", Cluster: "friendly-cluster"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	manifest, err := state.LoadManifest(prepared.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.KubernetesControlPlaneFingerprint == "" || manifest.KubernetesControlPlaneFingerprint != prepared.KubernetesControlPlaneFingerprint {
		t.Fatalf("fingerprints = session %q, manifest %q", prepared.KubernetesControlPlaneFingerprint, manifest.KubernetesControlPlaneFingerprint)
	}
	if manifest.KubernetesLabel != "friendly-cluster" {
		t.Fatalf("Kubernetes label = %q", manifest.KubernetesLabel)
	}
}

func TestPrepareReusesCachedImportedKubeconfig(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "kubectl.log")
	t.Setenv("PATH", bin)
	t.Setenv("KUBECTL_LOG", logPath)
	source := filepath.Join(t.TempDir(), "source.yaml")
	kubeconfig := `apiVersion: v1
kind: Config
current-context: work
contexts:
- name: work
  context:
    cluster: cluster
clusters:
- name: cluster
  cluster:
    server: https://api.example
    insecure-skip-tls-verify: true
`
	if err := os.WriteFile(source, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeKubectl := "#!/bin/sh\nprintf 'view\\n' >> \"$KUBECTL_LOG\"\nprintf '%s' \"$FAKE_KUBECONFIG\"\n"
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_KUBECONFIG", kubeconfig)
	resolved := resolver.Resolved{
		Name: "work", IdentityName: "person", ProjectName: "project", KubernetesName: "work",
		Identity:   &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: t.TempDir()},
		Project:    &config.Project{Provider: "gcp", ProjectID: "example-project"},
		Kubernetes: &config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-central1", Kubeconfig: source, Context: "work"},
	}

	first, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "view\n" {
		t.Fatalf("kubectl calls = %q, want one materialization", calls)
	}

	if err := os.WriteFile(source, []byte(kubeconfig+"\n# changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	calls, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "view\nview\n" {
		t.Fatalf("kubectl calls after source change = %q, want rematerialization", calls)
	}
}

func TestPrepareReusesFreshProviderKubeconfig(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gcloud.log")
	t.Setenv("PATH", bin)
	t.Setenv("GCLOUD_LOG", logPath)
	fakeGcloud := `#!/bin/sh
printf 'get-credentials\n' >> "$GCLOUD_LOG"
printf '%s' "$FAKE_KUBECONFIG" > "$KUBECONFIG"
`
	if err := os.WriteFile(filepath.Join(bin, "gcloud"), []byte(fakeGcloud), 0o700); err != nil {
		t.Fatal(err)
	}
	kubeconfig := `apiVersion: v1
kind: Config
current-context: generated
contexts:
- name: generated
  context:
    cluster: cluster
clusters:
- name: cluster
  cluster:
    server: https://api.example
    insecure-skip-tls-verify: true
`
	t.Setenv("FAKE_KUBECONFIG", kubeconfig)
	resolved := resolver.Resolved{
		Name: "work", IdentityName: "person", ProjectName: "project", KubernetesName: "cluster",
		Identity:   &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: t.TempDir()},
		Project:    &config.Project{Provider: "gcp", ProjectID: "example-project"},
		Kubernetes: &config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-central1"},
	}

	first, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !first.KubernetesProviderMaterialized {
		t.Fatal("fresh provider kubeconfig was not marked as provider-materialized")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.KubernetesProviderMaterialized {
		t.Fatal("cached provider kubeconfig was treated as freshly provider-materialized")
	}

	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "get-credentials\n" {
		t.Fatalf("gcloud calls = %q, want one credential materialization", calls)
	}

	target := *resolved.Kubernetes
	cachePath, err := kubeconfigCachePath(target, environmentMap(second.Env), "")
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-providerKubeconfigCacheTTL - time.Second)
	if err := os.Chtimes(cachePath, expired, expired); err != nil {
		t.Fatal(err)
	}
	third, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	if !third.KubernetesProviderMaterialized {
		t.Fatal("expired provider kubeconfig was not refreshed")
	}
	calls, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "get-credentials\nget-credentials\n" {
		t.Fatalf("gcloud calls after expiry = %q, want refresh", calls)
	}
}

func TestPreparePassesConfiguredGKEEndpointToGcloud(t *testing.T) {
	for _, test := range []struct {
		endpoint string
		want     string
	}{{"dns", "--dns-endpoint"}, {"internal-ip", "--internal-ip"}} {
		t.Run(test.endpoint, func(t *testing.T) {
			t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
			bin := t.TempDir()
			logPath := filepath.Join(t.TempDir(), "gcloud.log")
			t.Setenv("PATH", bin)
			t.Setenv("GCLOUD_LOG", logPath)
			fakeGcloud := `#!/bin/sh
printf '%s\n' "$*" > "$GCLOUD_LOG"
printf '%s' "$FAKE_KUBECONFIG" > "$KUBECONFIG"
`
			if err := os.WriteFile(filepath.Join(bin, "gcloud"), []byte(fakeGcloud), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FAKE_KUBECONFIG", `apiVersion: v1
kind: Config
current-context: generated
contexts:
- name: generated
  context:
    cluster: cluster
clusters:
- name: cluster
  cluster:
    server: https://api.example
    insecure-skip-tls-verify: true
`)
			resolved := resolver.Resolved{
				Name: "work", IdentityName: "person", ProjectName: "project", KubernetesName: "cluster",
				Identity:   &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: t.TempDir()},
				Project:    &config.Project{Provider: "gcp", ProjectID: "example-project"},
				Kubernetes: &config.Kubernetes{Type: "gke", Project: "project", Cluster: "cluster", Location: "us-central1", Endpoint: test.endpoint},
			}
			prepared, err := Prepare(context.Background(), resolved)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			arguments, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(arguments), test.want) {
				t.Fatalf("gcloud arguments = %q, want %s", arguments, test.want)
			}
		})
	}
}

func TestKubeconfigCacheRejectsCredentialMaterial(t *testing.T) {
	for name, document := range map[string]string{
		"token": `users:
- name: user
  user:
    token: secret
`,
		"client key": `users:
- name: user
  user:
    client-key-data: c2VjcmV0
`,
		"credential-bearing exec environment": `users:
- name: user
  user:
    exec:
      command: gke-gcloud-auth-plugin
      env:
      - name: TOKEN
        value: secret
`,
		"other exec plugin": `users:
- name: user
  user:
    exec:
      command: custom-auth
`,
	} {
		t.Run(name, func(t *testing.T) {
			if safeToPersistKubeconfig([]byte(document)) {
				t.Fatal("credential-bearing kubeconfig was accepted for persistent caching")
			}
		})
	}
	if !safeToPersistKubeconfig([]byte(`users:
- name: gke
  user:
    exec:
      command: /opt/google/bin/gke-gcloud-auth-plugin
`)) {
		t.Fatal("standard GKE exec plugin was rejected")
	}
}

func TestShellInitRemovesLegacyIntegration(t *testing.T) {
	script, err := ShellInit("zsh")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"add-zsh-hook -d precmd _chop_refresh_prompt", "unfunction _chop_refresh_prompt", "unfunction chop", "unset CONTEXTHOP_BINARY"} {
		if !strings.Contains(script, expected) {
			t.Fatalf("shell integration missing %q: %q", expected, script)
		}
	}
	for _, unwanted := range []string{"CONTEXTHOP_ACTIVATION_FILE", "_chop_refresh_prompt()", "add-zsh-hook precmd", "WarpTerminal", "chop()"} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("shell wrapper contains %q: %q", unwanted, script)
		}
	}
}

func TestWriteActivationUpdatesManagedEnvironmentAndPromptPrefix(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	prepared, err := Prepare(context.Background(), resolver.Resolved{
		Name: "next", DockerName: "orbstack", Docker: &config.Docker{Context: "orbstack"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	activation := filepath.Join(t.TempDir(), "activation.zsh")
	if err := prepared.WriteActivation(activation); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(activation)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"unset CLOUDSDK_AUTH_ACCESS_TOKEN\n", "export CONTEXTHOP_CONTEXT='next'", "export CONTEXTHOP_PROMPT_PREFIX='[orbstack] '", "export DOCKER_CONTEXT='orbstack'", "export CONTEXTHOP_SESSION_FILE="} {
		if !strings.Contains(text, expected) {
			t.Errorf("activation missing %q: %q", expected, text)
		}
	}
	if strings.Contains(text, "export PROMPT=") || strings.Contains(text, "precmd") {
		t.Fatalf("activation directly modifies prompt: %q", text)
	}
	stateData, err := os.ReadFile(filepath.Join(prepared.Directory, transactionStateFile))
	if err != nil || strings.TrimSpace(string(stateData)) != "committed" {
		t.Fatalf("transaction state = %q, %v", stateData, err)
	}
}

func TestCleanupManifestOnlyRemovesValidatedSession(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("CONTEXTHOP_CACHE_DIR", cache)
	resolved := resolver.Resolved{Name: "local", DockerName: "orbstack", Docker: &config.Docker{Context: "orbstack"}}
	prepared, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	if err := CleanupManifest(prepared.Manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared.Directory); !os.IsNotExist(err) {
		t.Fatalf("session directory still exists: %v", err)
	}
}

func TestCleanupShellSessionRemovesCurrentlyRegisteredManifest(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	prepared, err := Prepare(context.Background(), resolver.Resolved{
		Name: "local", DockerName: "orbstack", Docker: &config.Docker{Context: "orbstack"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := RegisterActive(os.Getpid(), prepared.Manifest); err != nil {
		t.Fatal(err)
	}
	active, err := ActiveForShell(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if active.Manifest != prepared.Manifest {
		t.Fatalf("active manifest = %q, want %q", active.Manifest, prepared.Manifest)
	}
	if err := CleanupShellSession(os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared.Directory); !os.IsNotExist(err) {
		t.Fatalf("session directory still exists: %v", err)
	}
}

func TestCleanupAbandonedStagingPreservesCommittedSessions(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("CONTEXTHOP_CACHE_DIR", cache)
	resolved := resolver.Resolved{Name: "local", DockerName: "orbstack", Docker: &config.Docker{Context: "orbstack"}}
	staged, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer committed.Close()
	if err := committed.Commit(); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, prepared := range []*Session{staged, committed} {
		marker := filepath.Join(prepared.Directory, transactionStateFile)
		if err := os.Chtimes(marker, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := CleanupAbandonedStaging(time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged.Directory); !os.IsNotExist(err) {
		t.Fatalf("abandoned staging directory still exists: %v", err)
	}
	if _, err := os.Stat(committed.Directory); err != nil {
		t.Fatalf("committed session was removed: %v", err)
	}
}

func TestActiveSessionCanBeReusedIndependently(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	source, err := Prepare(context.Background(), resolver.Resolved{
		Name: "local", DockerName: "orbstack", Docker: &config.Docker{Context: "orbstack"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := RegisterActive(os.Getpid(), source.Manifest); err != nil {
		t.Fatal(err)
	}
	records, err := ActiveSessions(0)
	if err != nil || len(records) != 1 {
		t.Fatalf("active sessions = %#v, %v", records, err)
	}
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "dummy-token")
	clone, err := Reuse(records[0])
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	if clone.Directory == source.Directory || clone.Manifest == source.Manifest {
		t.Fatal("reused session shares source paths")
	}
	cloneEnvironment := environmentMap(clone.Env)
	if _, exists := cloneEnvironment["CLOUDSDK_AUTH_ACCESS_TOKEN"]; exists {
		t.Fatal("reused session retained inline token")
	}
	if cloneEnvironment["DOCKER_CONTEXT"] != "orbstack" || cloneEnvironment["CONTEXTHOP_PROMPT_PREFIX"] != "[orbstack] " || !strings.HasPrefix(cloneEnvironment[state.SessionFileEnv], clone.Directory) {
		t.Fatalf("reused environment = %#v", cloneEnvironment)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(clone.Manifest); err != nil {
		t.Fatalf("closing source affected reused session: %v", err)
	}
}

func TestActiveSessionsPrunesDeadShell(t *testing.T) {
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	root, err := activeRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "dead.json")
	if err := writeJSONAtomic(path, Active{Key: "dead", ShellPID: 99999999, Manifest: "/missing"}); err != nil {
		t.Fatal(err)
	}
	records, err := ActiveSessions(0)
	if err != nil || len(records) != 0 {
		t.Fatalf("active sessions = %#v, %v", records, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("dead record was not pruned: %v", err)
	}
}

func TestOptInCurrentShellHookPreservesParentShellState(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh unavailable")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "chop")
	script := `#!/bin/sh
if [ "$1" = "_prompt" ]; then exit 0; fi
case "$1" in _shared-sync|_shell-baseline) exit 0;; esac
if [ "$1" = "_activate-session" ]; then exit 0; fi
printf '%s\n' "export DOCKER_CONTEXT='selected-context'" > "$CONTEXTHOP_ACTIVATION_FILE"
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	init := filepath.Join(dir, "init.zsh")
	if err := os.WriteFile(init, []byte(CurrentShellInit(binary)), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(zsh, "-f", "-c", `PROMPT='base> '; source "$1"; before=$$; original=$PWD; retained='keep'; chop; [[ $$ == $before && $PWD == $original && $retained == keep && $DOCKER_CONTEXT == selected-context ]]`, "_", init)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("current-shell integration: %v: %s", err, output)
	}
}

func TestPromptLabelsStayLiteralInZsh(t *testing.T) {
	for _, color := range []bool{false, true} {
		prefix := formatPromptPrefix(state.Manifest{
			WorkspaceName:   "$(printf WORKSPACE_EXECUTED)",
			KubernetesLabel: "`printf CLUSTER_EXECUTED`%n",
			Expected:        state.Component{Kubernetes: "review"},
		}, "$(printf NAMESPACE_EXECUTED)%n", color)
		command := exec.Command("zsh", "-f", "-c", `setopt promptsubst; PROMPT=$REVIEW_PREFIX; print -P -- "$PROMPT"`)
		command.Env = append(os.Environ(), "REVIEW_PREFIX="+prefix)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("render prompt: %v: %s", err, output)
		}
		for _, literal := range []string{"?(printf WORKSPACE_EXECUTED)", "?printf CLUSTER_EXECUTED?%n", "?(printf NAMESPACE_EXECUTED)%n"} {
			if !strings.Contains(string(output), literal) {
				t.Errorf("color=%v: prompt expanded label, output=%q; want %q", color, output, literal)
			}
		}
	}
}
