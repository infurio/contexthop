package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/debugtrace"
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/kubetarget"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/state"
	"gopkg.in/yaml.v3"
)

const disabledDockerContext = "contexthop-none"

const transactionStateFile = "transaction-state"

const environmentStateFile = "environment.json"

const kubeconfigCacheVersion = 1

// Provider-generated kubeconfigs have no local source revision to invalidate
// them. Refresh them periodically so endpoint or CA rotations are discovered;
// stale credentials fail closed because the cached CA remains pinned.
const providerKubeconfigCacheTTL = 30 * time.Minute

// ActivationFileEnv is set only by the temporary shell-local chop function.
// It tells the binary to return environment changes instead of opening a child.
const ActivationFileEnv = "CONTEXTHOP_ACTIVATION_FILE"

type Session struct {
	Directory                         string
	Manifest                          string
	Env                               []string
	KubernetesControlPlaneFingerprint string
	KubernetesProviderMaterialized    bool
}

func Prepare(ctx context.Context, resolved resolver.Resolved) (*Session, error) {
	cacheDir, err := sessionCacheDir()
	if err != nil {
		return nil, err
	}
	id, err := sessionID()
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(cacheDir, "contexthop", "sessions", id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}
	prepared := &Session{Directory: directory}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(directory)
		}
	}()
	if err := os.WriteFile(filepath.Join(directory, transactionStateFile), []byte("staging\n"), 0o600); err != nil {
		return nil, fmt.Errorf("mark staged session: %w", err)
	}

	environment := environmentMap(os.Environ())
	environment[ScopeEnv] = "local"
	environment[SharedRevisionEnv] = ""
	removeEnvironmentKeys(environment,
		"CLOUDSDK_ACTIVE_CONFIG_NAME",
		"CLOUDSDK_AUTH_ACCESS_TOKEN", "CLOUDSDK_AUTH_ACCESS_TOKEN_FILE",
		"CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE",
		"CLOUDSDK_AUTH_DISABLE_CREDENTIALS",
		"CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT",
		"CLOUDSDK_BILLING_QUOTA_PROJECT",
		"CLOUDSDK_CORE_QUOTA_PROJECT",
		"GOOGLE_CLOUD_PROJECT",
		"GOOGLE_CLOUD_QUOTA_PROJECT",
		"GCLOUD_PROJECT",
		"DOCKER_HOST",
		"DOCKER_TLS_VERIFY",
		"DOCKER_CERT_PATH",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AWS_DEFAULT_PROFILE",
		"AWS_ROLE_ARN",
		"AWS_WEB_IDENTITY_TOKEN_FILE",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_REGION",
		"AWS_DEFAULT_REGION",
	)
	environment["CONTEXTHOP_CONTEXT"] = resolved.Name
	environment["CONTEXTHOP_SESSION_ID"] = id
	environment["CONTEXTHOP_SESSION_DIR"] = directory

	expected := state.Component{}
	cloudSDKConfigValue := ""
	disabledADC := filepath.Join(directory, "disabled-google-credentials.json")
	environment["GOOGLE_APPLICATION_CREDENTIALS"] = disabledADC
	expected.ADC = disabledADC
	if resolved.Identity != nil {
		if resolved.Identity.Provider != "gcp" {
			return nil, fmt.Errorf("provider %q is not implemented", resolved.Identity.Provider)
		}
		cloudSDKConfig, err := resolver.ExpandPath(resolved.Identity.CloudSDKConfig)
		if err != nil {
			return nil, err
		}
		if cloudSDKConfig == "" {
			cloudSDKConfig = filepath.Join(directory, "gcloud")
		}
		if err := os.MkdirAll(cloudSDKConfig, 0o700); err != nil {
			return nil, fmt.Errorf("create isolated gcloud directory: %w", err)
		}
		environment["CLOUDSDK_CONFIG"] = cloudSDKConfig
		cloudSDKConfigValue = cloudSDKConfig
		environment["CLOUDSDK_CORE_ACCOUNT"] = resolved.Identity.Account
		expected.Provider = "gcp"
		expected.Identity = resolved.Identity.Account

		if resolved.ADCMode == "identity" {
			adc, err := resolver.ExpandPath(resolved.Identity.ADC)
			if err != nil {
				return nil, err
			}
			if adc == "" {
				adc = filepath.Join(cloudSDKConfig, "application_default_credentials.json")
			}
			environment["GOOGLE_APPLICATION_CREDENTIALS"] = adc
			expected.ADC = adc
		}
	} else {
		disabledGcloud := filepath.Join(directory, "gcloud-disabled")
		if err := os.MkdirAll(disabledGcloud, 0o700); err != nil {
			return nil, err
		}
		environment["CLOUDSDK_CONFIG"] = disabledGcloud
		environment["CLOUDSDK_CORE_ACCOUNT"] = ""
	}

	if resolved.Project != nil {
		environment["CLOUDSDK_CORE_PROJECT"] = resolved.Project.ProjectID
		environment["GOOGLE_CLOUD_PROJECT"] = resolved.Project.ProjectID
		environment["GCLOUD_PROJECT"] = resolved.Project.ProjectID
		expected.Project = resolved.Project.ProjectID
	} else {
		environment["CLOUDSDK_CORE_PROJECT"] = ""
	}

	kubeconfig := filepath.Join(directory, "kubeconfig")
	kubernetesTargetIdentity := ""
	kubernetesSourceRevision := ""
	kubernetesControlPlaneFingerprint := ""
	kubernetesLabel := ""
	if resolved.Kubernetes != nil {
		kubernetesSourceRevision, err = discovery.KubernetesSourceRevision(*resolved.Kubernetes)
		if err != nil {
			return nil, fmt.Errorf("record kubeconfig revision: %w", err)
		}
		providerMaterialized, err := materializeKubeconfig(ctx, kubeconfig, *resolved.Kubernetes, environment, kubernetesSourceRevision)
		if err != nil {
			return nil, err
		}
		prepared.KubernetesProviderMaterialized = providerMaterialized
		expected.Kubernetes = resolved.Kubernetes.Context
		if expected.Kubernetes == "" {
			expected.Kubernetes = resolved.KubernetesName
		}
		kubernetesLabel = resolved.Kubernetes.Cluster
		if kubernetesLabel == "" {
			kubernetesLabel = expected.Kubernetes
		}
		expected.Namespace, err = materializedKubernetesNamespace(kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("read staged Kubernetes namespace: %w", err)
		}
		kubernetesTargetIdentity = resolver.KubernetesTargetIdentity(*resolved.Kubernetes, resolved.Project)
		fingerprint, err := kubetarget.FingerprintFile(kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("identify staged Kubernetes control plane: %w", err)
		}
		kubernetesControlPlaneFingerprint = fingerprint.Digest
		prepared.KubernetesControlPlaneFingerprint = fingerprint.Digest
	} else if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\ncurrent-context: \"\"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("create disabled kubeconfig: %w", err)
	}
	environment["KUBECONFIG"] = kubeconfig

	if resolved.Docker != nil {
		environment["DOCKER_CONTEXT"] = resolved.Docker.Context
		expected.Docker = resolved.Docker.Context
	} else {
		environment["DOCKER_CONTEXT"] = disabledDockerContext
		expected.Docker = disabledDockerContext
	}

	// Prevent an unrelated inherited AWS profile from remaining usable.
	environment["AWS_PROFILE"] = "contexthop-none"
	environment["AWS_CONFIG_FILE"] = filepath.Join(directory, "aws-disabled-config")
	environment["AWS_SHARED_CREDENTIALS_FILE"] = filepath.Join(directory, "aws-disabled-credentials")

	manifest := state.Manifest{
		Production:                        resolved.Production,
		PromptColors:                      resolved.PromptColors,
		Version:                           1,
		SessionID:                         id,
		Destination:                       resolved.Name,
		WorkspaceName:                     resolved.WorkspaceName,
		IdentityName:                      resolved.IdentityName,
		ProjectName:                       resolved.ProjectName,
		KubernetesName:                    resolved.KubernetesName,
		DockerName:                        resolved.DockerName,
		KubernetesLabel:                   kubernetesLabel,
		Expected:                          expected,
		CloudSDKConfig:                    cloudSDKConfigValue,
		KubernetesTargetIdentity:          kubernetesTargetIdentity,
		KubernetesSourceRevision:          kubernetesSourceRevision,
		KubernetesControlPlaneFingerprint: kubernetesControlPlaneFingerprint,
		ADCMode:                           resolved.ADCMode,
	}
	environment["CONTEXTHOP_PROMPT_PREFIX"] = promptPrefix(manifest)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	prepared.Manifest = filepath.Join(directory, "session.json")
	if err := os.WriteFile(prepared.Manifest, manifestData, 0o600); err != nil {
		return nil, fmt.Errorf("write session manifest: %w", err)
	}
	environment[state.SessionFileEnv] = prepared.Manifest
	environmentData, err := json.MarshalIndent(managedEnvironment(environment), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode session environment: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, environmentStateFile), environmentData, 0o600); err != nil {
		return nil, fmt.Errorf("write session environment: %w", err)
	}
	prepared.Env = flattenEnvironment(environment)
	failed = false
	return prepared, nil
}

func (s *Session) Close() error {
	return os.RemoveAll(s.Directory)
}

func (s *Session) Commit() error {
	if err := os.WriteFile(filepath.Join(s.Directory, transactionStateFile), []byte("committed\n"), 0o600); err != nil {
		return fmt.Errorf("commit prepared session: %w", err)
	}
	return nil
}

func (s *Session) ConfigureShell(shell, binary string) error {
	if filepath.Base(shell) != "zsh" {
		return nil
	}
	environment := environmentMap(s.Env)
	originalZDOTDIR := environment["CONTEXTHOP_ORIGINAL_ZDOTDIR"]
	if originalZDOTDIR == "" {
		originalZDOTDIR = environment["ZDOTDIR"]
	}
	if originalZDOTDIR == "" {
		originalZDOTDIR = environment["HOME"]
	}
	environment["CONTEXTHOP_ORIGINAL_ZDOTDIR"] = originalZDOTDIR
	environment["CONTEXTHOP_BINARY"] = binary
	environment["CONTEXTHOP_IN_PLACE_SWITCH"] = "1"
	environment["CONTEXTHOP_ROOT_SESSION_FILE"] = s.Manifest
	wrapperDir := filepath.Join(s.Directory, "zsh")
	if err := os.MkdirAll(wrapperDir, 0o700); err != nil {
		return fmt.Errorf("prepare isolated Zsh shell: %w", err)
	}

	var exports strings.Builder
	for _, key := range managedEnvironmentKeys() {
		writeShellBinding(&exports, key, environment)
	}
	writeShellBinding(&exports, "CONTEXTHOP_BINARY", environment)
	writeShellBinding(&exports, "CONTEXTHOP_IN_PLACE_SWITCH", environment)
	writeShellBinding(&exports, "CONTEXTHOP_ROOT_SESSION_FILE", environment)
	exports.WriteString("export ZDOTDIR=")
	exports.WriteString(shellQuote(wrapperDir))
	exports.WriteByte('\n')

	for _, startup := range []string{".zshenv", ".zprofile", ".zshrc", ".zlogin", ".zlogout"} {
		var script strings.Builder
		original := filepath.Join(originalZDOTDIR, startup)
		script.WriteString("[[ -r ")
		script.WriteString(shellQuote(original))
		script.WriteString(" ]] && source ")
		script.WriteString(shellQuote(original))
		script.WriteByte('\n')
		if startup != ".zlogout" {
			script.WriteString(exports.String())
		}
		if startup == ".zshrc" {
			script.WriteString(zshFunction())
			script.WriteString(ZshCompletion())
		}
		if err := os.WriteFile(filepath.Join(wrapperDir, startup), []byte(script.String()), 0o600); err != nil {
			return fmt.Errorf("prepare isolated Zsh startup: %w", err)
		}
	}
	environment["ZDOTDIR"] = wrapperDir
	s.Env = flattenEnvironment(environment)
	return nil
}

// WriteActivation writes shell assignments for a locally verified session. The
// session-local chop function sources them into the existing isolated shell.
func (s *Session) WriteActivation(path string) error {
	if err := s.recordPrevious(); err != nil {
		return err
	}
	environment := environmentMap(s.Env)
	var script strings.Builder
	for _, key := range managedEnvironmentKeys() {
		writeShellBinding(&script, key, environment)
	}
	if err := os.WriteFile(path, []byte(script.String()), 0o600); err != nil {
		return fmt.Errorf("write shell activation: %w", err)
	}
	return s.Commit()
}

func zshFunction() string {
	return `_chop_cleanup_owned_session() {
  if [[ -n "${_chop_owned_session:-}" ]]; then
    command "$CONTEXTHOP_BINARY" _cleanup-session "$_chop_owned_session" >/dev/null 2>&1
  fi
}
_chop_restore_baseline() {
  _chop_cleanup_owned_session
  unset _chop_owned_session
  [[ -n "${_chop_baseline:-}" ]] && eval "$_chop_baseline"
  export CONTEXTHOP_SCOPE=shared
  unset CONTEXTHOP_SHARED_REVISION
}
_chop_sync_default() {
  [[ "${CONTEXTHOP_SCOPE:-shared}" == local ]] && return 0
  local _chop_update="$(command mktemp "${TMPDIR:-/tmp}/contexthop-follow.XXXXXXXX")" || return 1
  local _chop_previous="$CONTEXTHOP_SESSION_FILE"
  command "$CONTEXTHOP_BINARY" _shared-sync "$_chop_update"
  local _chop_result=$?
  if (( _chop_result == 0 )) && [[ -s "$_chop_update" ]]; then
    source "$_chop_update" || _chop_result=$?
    if (( _chop_result == 0 )); then
      command "$CONTEXTHOP_BINARY" _activate-session "$CONTEXTHOP_SESSION_FILE" "$_chop_previous" >/dev/null 2>&1
      if [[ "$CONTEXTHOP_SESSION_FILE" != "$_chop_previous" ]]; then _chop_owned_session="$CONTEXTHOP_SESSION_FILE"; fi
    fi
  fi
  command rm -f -- "$_chop_update"
  return $_chop_result
}
if [[ -z "${_chop_baseline+x}" ]]; then
  typeset -g _chop_baseline="$(command "$CONTEXTHOP_BINARY" _shell-baseline)"
  if [[ "${CONTEXTHOP_SCOPE:-shared}" != local ]]; then
    unset CONTEXTHOP_SHARED_REVISION
  fi
fi
_chop_refresh_prompt() {
  _chop_sync_default

  local _chop_live_prefix
  if ! _chop_live_prefix="$(command "$CONTEXTHOP_BINARY" _prompt 2>/dev/null)"; then
    _chop_live_prefix="$CONTEXTHOP_PROMPT_PREFIX"
  fi
  local _chop_old_fragment="${_chop_prompt_fragment-${CONTEXTHOP_APPLIED_PROMPT_PREFIX:-}}"
  local _chop_new_fragment="$_chop_live_prefix"
  # A stable reference lets themes and terminals retain their prompt snapshots
  # even when the context changes. Do not enable substitution on the user's
  # behalf: that could execute previously literal text in their prompt.
  if [[ -o promptsubst && ( -n "$_chop_live_prefix" || "$_chop_old_fragment" == '${CONTEXTHOP_APPLIED_PROMPT_PREFIX}' ) ]]; then
    _chop_new_fragment='${CONTEXTHOP_APPLIED_PROMPT_PREFIX}'
  fi

  # Without substitution, changing the label requires a literal prompt edit.
  # Ghostty exposes a clean snapshot; only use it when its hook is installed
  # and no other theme/integration has changed the marked prompt since then.
  if [[ "$_chop_old_fragment" != "$_chop_new_fragment" ]] &&
      (( $+functions[_ghostty_precmd] )) &&
      [[ -n ${_ghostty_saved_ps1+x} && -n ${_ghostty_marked_ps1+x} && "$PROMPT" == "$_ghostty_marked_ps1" ]]; then
    PROMPT="$_ghostty_saved_ps1"
  fi
  if [[ -n "$_chop_old_fragment" && "$PROMPT" == *"$_chop_old_fragment"* ]]; then
    if [[ "$_chop_old_fragment" != "$_chop_new_fragment" ]]; then
      PROMPT="${PROMPT/"$_chop_old_fragment"/"$_chop_new_fragment"}"
    fi
  else
    # Keep leading nonprinting terminal markers ahead of a newly enabled label.
    local _chop_leading='' _chop_tail="$PROMPT" _chop_marker
    while [[ "$_chop_tail" == '%{'* && "$_chop_tail" == *'%}'* ]]; do
      _chop_marker="${_chop_tail%%\%\}*}%}"
      _chop_leading+="$_chop_marker"
      _chop_tail="${_chop_tail#"$_chop_marker"}"
    done
    PROMPT="${_chop_leading}${_chop_new_fragment}${_chop_tail}"
  fi
  _chop_prompt_fragment="$_chop_new_fragment"
  CONTEXTHOP_APPLIED_PROMPT_PREFIX="$_chop_live_prefix"
  CONTEXTHOP_BASE_PROMPT="${PROMPT/"$_chop_prompt_fragment"/}"
}

autoload -Uz add-zsh-hook
add-zsh-hook precmd _chop_refresh_prompt
add-zsh-hook preexec _chop_sync_default
add-zsh-hook zshexit _chop_cleanup_owned_session
_chop_refresh_prompt

chop() {
  local _chop_activation="$(command mktemp "${TMPDIR:-/tmp}/contexthop-activation.XXXXXXXX")" || return 1
  local _chop_previous="$CONTEXTHOP_SESSION_FILE"
  : >| "$_chop_activation" || return 1
  CONTEXTHOP_ACTIVATION_FILE="$_chop_activation" command "$CONTEXTHOP_BINARY" "$@"
  local _chop_status=$?
  if (( _chop_status == 0 )) && [[ -s "$_chop_activation" ]]; then
    source "$_chop_activation" || _chop_status=$?
    if (( _chop_status == 0 )); then
      _chop_refresh_prompt
      command "$CONTEXTHOP_BINARY" _activate-session "$CONTEXTHOP_SESSION_FILE" "$_chop_previous" >/dev/null 2>&1
      if [[ "$CONTEXTHOP_SESSION_FILE" != "$_chop_previous" ]]; then _chop_owned_session="$CONTEXTHOP_SESSION_FILE"; fi
    fi
  fi
  command rm -f -- "$_chop_activation"
  if (( _chop_status == 0 )); then _chop_refresh_prompt; fi
  return $_chop_status
}
`
}

// Capture the context being left only in the newly prepared session. A failed
// activation leaves both the active context and its previous selection intact.
func (s *Session) recordPrevious() error {
	manifest, err := state.LoadManifest(s.Manifest)
	if err != nil {
		return err
	}
	if current, err := state.LoadManifest(os.Getenv(state.SessionFileEnv)); err == nil {
		previous := current.Selection()
		if _, namespace := state.KubernetesState(); namespace != "" {
			previous.Namespace = namespace
		}
		if previous == manifest.Selection() {
			manifest.Previous = current.Previous
		} else {
			manifest.Previous = &previous
		}
	} else {
		manifest.Previous = nil
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Manifest, data, 0o600)
}

func promptPrefix(manifest state.Manifest) string {
	return formatPromptPrefix(manifest, manifest.Expected.Namespace, false)
}

// CurrentZshPromptPrefix renders the prompt prefix from session-local files.
// Reading the namespace this way keeps precmd updates local and never invokes
// kubectl, its credential plugin, or the Kubernetes API.
func CurrentZshPromptPrefix() string {
	manifest, err := state.LoadManifest(os.Getenv(state.SessionFileEnv))
	if err != nil {
		return os.Getenv("CONTEXTHOP_PROMPT_PREFIX")
	}
	_, namespace := state.KubernetesState()
	if namespace == "" {
		namespace = manifest.Expected.Namespace
	}
	color := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	return formatPromptPrefix(manifest, namespace, color)
}

func formatPromptPrefix(manifest state.Manifest, namespace string, color bool) string {
	label := manifest.KubernetesLabel
	kind := "kubernetes"
	if label == "" {
		label = manifest.Expected.Kubernetes
	}
	hasKubernetes := label != ""
	if label == "" && manifest.Expected.Docker != "" && manifest.Expected.Docker != disabledDockerContext {
		label, kind = manifest.Expected.Docker, "docker"
	}
	if label == "" {
		for _, candidate := range []struct{ value, kind string }{
			{manifest.Expected.Project, "project"}, {manifest.Expected.Identity, "identity"},
			{manifest.Destination, "workspace"}, {"contexthop", ""},
		} {
			if candidate.value != "" {
				label, kind = candidate.value, candidate.kind
				break
			}
		}
	}
	type segment struct{ text, color string }
	itemColor := func(kind string) string {
		if value := manifest.PromptColors[kind]; config.ValidTagColor(value) {
			return value
		}
		return "39"
	}
	parts := []segment{{label, itemColor(kind)}}
	if manifest.WorkspaceName != "" && manifest.WorkspaceName != label {
		parts = append([]segment{{manifest.WorkspaceName, itemColor("workspace")}}, parts...)
	}
	if manifest.Production {
		parts = append([]segment{{"PROD", parts[0].color}}, parts...)
	}
	if hasKubernetes && namespace != "" {
		parts = append(parts, segment{namespace, "75"})
	}
	var prefix strings.Builder
	if color {
		prefix.WriteString("%F{245}[%f")
	} else {
		prefix.WriteByte('[')
	}
	for i, part := range parts {
		if i > 0 {
			if color {
				prefix.WriteString("%F{245}|%f")
			} else {
				prefix.WriteByte('|')
			}
		}
		if color {
			prefix.WriteString("%F{" + part.color + "}")
		}
		prefix.WriteString(escapeZshPrompt(part.text))
		if color {
			prefix.WriteString("%f")
		}
	}
	if color {
		prefix.WriteString("%F{245}]%f ")
	} else {
		prefix.WriteString("] ")
	}
	return prefix.String()
}

func escapeZshPrompt(value string) string {
	// PROMPT_SUBST may be enabled by a user's theme. Neutralize substitution
	// syntax as well as Zsh's native percent escapes before embedding labels.
	value = strings.NewReplacer("$", "?", "`", "?", "\\", "?", "\r", "?", "\n", "?").Replace(value)
	return strings.ReplaceAll(value, "%", "%%")
}

func CleanupAbandonedStaging(olderThan time.Duration) error {
	cacheDir, err := sessionCacheDir()
	if err != nil {
		return err
	}
	root := filepath.Join(cacheDir, "contexthop", "sessions")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		marker := filepath.Join(directory, transactionStateFile)
		contents, err := os.ReadFile(marker)
		if err != nil || strings.TrimSpace(string(contents)) != "staging" {
			continue
		}
		info, err := os.Stat(marker)
		if err != nil || now.Sub(info.ModTime()) < olderThan {
			continue
		}
		if err := os.RemoveAll(directory); err != nil {
			return fmt.Errorf("remove abandoned staged session %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func CleanupManifest(path string) error {
	manifest, err := state.LoadManifest(path)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if filepath.Base(path) != "session.json" || filepath.Base(directory) != manifest.SessionID {
		return fmt.Errorf("refusing to clean invalid session path")
	}
	cacheDir, err := sessionCacheDir()
	if err != nil {
		return err
	}
	root := filepath.Join(cacheDir, "contexthop", "sessions")
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || strings.Contains(relative, string(os.PathSeparator)) {
		return fmt.Errorf("refusing to clean session outside cache")
	}
	_ = removeActiveManifest(path)
	return os.RemoveAll(directory)
}

func managedEnvironment(environment map[string]string) map[string]string {
	result := make(map[string]string)
	for _, key := range managedEnvironmentKeys() {
		if value, ok := environment[key]; ok {
			result[key] = value
		}
	}
	return result
}

func managedEnvironmentKeys() []string {
	keys := []string{
		"AWS_ACCESS_KEY_ID", "AWS_CONFIG_FILE", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_DEFAULT_PROFILE", "AWS_DEFAULT_REGION", "AWS_PROFILE", "AWS_REGION", "AWS_ROLE_ARN", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"AWS_SHARED_CREDENTIALS_FILE", "AWS_WEB_IDENTITY_TOKEN_FILE",
		"CONTEXTHOP_CONTEXT", "CONTEXTHOP_ORIGINAL_ZDOTDIR", ScopeEnv, SharedRevisionEnv,
		"CONTEXTHOP_PROMPT_PREFIX",
		"CONTEXTHOP_SESSION_DIR", "CONTEXTHOP_SESSION_FILE", "CONTEXTHOP_SESSION_ID",
		"CLOUDSDK_ACTIVE_CONFIG_NAME", "CLOUDSDK_AUTH_ACCESS_TOKEN", "CLOUDSDK_AUTH_ACCESS_TOKEN_FILE", "CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE",
		"CLOUDSDK_AUTH_DISABLE_CREDENTIALS", "CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "CLOUDSDK_BILLING_QUOTA_PROJECT",
		"CLOUDSDK_CONFIG", "CLOUDSDK_CORE_ACCOUNT", "CLOUDSDK_CORE_PROJECT", "CLOUDSDK_CORE_QUOTA_PROJECT",
		"DOCKER_CERT_PATH", "DOCKER_CONTEXT", "DOCKER_HOST", "DOCKER_TLS_VERIFY", "GCLOUD_PROJECT",
		"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_QUOTA_PROJECT", "KUBECONFIG",
	}
	sort.Strings(keys)
	return keys
}

func writeShellBinding(builder *strings.Builder, key string, environment map[string]string) {
	value, exists := environment[key]
	if !exists {
		builder.WriteString("unset ")
		builder.WriteString(key)
		builder.WriteByte('\n')
		return
	}
	builder.WriteString("export ")
	builder.WriteString(key)
	builder.WriteByte('=')
	builder.WriteString(shellQuote(value))
	builder.WriteByte('\n')
}

func removeEnvironmentKeys(environment map[string]string, keys ...string) {
	for _, key := range keys {
		delete(environment, key)
	}
}

// ShellInit removes the legacy in-place integration. It remains temporarily so
// an existing startup line cleans itself up instead of breaking new shells.
func ShellInit(shell string) (string, error) {
	if shell != "zsh" {
		return "", fmt.Errorf("unsupported shell %q (currently supported: zsh)", shell)
	}
	var script strings.Builder
	script.WriteString("autoload -Uz add-zsh-hook\n")
	script.WriteString("add-zsh-hook -d precmd _chop_refresh_prompt 2>/dev/null\n")
	script.WriteString("add-zsh-hook -d preexec _chop_sync_default 2>/dev/null\n")
	script.WriteString("if [[ ${CONTEXTHOP_SCOPE:-} == shared ]] && (( $+functions[_chop_restore_baseline] )); then _chop_restore_baseline; fi\n")
	script.WriteString("add-zsh-hook -d zshexit _chop_cleanup_owned_session 2>/dev/null\n")
	script.WriteString("_chop_cleanup_owned_session 2>/dev/null\n")
	script.WriteString("unfunction _chop_sync_default _chop_restore_baseline _chop_cleanup_owned_session 2>/dev/null\n")
	script.WriteString("unset _chop_baseline _chop_owned_session CONTEXTHOP_SCOPE CONTEXTHOP_SHARED_REVISION\n")
	script.WriteString("unfunction _chop_refresh_prompt 2>/dev/null\n")
	script.WriteString("unfunction chop 2>/dev/null\n")
	script.WriteString("if [[ -n ${_chop_prompt_fragment+x} ]]; then PROMPT=\"${PROMPT/\"$_chop_prompt_fragment\"/}\"; elif [[ -n ${CONTEXTHOP_BASE_PROMPT+x} ]]; then PROMPT=\"$CONTEXTHOP_BASE_PROMPT\"; fi\n")
	script.WriteString("unset _chop_prompt_fragment CONTEXTHOP_BASE_PROMPT CONTEXTHOP_APPLIED_PROMPT_PREFIX\n")
	script.WriteString("unset CONTEXTHOP_BINARY\n")
	return script.String(), nil
}

func materializeKubeconfig(ctx context.Context, destination string, target config.Kubernetes, environment map[string]string, sourceRevision string) (bool, error) {
	started := time.Now()
	cachePath, err := kubeconfigCachePath(target, environment, sourceRevision)
	if err != nil {
		debugtrace.Record("Locate kubeconfig cache", time.Since(started), "failed")
		return false, err
	}
	providerGenerated := target.Type == "gke" && (target.Kubeconfig == "" || target.Context == "")
	started = time.Now()
	if cachedKubeconfigUsable(cachePath, providerGenerated) {
		if err := copyCachedKubeconfig(cachePath, destination); err == nil {
			debugtrace.Record("Kubeconfig cache", time.Since(started), "hit")
			return false, nil
		}
	}
	debugtrace.Record("Kubeconfig cache", time.Since(started), "miss")

	if target.Kubeconfig != "" && target.Context != "" {
		started = time.Now()
		source, err := expandPathList(target.Kubeconfig)
		if err != nil {
			return false, err
		}
		command := exec.CommandContext(ctx, "kubectl", "config", "view", "--flatten", "--minify", "--context", target.Context)
		command.Env = flattenEnvironment(withValue(environment, "KUBECONFIG", source))
		output, err := command.Output()
		debugtrace.Record("Export Kubernetes context", time.Since(started), outcomeDetail(err))
		if err != nil {
			return false, fmt.Errorf("materialize kubernetes context %q: %w", target.Context, err)
		}
		if usesGKEAuthentication(output) && (environment["CLOUDSDK_CORE_ACCOUNT"] == "" || environment["CLOUDSDK_CORE_PROJECT"] == "") {
			return false, fmt.Errorf("kubernetes context %q uses GKE authentication but has no resolved GCP identity and project dependency", target.Context)
		}
		if err := os.WriteFile(destination, output, 0o600); err != nil {
			return false, fmt.Errorf("write isolated kubeconfig: %w", err)
		}
		started = time.Now()
		if err := applyKubernetesNamespace(ctx, destination, target.Namespace, environment); err != nil {
			debugtrace.Record("Apply Kubernetes namespace", time.Since(started), "failed")
			return false, err
		}
		debugtrace.Record("Apply Kubernetes namespace", time.Since(started), namespaceDetail(target.Namespace))
		// Caching is an optimization. A valid isolated kubeconfig must remain
		// usable even when the user cache cannot be updated.
		started = time.Now()
		cacheErr := storeCachedKubeconfig(cachePath, destination)
		debugtrace.Record("Store kubeconfig cache", time.Since(started), outcomeDetail(cacheErr))
		return false, nil
	}
	if target.Type == "gke" {
		if err := os.WriteFile(destination, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
			return false, err
		}
		arguments := []string{"container", "clusters", "get-credentials", target.Cluster, "--location", target.Location, "--quiet"}
		switch target.Endpoint {
		case "dns":
			arguments = append(arguments, "--dns-endpoint")
		case "internal-ip":
			arguments = append(arguments, "--internal-ip")
		}
		command := exec.CommandContext(ctx, "gcloud", arguments...)
		command.Env = flattenEnvironment(withValue(environment, "KUBECONFIG", destination))
		started = time.Now()
		if output, err := command.CombinedOutput(); err != nil {
			debugtrace.Record("Fetch GKE credentials", time.Since(started), "failed")
			return false, fmt.Errorf("get credentials for GKE cluster %q: %w: %s", target.Cluster, err, strings.TrimSpace(string(output)))
		}
		debugtrace.Record("Fetch GKE credentials", time.Since(started), "complete")
		started = time.Now()
		if err := applyKubernetesNamespace(ctx, destination, target.Namespace, environment); err != nil {
			debugtrace.Record("Apply Kubernetes namespace", time.Since(started), "failed")
			return false, err
		}
		debugtrace.Record("Apply Kubernetes namespace", time.Since(started), namespaceDetail(target.Namespace))
		started = time.Now()
		cacheErr := storeCachedKubeconfig(cachePath, destination)
		debugtrace.Record("Store kubeconfig cache", time.Since(started), outcomeDetail(cacheErr))
		return true, nil
	}
	return false, fmt.Errorf("kubernetes target has no importable context")
}

func outcomeDetail(err error) string {
	if err != nil {
		return "failed"
	}
	return "complete"
}

func namespaceDetail(namespace string) string {
	if namespace == "" {
		return "not configured"
	}
	return "complete"
}

func kubeconfigCachePath(target config.Kubernetes, environment map[string]string, sourceRevision string) (string, error) {
	credentialConfig := ""
	if environment["CLOUDSDK_CORE_ACCOUNT"] != "" {
		credentialConfig = environment["CLOUDSDK_CONFIG"]
	}
	value := struct {
		Version        int               `json:"version"`
		Target         config.Kubernetes `json:"target"`
		SourceRevision string            `json:"sourceRevision,omitempty"`
		Account        string            `json:"account,omitempty"`
		Project        string            `json:"project,omitempty"`
		CloudSDKConfig string            `json:"cloudSdkConfig,omitempty"`
	}{
		Version: kubeconfigCacheVersion, Target: target, SourceRevision: sourceRevision,
		Account: environment["CLOUDSDK_CORE_ACCOUNT"], Project: environment["CLOUDSDK_CORE_PROJECT"], CloudSDKConfig: credentialConfig,
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode kubeconfig cache key: %w", err)
	}
	digest := sha256.Sum256(data)
	cacheDir, err := sessionCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "contexthop", "kubeconfigs", hex.EncodeToString(digest[:])+".yaml"), nil
}

func cachedKubeconfigUsable(path string, providerGenerated bool) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if providerGenerated {
		age := time.Since(info.ModTime())
		if age < 0 || age > providerKubeconfigCacheTTL {
			return false
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if !safeToPersistKubeconfig(data) {
		return false
	}
	_, err = kubetarget.FingerprintBytes(data)
	return err == nil
}

func copyCachedKubeconfig(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600)
}

func storeCachedKubeconfig(cachePath, source string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read materialized kubeconfig for caching: %w", err)
	}
	if !safeToPersistKubeconfig(data) {
		return fmt.Errorf("materialized kubeconfig contains credentials that must remain session-local")
	}
	if _, err := kubetarget.FingerprintBytes(data); err != nil {
		return fmt.Errorf("validate materialized kubeconfig for caching: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		return fmt.Errorf("create kubeconfig cache: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(cachePath), ".kubeconfig-*")
	if err != nil {
		return fmt.Errorf("create cached kubeconfig: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, cachePath); err != nil {
		return fmt.Errorf("commit cached kubeconfig: %w", err)
	}
	return nil
}

// safeToPersistKubeconfig prevents the warm-path cache from becoming a second
// credential store. Endpoint and CA data are safe to retain, as is the standard
// GKE exec-plugin reference. Static tokens, client keys, auth-provider state,
// credential-bearing exec environments, and other exec plugins remain confined
// to the source and terminal-local session file.
func safeToPersistKubeconfig(data []byte) bool {
	type kubeUser struct {
		Name string `yaml:"name"`
		User struct {
			Token                 string `yaml:"token"`
			TokenFile             string `yaml:"tokenFile"`
			Username              string `yaml:"username"`
			Password              string `yaml:"password"`
			ClientCertificate     string `yaml:"client-certificate"`
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKey             string `yaml:"client-key"`
			ClientKeyData         string `yaml:"client-key-data"`
			AuthProvider          any    `yaml:"auth-provider"`
			Exec                  struct {
				Command string `yaml:"command"`
				Env     []any  `yaml:"env"`
			} `yaml:"exec"`
		} `yaml:"user"`
	}
	var document struct {
		Users []kubeUser `yaml:"users"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return false
	}
	for _, entry := range document.Users {
		user := entry.User
		if user.Token != "" || user.TokenFile != "" || user.Username != "" || user.Password != "" ||
			user.ClientCertificate != "" || user.ClientCertificateData != "" || user.ClientKey != "" || user.ClientKeyData != "" ||
			user.AuthProvider != nil || len(user.Exec.Env) != 0 {
			return false
		}
		if user.Exec.Command != "" && filepath.Base(user.Exec.Command) != "gke-gcloud-auth-plugin" {
			return false
		}
	}
	return true
}

// Read the final file, including source defaults, explicit overrides, and cache
// hits. An omitted catalog namespace preserves the source context's namespace.
func materializedKubernetesNamespace(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var document struct {
		CurrentContext string `yaml:"current-context"`
		Contexts       []struct {
			Name    string `yaml:"name"`
			Context struct {
				Namespace string `yaml:"namespace"`
			} `yaml:"context"`
		} `yaml:"contexts"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return "", err
	}
	for _, entry := range document.Contexts {
		if entry.Name == document.CurrentContext && entry.Name != "" {
			if entry.Context.Namespace == "" {
				return "default", nil
			}
			return entry.Context.Namespace, nil
		}
	}
	return "", fmt.Errorf("current context %q does not exist", document.CurrentContext)
}

func applyKubernetesNamespace(ctx context.Context, kubeconfig, namespace string, environment map[string]string) error {
	if namespace == "" {
		return nil
	}
	command := exec.CommandContext(ctx, "kubectl", "config", "set-context", "--current", "--namespace", namespace)
	command.Env = flattenEnvironment(withValue(environment, "KUBECONFIG", kubeconfig))
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("set kubernetes namespace %q: %w: %s", namespace, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func usesGKEAuthentication(kubeconfig []byte) bool {
	text := strings.ToLower(string(kubeconfig))
	return strings.Contains(text, "gke-gcloud-auth-plugin") || strings.Contains(text, "name: gcp")
}

func expandPathList(value string) (string, error) {
	paths := filepath.SplitList(value)
	for index, path := range paths {
		expanded, err := resolver.ExpandPath(path)
		if err != nil {
			return "", err
		}
		paths[index] = expanded
	}
	return strings.Join(paths, string(os.PathListSeparator)), nil
}

func environmentMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, item, found := strings.Cut(value, "=")
		if found {
			result[key] = item
		}
	}
	return result
}

func flattenEnvironment(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func withValue(values map[string]string, key, value string) map[string]string {
	copy := make(map[string]string, len(values)+1)
	for existingKey, existingValue := range values {
		copy[existingKey] = existingValue
	}
	copy[key] = value
	return copy
}

func sessionID() (string, error) {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("create session id: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func sessionCacheDir() (string, error) {
	if configured := os.Getenv("CONTEXTHOP_CACHE_DIR"); configured != "" {
		return resolver.ExpandPath(configured)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find cache directory: %w", err)
	}
	return cacheDir, nil
}

// CurrentShellInit is opt-in integration for activating a selection in the caller.
func CurrentShellInit(binary string) string {
	return "export CONTEXTHOP_BINARY=" + shellQuote(binary) + "\n" + zshFunction() + ZshCompletion()
}
