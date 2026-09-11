package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/infurio/contexthop/internal/config"
	"golang.org/x/term"
)

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
