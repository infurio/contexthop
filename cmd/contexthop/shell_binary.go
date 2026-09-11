package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func shellBinary() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return stableShellBinary(executable, os.Args[0]), nil
}

// Keep Homebrew's public symlink across upgrades, but only when it refers to
// this executable. Never switch to an unrelated chop found on PATH.
func stableShellBinary(executable, invoked string) string {
	current, err := os.Stat(executable)
	if err != nil {
		return executable
	}
	matches := func(candidate string) bool {
		info, err := os.Stat(candidate)
		return err == nil && os.SameFile(current, info)
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err == nil {
		// <prefix>/Cellar/contexthop/<version>/bin/chop
		version := filepath.Dir(filepath.Dir(resolved))
		formula := filepath.Dir(version)
		cellar := filepath.Dir(formula)
		if filepath.Base(formula) == "contexthop" && filepath.Base(cellar) == "Cellar" {
			candidate := filepath.Join(filepath.Dir(cellar), "bin", "chop")
			if matches(candidate) {
				return candidate
			}
		}
	}
	if candidate, err := exec.LookPath(invoked); err == nil {
		if absolute, err := filepath.Abs(candidate); err == nil && matches(absolute) {
			return absolute
		}
	}
	return executable
}
