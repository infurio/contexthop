package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/lifecycle"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/ui"
)

func runDiscover(args []string) error {
	if err := discovery.RequireUnmanagedShell(); err != nil {
		return err
	}
	write, _, err := parseWriteMode("discover", args)
	if err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	current := config.New()
	if loaded, loadErr := config.Load(path); loadErr == nil {
		current = loaded
	} else if !os.IsNotExist(loadErr) {
		return loadErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	observations, err := discovery.ObserveLocal(ctx)
	if err != nil {
		return fmt.Errorf("observe local contexts: %w", err)
	}
	normalized, err := discovery.Normalize(observations)
	if err != nil {
		return fmt.Errorf("normalize local contexts: %w", err)
	}
	reconciled, report, err := discovery.Reconcile(current, normalized.Config)
	if err != nil {
		return fmt.Errorf("reconcile local contexts: %w", err)
	}

	fmt.Printf("Observed %d identities, %d project associations, %d Kubernetes contexts, and %d Docker contexts.\n",
		len(observations.Identities), len(observations.GCloudConfigs), len(observations.KubernetesContexts), len(observations.DockerContexts))
	fmt.Printf("Reconciliation: %d known, %d new, %d updated, %d aliases merged, %d ambiguous, %d stale.\n",
		report.Known, report.New, report.Updated, report.AliasesMerged, report.Ambiguous, report.Stale)
	for _, change := range report.Changes {
		fmt.Println("-", change)
	}
	for _, warning := range normalized.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", warning)
	}

	if !write {
		if report.Changed() {
			fmt.Println("Dry run; run `chop discover --write` to apply this reconciliation plan.")
		} else {
			fmt.Println("Catalog already matches the current observations.")
		}
		return nil
	}
	if !report.Changed() {
		fmt.Println("Catalog already matches the current observations; nothing written.")
		return nil
	}
	if err := config.Write(path, reconciled); err != nil {
		return err
	}
	fmt.Println("Wrote", path)
	if report.Ambiguous > 0 {
		fmt.Println("Ambiguous access profiles were retained separately; review them in `chop config`.")
	}
	return nil
}

func runBackup(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: chop backup")
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	backup, err := lifecycle.Create(path)
	if err != nil {
		return err
	}
	fmt.Printf("Created backup %s at %s\n", backup.ID, backup.Directory)
	fmt.Println("Credentials and live session data were not included.")
	return nil
}

func runRestore(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: chop restore [backup-id|latest|path]")
	}
	if err := ensureNoActiveSessions("restore"); err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	reference := ""
	if len(args) == 1 {
		reference = args[0]
	} else if isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		reference, err = selectBackup(path)
		if err != nil {
			return err
		}
	}
	backup, err := lifecycle.Resolve(path, reference)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		if current, backupErr := lifecycle.Create(path); backupErr != nil {
			return fmt.Errorf("protect current state before restore: %w", backupErr)
		} else {
			fmt.Println("Backed up current state as", current.ID)
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if err := lifecycle.Restore(path, backup); err != nil {
		return err
	}
	fmt.Printf("Restored backup %s from %s\n", backup.ID, backup.Directory)
	fmt.Println("Credentials were unchanged; transient sessions and generated kubeconfigs were cleared.")
	return nil
}

func runReset(args []string) error {
	credentials := false
	for _, arg := range args {
		switch arg {
		case "--credentials":
			if credentials {
				return errors.New("--credentials may only be specified once")
			}
			credentials = true
		default:
			return fmt.Errorf("unknown reset option %q", arg)
		}
	}
	if err := ensureNoActiveSessions("reset"); err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	var backup *lifecycle.Backup
	if _, statErr := os.Stat(path); statErr == nil {
		created, backupErr := lifecycle.Create(path)
		if backupErr != nil {
			return backupErr
		}
		backup = &created
	} else if !os.IsNotExist(statErr) {
		return statErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := discovery.Discover(ctx)
	if err != nil {
		return fmt.Errorf("discover clean configuration: %w", err)
	}
	if err := lifecycle.ClearMetadata(); err != nil {
		return err
	}
	if err := config.Write(path, result.Config); err != nil {
		return err
	}
	if credentials {
		if err := lifecycle.ClearCredentials(); err != nil {
			return err
		}
	}
	if backup != nil {
		fmt.Printf("Backed up previous state as %s at %s\n", backup.ID, backup.Directory)
	}
	fmt.Printf("Reset ContextHop with %d identities, %d projects, %d Kubernetes targets, and %d Docker targets.\n",
		len(result.Config.Identities), len(result.Config.Projects), len(result.Config.Kubernetes), len(result.Config.Docker))
	for _, warning := range result.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", warning)
	}
	if credentials {
		fmt.Println("Cleared ContextHop-managed gcloud credentials; authenticate identities again with `chop auth`.")
	} else {
		fmt.Println("Credentials were preserved.")
	}
	return nil
}

func ensureNoActiveSessions(action string) error {
	active, err := session.ActiveSessions(-1)
	if err != nil {
		return fmt.Errorf("inspect active sessions: %w", err)
	}
	if len(active) == 0 {
		return nil
	}
	return fmt.Errorf("cannot %s while %d ContextHop session(s) are active; exit them first", action, len(active))
}

func selectBackup(configPath string) (string, error) {
	backups, err := lifecycle.List(configPath)
	if err != nil {
		return "", err
	}
	if len(backups) == 0 {
		return "", errors.New("no ContextHop backups are available")
	}
	fmt.Println("Select a backup:")
	for index, backup := range backups {
		fmt.Printf("  %d) %s  %s\n", index+1, backup.ID, backup.CreatedAt.Local().Format(time.RFC3339))
	}
	fmt.Print("Backup [1]: ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return backups[0].ID, nil
	}
	for index, backup := range backups {
		if answer == fmt.Sprint(index+1) || answer == backup.ID {
			return backup.ID, nil
		}
	}
	return "", fmt.Errorf("unknown backup selection %q", answer)
}

func runInit(args []string) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	write, dryRun, err := parseWriteMode("init", args)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("configuration already exists at %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := discovery.Discover(ctx)
	if err != nil {
		return fmt.Errorf("discover local contexts: %w", err)
	}
	fmt.Printf("Discovered %d identities, %d projects, %d Kubernetes targets, and %d Docker targets.\n",
		len(result.Config.Identities), len(result.Config.Projects), len(result.Config.Kubernetes), len(result.Config.Docker))
	for _, warning := range result.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", warning)
	}

	if !write && !dryRun && isTerminal(os.Stdin) {
		fmt.Printf("Write configuration to %s? [y/N] ", path)
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		write = strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes")
	}
	if !write {
		return config.Encode(os.Stdout, result.Config)
	}
	if err := config.Write(path, result.Config); err != nil {
		return err
	}
	fmt.Println("Wrote", path)
	fmt.Println("Review unresolved project-to-identity mappings before activating cloud targets.")
	return nil
}

func parseWriteMode(command string, args []string) (write, dryRun bool, err error) {
	for _, arg := range args {
		switch arg {
		case "--write":
			if write {
				return false, false, errors.New("--write may only be specified once")
			}
			write = true
		case "--dry-run":
			if dryRun {
				return false, false, errors.New("--dry-run may only be specified once")
			}
			dryRun = true
		default:
			return false, false, fmt.Errorf("unknown %s option %q", command, arg)
		}
	}
	if write && dryRun {
		return false, false, errors.New("--write and --dry-run cannot be used together")
	}
	return write, dryRun, nil
}

func runConfig(args []string) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if isTerminal(os.Stdin) && isTerminal(os.Stdout) {
			return runInteractive(ui.ScreenWorkspace)
		}
		fmt.Println(path)
		return nil
	}
	if args[0] == "path" {
		if len(args) != 1 {
			return errors.New("usage: chop config path")
		}
		fmt.Println(path)
		return nil
	}
	switch args[0] {
	case "summary-startup":
		return runSummaryStartupConfig(path, args[1:])
	case "display":
		return runDisplayConfig(path, args[1:])
	case "browser":
		return runBrowserConfig(args[1:])
	case "validate":
		if len(args) != 1 {
			return errors.New("usage: chop config validate")
		}
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		fmt.Printf("valid: %s (%d identities, %d projects, %d kubernetes targets, %d docker targets, %d workspaces)\n",
			path, len(cfg.Identities), len(cfg.Projects), len(cfg.Kubernetes), len(cfg.Docker), len(cfg.Destinations))
		return nil
	case "edit":
		if len(args) > 2 {
			return errors.New("usage: chop config edit [recovery-file]")
		}
		return editConfig(path, args[1:]...)
	case "map", "manage":
		if len(args) != 1 {
			return errors.New("usage: chop config")
		}
		return runConfig(nil)
	case "discover":
		return runCatalogDiscovery(args[1:])
	case "cache":
		return runCatalogCache(args[1:])
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}
