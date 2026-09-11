package main

import (
	"fmt"
	"os"

	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
)

func runSelectionLaunch(resolved resolver.Resolved, action string) error {
	return runResolvedMode(resolved, nil, action == "launch-shell", action == "default-shell")
}

func syncSharedDefault(path string) error {
	if os.Getenv(session.ScopeEnv) == "local" {
		return nil
	}
	revision := os.Getenv(session.SharedRevisionEnv)
	prepared, next, err := session.FollowShared(revision)
	if err != nil {
		return fmt.Errorf("follow shared config: %w; current context retained", err)
	}
	if prepared == nil {
		if next == "" && revision != "" {
			return os.WriteFile(path, []byte("_chop_restore_baseline\n"), 0600)
		}
		return nil
	}
	prepared.SetScope("shared", next)
	if err := prepared.WriteActivation(path); err != nil {
		prepared.Close()
		return err
	}
	return nil
}

func runSharedDefault(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: chop shared <use|clear>")
	}
	switch args[0] {
	case "clear":
		return session.ClearShared()
	case "use", "follow":
		path := os.Getenv(session.ActivationFileEnv)
		if path == "" {
			return fmt.Errorf("enable Zsh integration first: eval \"$(chop shell-init zsh --in-place)\"")
		}
		return os.WriteFile(path, []byte("export CONTEXTHOP_SCOPE=shared\nexport CONTEXTHOP_SHARED_REVISION=follow\n"), 0600)
	default:
		return fmt.Errorf("usage: chop shared <use|clear>")
	}
}
