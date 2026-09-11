package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTabBorderSpacingAndSelectionColors(t *testing.T) {
	line := resourcePanelTabs("project", 120)[0]
	if !strings.HasPrefix(ansi.Strip(line), "┌── Workspaces ─── Identities ──[ Projects ]── Kubernetes ─── Docker ─") {
		t.Fatal("tab spacing", ansi.Strip(line))
	}
	for _, width := range []int{35, 80, 120} {
		for _, tab := range applicationTabs {
			for _, active := range []Screen{ScreenProject, tab.screen} {
				for _, staged := range []bool{false, true} {
					draft := Draft{}
					if staged {
						draft[tab.screen] = "selected-resource"
					}
					line := resourcePanelTabs(string(active), width, draft)[0]
					label := tab.label
					if width == 35 {
						label = tab.key
					}
					pos := strings.Index(line, label)
					if pos < 0 {
						t.Fatal("missing tab", label, line)
					}
					want := "112,128,144"
					if active == tab.screen {
						want = "255,255,255"
					}
					if staged {
						want = "115,155,121"
						if active == tab.screen {
							want = "152,251,152"
						}
					}
					fg, bg := rowColoursAt(line, pos)
					if fg != want || bg != "" || ansi.StringWidth(line) != width {
						t.Fatalf("%s active=%s staged=%t width=%d: fg=%s bg=%s", tab.screen, active, staged, width, fg, bg)
					}
				}
			}
		}
	}
}
