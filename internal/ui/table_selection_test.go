package ui

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestIdentityColumnsIgnoreSuppressedRowsAndPreserveStatus(t *testing.T) {
	visible := Option{Name: "work", Identity: true, IdentityAccount: "developer@team-a.example.com", Provider: "gcp", AuthStatus: "Sign-in required", Source: "GCP"}
	long := Option{Name: "service", Identity: true, IdentityAccount: strings.Repeat("service-account-", 9) + "@example.com", Provider: "gcp", AuthStatus: "Authenticated", Source: "GCP", Hidden: true}
	model := listModel{resourceBrowser: true, dimension: "identity", width: 120, options: []Option{visible, long}}
	baseline := model
	baseline.options = []Option{visible}
	if got, want := model.detailRow(visible, false), baseline.detailRow(visible, false); got != want {
		t.Fatalf("hidden account affects sizing:\n%s\n%s", got, want)
	}
	model.showHidden = true
	row := ansi.Strip(model.detailRow(long, false))
	if !strings.Contains(row, "Authenticated") || !strings.Contains(row, "gcp") || !strings.Contains(row, "GCP") {
		t.Fatalf("long account squeezed short status fields: %s", row)
	}
	model.filter = "team-a"
	if got, want := model.detailRow(visible, false), baseline.detailRow(visible, false); got != want {
		t.Fatal("filtered-out rows affect sizing")
	}
}

func TestStagedRowKeepsColorWhenCursorMovesAway(t *testing.T) {
	model := listModel{resourceBrowser: true, composeSelection: true, dimension: "docker", width: 100, height: 24, selectedName: "selected", cursor: 1, options: []Option{
		{Name: "selected", Docker: true, DockerContext: "selected"},
		{Name: "focused", Docker: true, DockerContext: "focused"},
	}}
	view := model.resourceBrowserView()
	selectedPrefix := strings.TrimSuffix(stagedStyle.Render("✓ selected"), "\x1b[m")
	cursorPrefix := strings.TrimSuffix(selectedStyle.Render("> focused"), "\x1b[m")
	if !strings.Contains(view, selectedPrefix) {
		t.Fatal("staged row lost persistent color")
	}
	if !strings.Contains(view, cursorPrefix) {
		t.Fatal("cursor lost its distinct highlight")
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > model.width {
			t.Fatal("highlight exceeds terminal width")
		}
	}
}
