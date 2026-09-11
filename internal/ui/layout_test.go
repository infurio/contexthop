package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

type overflowingBody struct{}

func (overflowingBody) PreferredHeight() int { return 20 }
func (overflowingBody) Render(int) []string {
	return []string{"first\nescaped", "second", "third", "fourth", "fifth"}
}

func TestPageLayoutEnforcesModuleBounds(t *testing.T) {
	view := (pageLayout{
		Width: 20, Height: 6, Top: []string{"header"}, Body: overflowingBody{},
		Footer: [footerHeight]string{"action", "global"},
	}).Render()
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(view), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("layout rendered %d rows, want 6: %#v", len(lines), lines)
	}
	if !strings.Contains(lines[1], "first escaped") {
		t.Fatalf("embedded newline escaped its allocated row: %#v", lines)
	}
	if !strings.Contains(lines[4], "action") || !strings.Contains(lines[5], "global") {
		t.Fatalf("footer moved from final rows: %#v", lines)
	}
}
