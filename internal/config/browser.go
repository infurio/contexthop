package config

import (
	"fmt"
	"strings"
)

// BrowserProfile refers to an existing local Chromium profile directory.
type BrowserProfile struct {
	App     string `yaml:"app,omitempty"`
	Profile string `yaml:"profile,omitempty"`
}

func (b BrowserProfile) Validate() error {
	if b == (BrowserProfile{}) {
		return nil
	}
	if b.App != "chrome" && b.App != "edge" {
		return fmt.Errorf("app must be chrome or edge")
	}
	if err := ValidateName(b.Profile); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if b.Profile == "." || b.Profile == ".." || strings.ContainsAny(b.Profile, "/\\") {
		return fmt.Errorf("profile must be a directory name such as Default or Profile 1")
	}
	return nil
}
