package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidationProgressNonTerminalIsOnePlainLine(t *testing.T) {
	var output bytes.Buffer
	progress := newValidationProgress(&output, false)
	progress.Update("Checking project, Kubernetes")
	progress.Update("Waiting for Kubernetes")
	progress.Stop()
	if got := output.String(); got != "Checking project, Kubernetes…\n" {
		t.Fatalf("output = %q", got)
	}
	if strings.Contains(output.String(), "\x1b") {
		t.Fatal("non-terminal progress contains control sequences")
	}
}

func TestValidationProgressTerminalRewritesAndClears(t *testing.T) {
	var output bytes.Buffer
	progress := newValidationProgress(&output, true)
	progress.Update("Checking Kubernetes")
	progress.Stop()
	text := output.String()
	if !strings.Contains(text, "Checking Kubernetes · ") || !strings.HasSuffix(text, "\r\x1b[2K") {
		t.Fatalf("output = %q", text)
	}
}
