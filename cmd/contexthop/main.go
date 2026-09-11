package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/debugtrace"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/ui"
)

var version = "0.1.0-dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "_prompt" {
		fmt.Print(session.CurrentZshPromptPrefix())
		return
	}
	if len(os.Args) < 2 || (os.Args[1] != "_shared-sync" && os.Args[1] != "_shell-baseline") {
		_ = session.CleanupAbandonedStaging(time.Hour)
	}
	if err := run(os.Args[1:]); err != nil {
		var completed *shellExitError
		var childExit *exec.ExitError
		if errors.As(err, &completed) && errors.As(err, &childExit) {
			code := childExit.ExitCode()
			if code < 0 {
				code = 1
			}
			os.Exit(code)
		}
		fmt.Fprintln(os.Stderr, "chop:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "_shell-baseline" {
		fmt.Print(session.ShellBaseline())
		return nil
	}
	if len(args) == 2 && args[0] == "_shared-sync" {
		return syncSharedDefault(args[1])
	}
	if len(args) > 0 && (args[0] == "default" || args[0] == "shared") {
		return runSharedDefault(args[1:])
	}

	if len(args) > 0 && args[0] == "_prompt" {
		fmt.Print(session.CurrentZshPromptPrefix())
		return nil
	}
	if len(args) > 0 && args[0] == "_cleanup-session" {
		if len(args) != 2 {
			return errors.New("invalid session cleanup request")
		}
		return session.CleanupManifest(args[1])
	}
	if len(args) > 0 && args[0] == "_activate-session" {
		if len(args) != 3 {
			return errors.New("invalid session activation request")
		}
		if err := session.RegisterActive(os.Getppid(), args[1]); err != nil {
			return err
		}
		if args[2] != "" && args[2] != args[1] && args[2] != os.Getenv("CONTEXTHOP_ROOT_SESSION_FILE") {
			_ = session.CleanupManifest(args[2])
		}
		return nil
	}
	if err := rejectNestedSession(args); err != nil {
		return err
	}
	if len(args) == 0 {
		snapshot := state.InspectLocal()
		if !isTerminal(os.Stdout) || !isTerminal(os.Stdin) {
			fmt.Print(ui.FormatText(snapshot))
			if snapshot.Message != "" {
				fmt.Println(snapshot.Message)
			}
			return nil
		}
		return runInteractive(ui.ScreenWorkspace)
	}
	if len(args) == 1 && (args[0] == "-r" || args[0] == "--reuse") {
		return reuseMostRecent()
	}

	switch args[0] {
	case "use":
		if len(args) != 2 {
			return errors.New("usage: chop use <destination>")
		}
		return runUse(args[1])
	case "-":
		if len(args) != 1 {
			return errors.New("usage: chop -")
		}
		return runPrevious()
	case "console":
		return runConsole(args[1:])
	case "completion":
		if len(args) != 2 || args[1] != "zsh" {
			return errors.New("usage: chop completion zsh")
		}
		fmt.Print(session.ZshCompletion())
		return nil
	case "_complete":
		return runCompletion(args[1:])
	case "reuse":
		if len(args) != 1 {
			return errors.New("usage: chop reuse")
		}
		return reuseMostRecent()
	case "debug":
		return runDebug(args[1:])
	case "status":
		if len(args) != 1 {
			return errors.New("usage: chop status")
		}
		snapshot := state.InspectLocal()
		result := state.Probe(context.Background())
		snapshot.Observed = result.Observed
		if snapshot.Managed {
			if mismatches := snapshot.Mismatches(); len(mismatches) > 0 {
				snapshot.LocalStatus = "CONTEXT-DRIFT"
				snapshot.Message = strings.Join(mismatches, "; ")
			} else {
				snapshot.LocalStatus = "LOCAL MATCH"
			}
		}
		fmt.Print(ui.FormatText(snapshot))
		if snapshot.Message != "" {
			fmt.Println(snapshot.Message)
		}
		for _, probeError := range result.RelevantErrors(snapshot) {
			fmt.Fprintln(os.Stderr, "warning:", probeError)
		}
		if snapshot.LocalStatus == "CONTEXT-DRIFT" {
			return errors.New("managed context has drifted")
		}
		return nil
	case "config", "c", "catalog":
		return runConfig(args[1:])
	case "shell-init":
		return runShellInit(args[1:])
	case "init":
		return runInit(args[1:])
	case "backup":
		return runBackup(args[1:])
	case "restore":
		return runRestore(args[1:])
	case "reset":
		return runReset(args[1:])
	case "discover":
		return runDiscover(args[1:])
	case "list":
		if len(args) != 1 {
			return errors.New("usage: chop list")
		}
		return runList()
	case "k", "kubernetes", "i", "identity", "p", "project", "d", "docker", "m", "map":
		if len(args) != 1 {
			return errors.New("usage: chop config")
		}
		return runConfig(nil)
	case "w", "destination", "workspace":
		if len(args) != 1 {
			return errors.New("usage: chop workspace")
		}
		return runInteractive(ui.ScreenWorkspace)
	case "select":
		return runSelect(args[1:])
	case "shell":
		if len(args) != 2 {
			return errors.New("usage: chop shell <workspace>")
		}
		return runInDestination(args[1], nil)
	case "exec":
		return runExec(args[1:])
	case "auth":
		return runAuth(args[1:])
	case "link":
		if len(args) != 3 {
			return errors.New("usage: chop link <project> <identity>")
		}
		return runLink(args[1], args[2])
	case "version", "--version", "-v":
		if len(args) != 1 {
			return errors.New("usage: chop version")
		}
		fmt.Println("contexthop", version)
		return nil
	case "help", "--help", "-h":
		if len(args) != 1 {
			return errors.New("usage: chop help")
		}
		printHelp()
		return nil
	default:
		if strings.HasPrefix(args[0], "-") {
			return fmt.Errorf("unknown option %q; run `chop help`", args[0])
		}
		if len(args) != 1 {
			return errors.New("usage: chop shell <workspace>")
		}
		return runUse(args[0])
	}
}

func runSelect(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: chop select <kubernetes|identity|project|docker|workspace>")
	}
	switch args[0] {
	case "k", "kubernetes", "i", "identity", "p", "project", "d", "docker":
		return runConfig(nil)
	case "w", "workspace":
		return runInteractive(ui.ScreenWorkspace)
	default:
		return fmt.Errorf("unknown selection %q; use chop or chop config", args[0])
	}
}

func runDebug(args []string) (err error) {
	debugtrace.Enable()
	defer func() {
		debugtrace.Flush(os.Stderr)
		debugtrace.Disable()
	}()
	if len(args) > 1 {
		return errors.New("usage: chop debug [config|<workspace>]")
	}
	if len(args) == 0 {
		return runInteractive(ui.ScreenWorkspace)
	}
	if strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("unknown debug option %q", args[0])
	}
	switch args[0] {
	case "config", "c", "catalog", "m", "map", "k", "kubernetes", "i", "identity", "p", "project", "d", "docker":
		return runConfig(nil)
	case "w", "workspace":
		return runInteractive(ui.ScreenWorkspace)
	default:
		return runUse(args[0])
	}
}

func rejectNestedSession(args []string) error {
	if os.Getenv("CONTEXTHOP_CONTEXT") == "" || os.Getenv(session.ActivationFileEnv) != "" || !startsChildShell(args) {
		return nil
	}
	current := os.Getenv("CONTEXTHOP_CONTEXT")
	if manifest, err := state.LoadManifest(os.Getenv(state.SessionFileEnv)); err == nil {
		if manifest.KubernetesLabel != "" {
			current = manifest.KubernetesLabel
		}
	}
	if os.Getenv("CONTEXTHOP_IN_PLACE_SWITCH") == "1" {
		return fmt.Errorf("already in ContextHop session %q; use `chop` without a path to switch this session in place", current)
	}
	return fmt.Errorf("already in ContextHop session %q; run `exit` to return to your previous shell, then run `chop` again", current)
}

func startsChildShell(args []string) bool {
	if len(args) > 1 && (args[0] == "select" || args[0] == "debug") {
		return startsChildShell(args[1:])
	}
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "k", "kubernetes", "i", "identity", "p", "project", "d", "docker", "console", "completion", "_complete", "status", "config", "shell-init", "init", "backup", "restore", "reset", "discover", "list", "c", "catalog", "m", "map", "exec", "auth", "link", "version", "--version", "-v", "help", "--help", "-h":
		return false
	default:
		return true
	}
}

func runShellInit(args []string) error {
	if (len(args) != 1 && len(args) != 2) || args[0] != "zsh" || len(args) == 2 && args[1] != "--in-place" {
		return errors.New("usage: chop shell-init zsh [--in-place]")
	}
	if len(args) == 2 {
		binary, err := shellBinary()
		if err != nil {
			return err
		}
		fmt.Print(session.CurrentShellInit(binary))
		return nil
	}
	script, err := session.ShellInit("zsh")
	if err != nil {
		return err
	}
	fmt.Print(script)
	return nil
}

const (
	noProjectSelection    = "\x00__contexthop_no_project__"
	noKubernetesSelection = "\x00__contexthop_no_kubernetes__"
)
