package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/debugtrace"
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/ui"
)

func runInteractive(start ui.Screen) error {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return fmt.Errorf("chop %s requires an interactive terminal", shortComponent(string(start)))
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	started := time.Now()
	saved, err := loadConfig()
	debugtrace.Record("Load configuration", time.Since(started), outcomeDetail(err))
	if err != nil {
		return err
	}
	started = time.Now()
	activeSessions, err := session.ActiveSessions(os.Getppid())
	debugtrace.Record("Inspect active sessions", time.Since(started), outcomeDetail(err))
	if err != nil {
		return err
	}
	started = time.Now()
	history := recency.Load()
	debugtrace.Record("Load recent selections", time.Since(started), "local")
	controller := newApplicationController(path, saved, history, activeSessions)
	controller.autoSaveDiscovery = true
	selectionCfg := controller.resources.Selection()
	if start != ui.ScreenWorkspace && start != ui.ScreenDocker && start != ui.ScreenCatalogEntity && start != ui.ScreenReuse {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		started = time.Now()
		enrichErr := discovery.EnrichKubernetesDependencies(ctx, &selectionCfg, "")
		debugtrace.Record("Discover Kubernetes dependencies", time.Since(started), outcomeDetail(enrichErr))
		cancel()
		controller.resources.Observe(selectionCfg)
	}
	kubernetesRevisions := discovery.CaptureKubernetesSourceRevisions(selectionCfg)
	var initialDraft ui.Draft
	var dialog *ui.Picker
	for {
		pickers := controller.Pickers()
		outcome, err := ui.Run(ui.AppOptions{
			Version: version,
			RefreshIdentityAuth: func() {
				if cfg, err := config.Load(path); err == nil {
					cloudauth.RefreshStatuses(cfg)
				}
			},
			ComposeSelection: true, CanApplyShell: os.Getenv(session.ActivationFileEnv) != "",
			ReadSharedConfig: session.SharedConfig, AsyncFlow: true, Snapshot: state.InspectLocal(), StartScreen: start,
			InitialDraft: initialDraft, InitialDialog: dialog,
			Pickers: pickers, ResourceBrowser: true, Probe: true,
			BrowserPicker: controller.Browser, Flow: controller.Prepare, AcceptResult: controller.Accept,
			LaunchPreview: controller.LaunchPreview,
		})
		if err != nil || !outcome.Complete {
			return err
		}
		err = activateInteractiveOutcome(controller, outcome, kubernetesRevisions)
		var childExit *shellExitError
		if err == nil || errors.As(err, &childExit) {
			return err
		}
		initialDraft = outcome.Draft
		// Require another explicit launch after displaying the refreshed review.
		if fresh, loadErr := loadConfig(); loadErr == nil {
			controller.resources.SavedChange(fresh)
		}
		selectionCfg = controller.resources.Selection()
		kubernetesRevisions = discovery.CaptureKubernetesSourceRevisions(selectionCfg)
		review := shellMenuPicker(selectionCfg, initialDraft, os.Getenv(session.ActivationFileEnv) != "")
		review.Description = err.Error() + "\n\nReview the selection and try again, or press Esc to edit it.\n" + review.Description
		dialog = &review
	}
}

func activateInteractiveOutcome(controller *applicationController, outcome ui.Outcome, kubernetesRevisions map[string]discovery.SourceRevision) error {
	if outcome.Choice.Action == "follow-shared" {
		return runSharedDefault([]string{"follow"})
	}

	if outcome.Choice.Action == "launch-shell" {
		activation := os.Getenv(session.ActivationFileEnv)
		_ = os.Unsetenv(session.ActivationFileEnv)
		defer os.Setenv(session.ActivationFileEnv, activation)
	}
	if workspace := outcome.Draft[ui.ScreenWorkspace]; workspace != "" && (outcome.Choice.Action == "default-shell" || outcome.Choice.Action == "launch-shell" || outcome.Choice.Action == "apply-shell") {
		baseline := controller.resources.Selection()
		resolved, err := resolveShellSelection(baseline, outcome.Draft)
		if err != nil {
			return err
		}
		if err := validateReviewedWorkspace(controller.resources.Saved(), workspace, outcome.Draft[ui.ScreenShellADCOverride]); err != nil {
			return err
		}
		if err := validateReviewedKubernetes(resolved, kubernetesRevisions); err != nil {
			return err
		}
		return runSelectionLaunch(resolved, outcome.Choice.Action)
	}
	if outcome.Choice.Screen == ui.ScreenWorkspace && outcome.Choice.Action == "" {
		return runInDestination(outcome.Choice.Option.Name, nil)
	}
	if outcome.Choice.Screen == ui.ScreenReuse {
		record, err := session.FindActive(outcome.Choice.Option.Name, os.Getppid())
		if err != nil {
			return err
		}
		return reuseActive(record)
	}
	selectionCfg := controller.resources.Selection()
	selection, err := selectionFromDraft(selectionCfg, outcome.Draft)
	if err != nil {
		return err
	}

	started := time.Now()
	selection.Name = componentContextName(selectionCfg, selection)
	if err := validateSelectionFresh(controller.resources.Saved(), selection); err != nil {
		debugtrace.Record("Verify selected catalog entries", time.Since(started), "failed")
		return switchAborted(selection.Name, err)
	}
	debugtrace.Record("Verify selected catalog entries", time.Since(started), "local")
	if selection.Kubernetes != "" {
		if revision, ok := kubernetesRevisions[selection.Kubernetes]; ok {
			started = time.Now()
			if err := discovery.ValidateKubernetesSourceRevision(selectionCfg.Kubernetes[selection.Kubernetes], revision); err != nil {
				debugtrace.Record("Verify Kubernetes source revision", time.Since(started), "changed")
				return switchAborted(selection.Name, err)
			}
			debugtrace.Record("Verify Kubernetes source revision", time.Since(started), "unchanged")
		}
	}
	started = time.Now()
	resolved, err := resolver.Components(selectionCfg, selection)
	debugtrace.Record("Resolve selected context", time.Since(started), outcomeDetail(err))
	if err != nil {
		return switchAborted(selection.Name, err)
	}
	return runSelectionLaunch(resolved, outcome.Choice.Action)
}

func validateReviewedWorkspace(baseline config.Config, name, override string) error {
	fresh, err := loadConfig()
	if err != nil {
		return err
	}
	draft := ui.Draft{ui.ScreenWorkspace: name, ui.ScreenShellADCOverride: override}
	before, beforeErr := resolveShellSelection(baseline, draft)
	after, afterErr := resolveShellSelection(fresh, draft)
	if beforeErr != nil || afterErr != nil || !reflect.DeepEqual(baseline.Destinations[name], fresh.Destinations[name]) || !reflect.DeepEqual(before, after) {
		return fmt.Errorf("workspace %q or its dependencies changed after review; review the updated selection before launching", name)
	}
	return nil
}

func validateReviewedKubernetes(resolved resolver.Resolved, revisions map[string]discovery.SourceRevision) error {
	if resolved.Kubernetes != nil {
		if revision, ok := revisions[resolved.KubernetesName]; ok {
			return discovery.ValidateKubernetesSourceRevision(*resolved.Kubernetes, revision)
		}
	}
	return nil
}
