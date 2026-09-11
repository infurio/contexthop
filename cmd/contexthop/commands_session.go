package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/debugtrace"
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/kubetarget"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/ui"
)

func runExec(args []string) error {
	separator := -1
	for index, arg := range args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator != 1 || len(args) <= separator+1 {
		return errors.New("usage: chop exec <workspace> -- <command> [args...]")
	}
	return runInDestination(args[0], args[separator+1:])
}

func runInDestination(name string, commandArgs []string, adcOverride ...string) error {
	started := time.Now()
	cfg, err := loadConfig()
	debugtrace.Record("Load configuration", time.Since(started), outcomeDetail(err))
	if err != nil {
		return err
	}
	dependencyContext, dependencyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dependencyCancel()
	started = time.Now()
	if err := discovery.EnrichKubernetesDependencies(dependencyContext, &cfg, ""); err != nil {
		debugtrace.Record("Discover Kubernetes dependencies", time.Since(started), "failed")
		return err
	}
	debugtrace.Record("Discover Kubernetes dependencies", time.Since(started), "complete")
	started = time.Now()
	if len(adcOverride) > 0 && adcOverride[0] != "" {
		if err := overrideWorkspaceADC(&cfg, name, adcOverride[0]); err != nil {
			return err
		}
	}
	resolved, err := resolver.Destination(cfg, name)
	debugtrace.Record("Resolve workspace", time.Since(started), outcomeDetail(err))
	if err != nil {
		return switchAborted(name, err)
	}
	if len(commandArgs) == 0 {
		return activateLaunch(resolved, "shell")
	}
	return runResolved(resolved, commandArgs)
}

func runResolved(resolved resolver.Resolved, commandArgs []string) error {
	return runResolvedMode(resolved, commandArgs, false)
}

func runResolvedMode(resolved resolver.Resolved, commandArgs []string, forceSubshell bool, publish ...bool) error {
	name := resolved.Name
	shared := len(publish) > 0 && publish[0]
	switchContext, stopSwitch := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSwitch()
	equivalent := false
	if len(commandArgs) == 0 && !forceSubshell && !shared && os.Getenv(session.ScopeEnv) != "shared" {
		var err error
		started := time.Now()
		equivalent, err = currentContextEquivalent(resolved)
		debugtrace.Record("Compare current context", time.Since(started), outcomeDetail(err))
		if err != nil {
			return switchAborted(name, err)
		}
	}
	if equivalent {
		if err := verifyLaunchADC(switchContext, resolved); err != nil {
			return switchAborted(name, err)
		}
		recordResolvedUse(resolved)
		fmt.Printf("Context already active: %s; no changes needed.\n", os.Getenv("CONTEXTHOP_CONTEXT"))
		return nil
	}
	if resolved.Kubernetes != nil {
		validationContext, validationCancel := context.WithTimeout(switchContext, 10*time.Second)
		started := time.Now()
		err := discovery.ValidateKubernetesSource(validationContext, *resolved.Kubernetes)
		debugtrace.Record("Verify local Kubernetes source", time.Since(started), outcomeDetail(err))
		validationCancel()
		if err != nil {
			return switchAborted(name, err)
		}
	}
	if resolved.Identity != nil {
		if err := ensureIdentityLogin(switchContext, resolved.IdentityName, *resolved.Identity); err != nil {
			return switchAborted(name, err)
		}
	}
	prepareContext, prepareCancel := context.WithTimeout(switchContext, 20*time.Second)
	defer prepareCancel()
	started := time.Now()
	prepared, err := session.Prepare(prepareContext, resolved)
	debugtrace.Record("Prepare isolated session", time.Since(started), outcomeDetail(err))
	if err != nil {
		return switchAborted(name, err)
	}
	if shared {
		defer prepared.Close()
		if err := verifyLaunchADC(switchContext, resolved); err != nil {
			return switchAborted(name, err)
		}
		if activation := os.Getenv(session.ActivationFileEnv); activation != "" {
			// Publishing is the durable operation. The wrapper follows at the next prompt.
			if err := os.WriteFile(activation, []byte("export CONTEXTHOP_SCOPE=shared\nunset CONTEXTHOP_SHARED_REVISION\n"), 0600); err != nil {
				return fmt.Errorf("could not prepare shared following in this shell: %w", err)
			}
		}
		if err := session.PublishShared(prepared); err != nil {
			return switchAborted(name, err)
		}
		if os.Getenv(session.ActivationFileEnv) == "" {
			fmt.Println("Shared config saved. This terminal is unchanged. To follow it here, enable Zsh integration:")
			fmt.Println("eval \"$(chop shell-init zsh --in-place)\"")
		}
		recordResolvedUse(resolved)
		return nil
	}
	providerDetail := "no cloud identity; reachability deferred until provider tools are used"
	if resolved.Identity != nil {
		providerDetail = "identity login verified; reachability deferred until provider tools are used"
	}
	if prepared.KubernetesProviderMaterialized {
		providerDetail = "identity login verified and required GKE materialization performed; reachability deferred"
	}
	debugtrace.Record("Remote authentication and reachability", 0, providerDetail)
	return activatePrepared(prepared, commandArgs, name, func() { recordResolvedUse(resolved) }, nil)
}

func recordResolvedUse(resolved resolver.Resolved) {
	_ = recency.Record(map[string]string{
		"identity":   resolved.IdentityName,
		"project":    resolved.ProjectName,
		"kubernetes": resolved.KubernetesName,
		"docker":     resolved.DockerName,
		"workspace":  resolved.WorkspaceName,
	})
}

// ADC remains opt-in, but every requested activation must verify its principal.
// Bound both token refresh and identity lookup, and never initiate a browser here.
func verifyLaunchADC(ctx context.Context, resolved resolver.Resolved) error {
	if resolved.ADCMode == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	started := time.Now()
	err := cloudauth.CheckADC(ctx, resolved)
	debugtrace.Record("Verify application credential identity", time.Since(started), outcomeDetail(err))
	if err != nil {
		return fmt.Errorf("ADC verification failed: %w; run chop auth --adc %q, then retry", err, resolved.IdentityName)
	}
	return nil
}

func activatePrepared(prepared *session.Session, commandArgs []string, name string, activated func(), completed func(string)) error {
	prepared.SetScope("local", "")
	// All new activations, including session reuse, pass this gate before commit.
	manifest, err := state.LoadManifest(prepared.Manifest)
	if err == nil && manifest.ADCMode != "" {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err = verifyLaunchADC(ctx, resolver.Resolved{
			ADCMode: manifest.ADCMode, IdentityName: manifest.IdentityName,
			Identity: &config.Identity{Provider: manifest.Expected.Provider,
				Account: manifest.Expected.Identity, CloudSDKConfig: manifest.CloudSDKConfig,
				ADC: manifest.Expected.ADC},
		})
		cancel()
	}
	if err != nil {
		_ = prepared.Close()
		return switchAborted(name, err)
	}
	if len(commandArgs) == 0 && os.Getenv(session.ActivationFileEnv) != "" {
		started := time.Now()
		if err := prepared.WriteActivation(os.Getenv(session.ActivationFileEnv)); err != nil {
			debugtrace.Record("Write in-place shell activation", time.Since(started), "failed")
			_ = prepared.Close()
			return switchAborted(name, err)
		}
		debugtrace.Record("Write in-place shell activation", time.Since(started), "complete")
		if activated != nil {
			started = time.Now()
			activated()
			debugtrace.Record("Record recent selection", time.Since(started), "local")
		}
		debugtrace.Flush(os.Stderr)
		return nil
	}
	defer prepared.Close()

	var command *exec.Cmd
	interactiveShell := len(commandArgs) == 0
	if len(commandArgs) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		binary, err := shellBinary()
		if err != nil {
			return switchAborted(name, fmt.Errorf("locate ContextHop binary: %w", err))
		}
		started := time.Now()
		if err := prepared.ConfigureShell(shell, binary); err != nil {
			debugtrace.Record("Configure isolated shell", time.Since(started), "failed")
			return switchAborted(name, err)
		}
		debugtrace.Record("Configure isolated shell", time.Since(started), "complete")
		started = time.Now()
		if err := prepared.Commit(); err != nil {
			debugtrace.Record("Commit isolated session", time.Since(started), "failed")
			return switchAborted(name, err)
		}
		debugtrace.Record("Commit isolated session", time.Since(started), "complete")
		command = exec.Command(shell, "-l")
	} else {
		started := time.Now()
		if err := prepared.Commit(); err != nil {
			debugtrace.Record("Commit isolated session", time.Since(started), "failed")
			return switchAborted(name, err)
		}
		debugtrace.Record("Commit isolated session", time.Since(started), "complete")
		command = exec.Command(commandArgs[0], commandArgs[1:]...)
	}
	command.Env = prepared.Env
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if interactiveShell {
		debugtrace.Flush(os.Stderr)
		if err := command.Start(); err != nil {
			return err
		}
		if err := session.RegisterActive(command.Process.Pid, prepared.Manifest); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return fmt.Errorf("register active session: %w", err)
		}
		if activated != nil {
			activated()
		}
		waitErr := command.Wait()
		if completed != nil {
			if active, err := session.ActiveForShell(command.Process.Pid); err == nil {
				completed(active.Manifest)
			}
		}
		_ = session.CleanupShellSession(command.Process.Pid)
		if waitErr != nil {
			return &shellExitError{err: waitErr}
		}
		return nil
	}
	// Register the supervising process before starting the command, so even a
	// child that immediately invokes restore/reset sees its session as active.
	if err := session.RegisterActive(os.Getpid(), prepared.Manifest); err != nil {
		return fmt.Errorf("register active command session: %w", err)
	}
	defer session.UnregisterActive(prepared.Manifest)
	if err := command.Run(); err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return &shellExitError{err: err}
		}
		return err
	}
	if activated != nil {
		activated()
	}
	return nil
}

func reuseMostRecent() error {
	records, err := session.ActiveSessions(os.Getppid())
	if err != nil {
		return err
	}
	if len(records) == 0 {
		if isTerminal(os.Stdin) && isTerminal(os.Stdout) {
			fmt.Fprintln(os.Stderr, "No open ContextHop sessions are available to reuse.")
			return runInteractive(ui.ScreenWorkspace)
		}
		return errors.New("no open ContextHop sessions are available to reuse")
	}
	return reuseActive(records[0])
}

func reuseActive(record session.Active) error {
	// Ordinary shells get an isolated child even when opt-in integration exists.
	if os.Getenv(state.SessionFileEnv) == "" {
		activation, present := os.LookupEnv(session.ActivationFileEnv)
		_ = os.Unsetenv(session.ActivationFileEnv)
		defer func() {
			if present {
				_ = os.Setenv(session.ActivationFileEnv, activation)
			}
		}()
	}
	prepared, err := session.Reuse(record)
	if err != nil {
		return fmt.Errorf("reuse %q: %w", record.Destination, err)
	}
	manifest, _ := state.LoadManifest(prepared.Manifest)
	return activatePrepared(prepared, nil, record.Destination, func() {
		_ = recency.Record(map[string]string{
			"identity": manifest.IdentityName, "project": manifest.ProjectName,
			"kubernetes": manifest.KubernetesName, "docker": manifest.DockerName,
			"workspace": manifest.WorkspaceName,
		})
	}, nil)
}

func outcomeDetail(err error) string {
	if err != nil {
		return "failed"
	}
	return "complete"
}

func currentContextEquivalent(resolved resolver.Resolved) (bool, error) {
	// Rebuild activation when Google authentication or configuration overrides
	// would otherwise survive the already-active shortcut.
	for _, key := range []string{
		"CLOUDSDK_ACTIVE_CONFIG_NAME", "CLOUDSDK_AUTH_ACCESS_TOKEN",
		"CLOUDSDK_AUTH_ACCESS_TOKEN_FILE", "CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE",
		"CLOUDSDK_AUTH_DISABLE_CREDENTIALS", "CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT",
		"CLOUDSDK_BILLING_QUOTA_PROJECT", "CLOUDSDK_CORE_QUOTA_PROJECT",
		"GOOGLE_CLOUD_QUOTA_PROJECT",
	} {
		if _, present := os.LookupEnv(key); present {
			return false, nil
		}
	}
	snapshot := state.InspectLocal()
	if !snapshot.Managed || snapshot.LocalStatus != "LOCAL MATCH" {
		return false, nil
	}
	manifest, err := state.LoadManifest(os.Getenv(state.SessionFileEnv))
	if err != nil {
		return false, nil
	}
	if manifest.WorkspaceName != resolved.WorkspaceName || manifest.Production != resolved.Production || !maps.Equal(manifest.PromptColors, resolved.PromptColors) {
		return false, nil
	}

	desiredIdentity, desiredProvider, desiredProject := "", "", ""
	if resolved.Identity != nil {
		desiredIdentity = resolved.Identity.Account
		desiredProvider = resolved.Identity.Provider
		cloudSDKConfig, err := resolver.ExpandPath(resolved.Identity.CloudSDKConfig)
		if err != nil {
			return false, err
		}
		if cloudSDKConfig == "" || manifest.CloudSDKConfig != cloudSDKConfig {
			return false, nil
		}
		if resolved.ADCMode == "identity" {
			adc, err := resolver.ExpandPath(resolved.Identity.ADC)
			if err != nil {
				return false, err
			}
			if adc == "" {
				adc = filepath.Join(cloudSDKConfig, "application_default_credentials.json")
			}
			if manifest.Expected.ADC != adc {
				return false, nil
			}
		}
	}
	if manifest.ADCMode != resolved.ADCMode {
		return false, nil
	}
	if resolved.ADCMode == "" && !strings.HasSuffix(manifest.Expected.ADC, "/disabled-google-credentials.json") {
		return false, nil
	}
	if resolved.Project != nil {
		desiredProject = resolved.Project.ProjectID
	}
	if manifest.Expected.Identity != desiredIdentity || manifest.Expected.Provider != desiredProvider || manifest.Expected.Project != desiredProject {
		return false, nil
	}

	desiredKubernetesIdentity := ""
	desiredKubernetesContext := ""
	desiredNamespace := ""
	if resolved.Kubernetes != nil {
		desiredKubernetesIdentity = resolver.KubernetesTargetIdentity(*resolved.Kubernetes, resolved.Project)
		desiredKubernetesContext = resolved.Kubernetes.Context
		if desiredKubernetesContext == "" {
			desiredKubernetesContext = resolved.KubernetesName
		}
		desiredNamespace = resolved.Kubernetes.Namespace
		if desiredNamespace == "" {
			desiredNamespace = "default"
		}
		if manifest.KubernetesTargetIdentity != desiredKubernetesIdentity {
			return false, nil
		}
		if manifest.KubernetesControlPlaneFingerprint == "" {
			return false, nil
		}
		fingerprint, err := kubetarget.FingerprintFile(os.Getenv("KUBECONFIG"))
		if err != nil || fingerprint.Digest != manifest.KubernetesControlPlaneFingerprint {
			return false, nil
		}
		if resolved.Kubernetes.Kubeconfig != "" {
			revision, err := discovery.KubernetesSourceRevision(*resolved.Kubernetes)
			if err != nil {
				return false, err
			}
			if manifest.KubernetesSourceRevision == "" || manifest.KubernetesSourceRevision != revision {
				return false, nil
			}
		}
	}
	if manifest.Expected.Kubernetes != desiredKubernetesContext || manifest.Expected.Namespace != desiredNamespace {
		return false, nil
	}

	desiredDocker := "contexthop-none"
	if resolved.Docker != nil {
		desiredDocker = resolved.Docker.Context
	}
	return manifest.Expected.Docker == desiredDocker, nil
}

func switchAborted(destination string, cause error) error {
	current := os.Getenv("CONTEXTHOP_CONTEXT")
	if current == "" {
		current = "unmanaged shell"
	}
	detail := ""
	if current == destination {
		detail = " (it was already active before this attempt)"
	}
	return fmt.Errorf("switch to %q aborted: %w\nActive context unchanged: %s%s", destination, cause, current, detail)
}

// A completed child shell or command is distinct from a failed activation.
type shellExitError struct{ err error }

func (e *shellExitError) Error() string { return e.err.Error() }
func (e *shellExitError) Unwrap() error { return e.err }
