package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/session"
	"golang.org/x/term"
)

func configuredPromptPrefix() string {
	if cfg, err := loadConfig(); err != nil || cfg.DisplayMode() != "prompt" {
		return ""
	}
	return session.CurrentZshPromptPrefix()
}

func editConfig(path string, recovery ...string) error {
	source := path
	if len(recovery) > 0 {
		source = recovery[0]
	}
	original, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".contexthop-edit-*.yaml")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	keepRecovery := false
	defer func() {
		if !keepRecovery {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(original); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{"vi"}
	}
	command := exec.Command(parts[0], append(parts[1:], temporaryPath)...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		keepRecovery = true
		return fmt.Errorf("editor failed; edits retained at %s: %w", temporaryPath, err)
	}
	updated, err := config.Load(temporaryPath)
	if err != nil {
		keepRecovery = true
		return fmt.Errorf("edited configuration is invalid; original left unchanged. Edits retained at %s; retry with chop config edit <recovery-file>: %w", temporaryPath, err)
	}
	if err := config.Write(path, updated); err != nil {
		keepRecovery = true
		return fmt.Errorf("could not save configuration; edits retained at %s: %w", temporaryPath, err)
	}
	fmt.Println("Updated and validated", path)
	return nil
}

func configPath() (string, error) {
	if path := os.Getenv("CONTEXTHOP_CONFIG"); path != "" {
		if filepath.IsAbs(path) {
			return path, nil
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		return absolute, nil
	}
	return config.DefaultPath()
}

func isTerminal(file *os.File) bool {
	return file != nil && term.IsTerminal(int(file.Fd()))
}

func runDisplayConfig(path string, args []string) error {
	if len(args) > 1 || len(args) == 1 && args[0] != "summary" && args[0] != "prompt" && args[0] != "off" {
		return fmt.Errorf("usage: chop config display [summary|prompt|off]")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		fmt.Println(cfg.DisplayMode())
		return nil
	}
	plan, err := catalog.PlanDisplay(cfg, args[0])
	if err != nil {
		return err
	}
	if err = catalog.Apply(path, plan); err != nil {
		return err
	}
	fmt.Printf("Display mode %s. Integrated terminals update at their next prompt.\n", args[0])
	return nil
}
func configuredSummary() string {
	cfg, err := loadConfig()
	if err != nil || cfg.DisplayMode() != "summary" {
		return ""
	}
	return session.CurrentSummary(version)
}
