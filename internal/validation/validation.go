package validation

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/kubetarget"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/state"
)

var checkTimeout = 8 * time.Second

type Result struct {
	// Warnings is retained for callers that present warning text directly.
	// WarningDetails contains the same warnings in a machine-readable form.
	Warnings       []string
	WarningDetails []Warning
	Checks         []CheckResult
	Duration       time.Duration
}

// Check identifies the component whose validation failed.
type Check string

const (
	CheckIdentity    Check = "identity"
	CheckProject     Check = "project"
	CheckKubernetes  Check = "kubernetes"
	CheckFingerprint Check = "fingerprint"
	CheckDocker      Check = "docker"
)

// FailureKind is a stable, presentation-independent failure classification.
type FailureKind string

const (
	FailureCanceled         FailureKind = "canceled"
	FailureTimeout          FailureKind = "timeout"
	FailureMissingTool      FailureKind = "missing_tool"
	FailureAuthentication   FailureKind = "authentication"
	FailureAuthorization    FailureKind = "authorization"
	FailureNetwork          FailureKind = "network"
	FailureNotFound         FailureKind = "not_found"
	FailureIdentityMismatch FailureKind = "identity_mismatch"
	FailureTargetMismatch   FailureKind = "target_mismatch"
	FailureInvalidResponse  FailureKind = "invalid_response"
	FailureProvider         FailureKind = "provider_error"
)

// Error describes a validation failure while retaining its original cause.
// Callers should use errors.As to inspect Check and Kind rather than parsing
// the human-readable message.
type Error struct {
	Check     Check
	Kind      FailureKind
	Subject   string
	Operation string
	Cause     error
}

func (e *Error) Error() string {
	if e.Check == "" && e.Operation == "canceled" {
		if e.Cause == nil {
			return fmt.Sprintf("Validation canceled (%s)", displayFailureKind(e.Kind))
		}
		return fmt.Sprintf("Validation canceled (%s): %v", displayFailureKind(e.Kind), e.Cause)
	}
	prefix := e.Subject
	if prefix == "" {
		prefix = string(e.Check)
	}
	message := fmt.Sprintf("%s validation failed (%s)", prefix, displayFailureKind(e.Kind))
	if e.Cause != nil {
		message += ": " + displayFailureCause(e.Kind, e.Cause)
	}
	if hint := failureHint(e.Kind); hint != "" {
		message += "; " + hint
	}
	return message
}

func (e *Error) Unwrap() error { return e.Cause }

// Warning is a non-fatal validation result, such as an RBAC restriction that
// prevents ContextHop from proving namespace existence.
type Warning struct {
	Check   Check
	Kind    FailureKind
	Subject string
	Scope   string
	Message string
}

func (w Warning) String() string { return w.Message }

type CheckResult struct {
	Check    Check
	Name     string
	Duration time.Duration
}

type ProgressFunc func(string)

// Validate verifies a fully materialized but uncommitted ContextHop environment.
// Every command receives the staged environment supplied by the caller.
func Validate(ctx context.Context, resolved resolver.Resolved, environment []string, progress ProgressFunc) (result Result, returnedError error) {
	return validate(ctx, resolved, environment, progress, false)
}

// ValidateProviderMaterialized validates a newly staged session whose GKE
// kubeconfig was generated directly by gcloud for the selected coordinates.
// The provider describe would duplicate that immediately preceding request.
func ValidateProviderMaterialized(ctx context.Context, resolved resolver.Resolved, environment []string, progress ProgressFunc) (result Result, returnedError error) {
	return validate(ctx, resolved, environment, progress, true)
}

func validate(ctx context.Context, resolved resolver.Resolved, environment []string, progress ProgressFunc, providerMaterialized bool) (result Result, returnedError error) {
	started := time.Now()
	defer func() { result.Duration = time.Since(started) }()

	if resolved.Identity != nil {
		account := environmentValue(environment, "CLOUDSDK_CORE_ACCOUNT")
		if !strings.EqualFold(account, resolved.Identity.Account) {
			return result, &Error{
				Check: CheckIdentity, Kind: FailureIdentityMismatch, Subject: "Google identity",
				Cause: fmt.Errorf("expected %q, observed %q", resolved.Identity.Account, normalizedValue(account)),
			}
		}
	}

	type check struct {
		kind Check
		name string
		run  func(context.Context) ([]Warning, error)
	}
	checksContext, cancelChecks := context.WithCancel(ctx)
	defer cancelChecks()
	var checks []check
	managedGKE := resolved.Kubernetes != nil && resolved.Kubernetes.Type == "gke"
	if resolved.Project != nil && !managedGKE {
		checks = append(checks, check{kind: CheckProject, name: "Google project", run: func(checkContext context.Context) ([]Warning, error) {
			projectID, err := run(checkContext, environment, "gcloud", "projects", "describe", resolved.Project.ProjectID, "--format=value(projectId)", "--quiet")
			if err != nil {
				return nil, classifiedError(CheckProject, fmt.Sprintf("Google project %q", resolved.Project.ProjectID), "access", err)
			}
			if strings.TrimSpace(projectID) != resolved.Project.ProjectID {
				return nil, &Error{
					Check: CheckProject, Kind: FailureTargetMismatch, Subject: "Google project",
					Cause: fmt.Errorf("expected %q, observed %q", resolved.Project.ProjectID, normalizedValue(projectID)),
				}
			}
			return nil, nil
		}})
	}

	if resolved.Kubernetes != nil {
		namespace := resolved.Kubernetes.Namespace
		if namespace == "" {
			namespace = "default"
		}
		checks = append(checks, check{kind: CheckKubernetes, name: "Kubernetes", run: func(checkContext context.Context) ([]Warning, error) {
			fingerprint, sourceRevision, err := validateStagedKubernetesFingerprint(environment)
			if err != nil {
				return nil, err
			}
			if managedGKE && !providerMaterialized && !cachedGKEVerification(resolved, fingerprint, sourceRevision) {
				if err := validateGKEControlPlane(checkContext, resolved, environment, fingerprint); err != nil {
					return nil, err
				}
				recordGKEVerification(resolved, fingerprint, sourceRevision)
			}
			output, err := run(checkContext, environment, "kubectl", "--request-timeout=8s", "get", "namespace", namespace, "--ignore-not-found", "-o", "name")
			if err != nil {
				if isForbidden(err) {
					message := fmt.Sprintf("Kubernetes namespace %q could not be verified because the active identity is not permitted to read namespaces", namespace)
					return []Warning{{
						Check: CheckKubernetes, Kind: FailureAuthorization,
						Subject: resolved.KubernetesName, Scope: "namespace", Message: message,
					}}, nil
				}
				return nil, classifiedError(CheckKubernetes, fmt.Sprintf("Kubernetes target %q", resolved.KubernetesName), "reachability", err)
			}
			if strings.TrimSpace(output) == "" {
				return nil, &Error{
					Check: CheckKubernetes, Kind: FailureNotFound,
					Subject: fmt.Sprintf("Kubernetes namespace %q", namespace), Cause: errors.New("does not exist"),
				}
			}
			return nil, nil
		}})
	}

	if resolved.Docker != nil {
		checks = append(checks, check{kind: CheckDocker, name: "Docker", run: func(checkContext context.Context) ([]Warning, error) {
			if _, err := run(checkContext, environment, "docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
				return nil, classifiedError(CheckDocker, fmt.Sprintf("Docker context %q", resolved.Docker.Context), "reachability", err)
			}
			return nil, nil
		}})
	}

	if len(checks) == 0 {
		return result, nil
	}
	names := make([]string, len(checks))
	for index, item := range checks {
		names[index] = item.name
	}
	progressMessage(progress, "Checking "+strings.Join(names, ", "))

	type outcome struct {
		index    int
		duration time.Duration
		warnings []Warning
		err      error
	}
	outcomes := make(chan outcome, len(checks))
	for index, item := range checks {
		go func(index int, item check) {
			started := time.Now()
			checkContext, cancelCheck := context.WithTimeout(checksContext, checkTimeout)
			defer cancelCheck()
			warnings, err := item.run(checkContext)
			if err != nil && !errors.Is(err, context.Canceled) {
				cancelChecks()
			}
			outcomes <- outcome{index: index, duration: time.Since(started), warnings: warnings, err: err}
		}(index, item)
	}

	ordered := make([]outcome, len(checks))
	remaining := make(map[int]bool, len(checks))
	for index := range checks {
		remaining[index] = true
	}
	for range checks {
		item := <-outcomes
		ordered[item.index] = item
		delete(remaining, item.index)
		if len(remaining) > 0 {
			var waiting []string
			for index := range checks {
				if remaining[index] {
					waiting = append(waiting, checks[index].name)
				}
			}
			progressMessage(progress, "Waiting for "+strings.Join(waiting, ", "))
		}
	}
	var cancellationError error
	for index, item := range ordered {
		result.Checks = append(result.Checks, CheckResult{Check: checks[index].kind, Name: checks[index].name, Duration: item.duration})
		result.WarningDetails = append(result.WarningDetails, item.warnings...)
		for _, warning := range item.warnings {
			result.Warnings = append(result.Warnings, warning.String())
		}
		if item.err != nil && !errors.Is(item.err, context.Canceled) {
			return result, item.err
		}
		if item.err != nil && cancellationError == nil {
			cancellationError = item.err
		}
	}
	if cancellationError != nil {
		return result, &Error{
			Kind: failureKind(cancellationError), Subject: "Validation", Operation: "canceled", Cause: cancellationError,
		}
	}
	return result, nil
}

func validateStagedKubernetesFingerprint(environment []string) (kubetarget.Fingerprint, string, error) {
	manifestPath := environmentValue(environment, state.SessionFileEnv)
	if manifestPath == "" {
		return kubetarget.Fingerprint{}, "", nil
	}
	kubeconfig := environmentValue(environment, "KUBECONFIG")
	if kubeconfig == "" {
		return kubetarget.Fingerprint{}, "", &Error{
			Check: CheckFingerprint, Kind: FailureTargetMismatch, Subject: "Kubernetes control plane",
			Cause: errors.New("staged KUBECONFIG is missing"),
		}
	}
	fingerprint, err := kubetarget.FingerprintFile(kubeconfig)
	if err != nil {
		return kubetarget.Fingerprint{}, "", &Error{
			Check: CheckFingerprint, Kind: FailureInvalidResponse, Subject: "Kubernetes control plane", Cause: err,
		}
	}
	manifest, err := state.LoadManifest(manifestPath)
	if err != nil {
		return kubetarget.Fingerprint{}, "", &Error{
			Check: CheckFingerprint, Kind: FailureInvalidResponse, Subject: "Kubernetes session fingerprint", Cause: err,
		}
	}
	if manifest.KubernetesControlPlaneFingerprint == "" || manifest.KubernetesControlPlaneFingerprint != fingerprint.Digest {
		return kubetarget.Fingerprint{}, "", &Error{
			Check: CheckFingerprint, Kind: FailureTargetMismatch, Subject: "Kubernetes control plane",
			Cause: errors.New("staged endpoint or certificate authority changed before activation"),
		}
	}
	return fingerprint, manifest.KubernetesSourceRevision, nil
}

type commandError struct {
	command string
	detail  string
	err     error
}

func (e *commandError) Error() string {
	if e.detail == "" {
		return fmt.Sprintf("%s: %v", e.command, e.err)
	}
	return fmt.Sprintf("%s: %v: %s", e.command, e.err, e.detail)
}

func (e *commandError) Unwrap() error { return e.err }

func run(ctx context.Context, environment []string, name string, args ...string) (string, error) {
	checkContext, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	command := exec.CommandContext(checkContext, name, args...)
	command.Env = environment
	output, err := command.CombinedOutput()
	if err == nil {
		return string(output), nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(checkContext.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s timed out after %s: %w", name, checkTimeout, context.DeadlineExceeded)
	}
	if errors.Is(checkContext.Err(), context.Canceled) {
		return "", context.Canceled
	}
	return "", &commandError{command: name, detail: safeDetail(string(output)), err: err}
}

func isForbidden(err error) bool {
	var commandErr *commandError
	return errors.As(err, &commandErr) && failureKind(err) == FailureAuthorization
}

func classifiedError(check Check, subject, operation string, err error) error {
	return &Error{Check: check, Kind: failureKind(err), Subject: subject, Operation: operation, Cause: err}
}

func failureKind(err error) FailureKind {
	if errors.Is(err, context.Canceled) {
		return FailureCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timed out") {
		return FailureTimeout
	}
	if errors.Is(err, exec.ErrNotFound) {
		return FailureMissingTool
	}
	text := strings.ToLower(err.Error())
	classes := []struct {
		name  FailureKind
		terms []string
	}{
		{name: FailureAuthentication, terms: []string{"unauthorized", "unauthenticated", "invalid_grant", "login required", "credential"}},
		{name: FailureAuthorization, terms: []string{"forbidden", "permission denied", "does not have permission", "permission(s)", "code=403", "http 403"}},
		{name: FailureNetwork, terms: []string{"no such host", "connection refused", "network is unreachable", "i/o timeout", "tls handshake timeout", "unable to connect to the server"}},
		{name: FailureNotFound, terms: []string{"not found", "notfound"}},
	}
	for _, class := range classes {
		for _, term := range class.terms {
			if strings.Contains(text, term) {
				return class.name
			}
		}
	}
	return FailureProvider
}

func displayFailureCause(kind FailureKind, cause error) string {
	if kind != FailureAuthorization {
		return cause.Error()
	}
	text := cause.Error()
	if start := strings.Index(strings.ToLower(text), `required "`); start >= 0 {
		permission := text[start+len(`required "`):]
		if end := strings.Index(permission, `"`); end > 0 {
			return fmt.Sprintf("selected identity lacks %q", permission[:end])
		}
	}
	return "access was denied for the selected identity"
}

func displayFailureKind(kind FailureKind) string {
	return strings.ReplaceAll(string(kind), "_", " ")
}

func failureHint(kind FailureKind) string {
	switch kind {
	case FailureAuthentication:
		return "reauthenticate the selected identity and retry"
	case FailureAuthorization:
		return "choose an identity with access or grant the required permission"
	case FailureNetwork:
		return "check network, VPN, tunnel, and DNS access"
	case FailureMissingTool:
		return "install the required provider CLI and retry"
	case FailureNotFound:
		return "refresh the selected resource mapping before retrying"
	case FailureTargetMismatch:
		return "refresh and reselect the target; the previous context remains active"
	}
	return ""
}

func progressMessage(progress ProgressFunc, message string) {
	if progress != nil {
		progress(message)
	}
}

func normalizedValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "(unset)" {
		return "none"
	}
	return value
}

func environmentValue(environment []string, wanted string) string {
	for index := len(environment) - 1; index >= 0; index-- {
		key, value, found := strings.Cut(environment[index], "=")
		if found && key == wanted {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safeDetail(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const limit = 300
	if len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}
