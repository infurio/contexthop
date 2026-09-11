package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFooterPlanSearchAndStageLabels(t *testing.T) {
	for _, width := range []int{59, 60, 90, 164} {
		for _, searching := range []bool{false, true} {
			m := listModel{width: width, resourceBrowser: true, composeSelection: true, searching: searching, canApplyShell: true, selectedName: "item", dimension: "workspace"}
			plan := m.resourceFooterPlan(Option{Name: "item"}, true)
			lines := plan.Render()
			got := ansi.Strip(strings.Join(lines[:], "\n"))
			help := "[?]"
			if searching {
				help = "[F1] Help"
			}
			if !strings.Contains(got, help) {
				t.Fatal(width, searching, got)
			}
			if strings.Contains(got, "[space]") == searching {
				t.Fatal("stage hint does not match search mode", got)
			}
			if !searching {
				label := "Unstage"
				if m.contentWidth() < 60 {
					label = "Unset"
				}
				if !strings.Contains(got, label) {
					t.Fatal(width, got)
				}
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.contentWidth() {
					t.Fatal("footer overflow", line)
				}
			}
		}
	}
}

func TestFooterPlanOpenActionAndUnavailableSelection(t *testing.T) {
	m := listModel{width: 164, resourceBrowser: true, composeSelection: true, canApplyShell: true, dimension: "workspace"}
	lines := m.resourceFooterPlan(Option{Name: "item", OpenScreen: "docker"}, true).Render()
	if got := ansi.Strip(lines[0]); !strings.Contains(got, "[enter] Open") || strings.Contains(got, "Update shared") || strings.Contains(got, "Stage") {
		t.Fatal(got)
	}
	lines = m.resourceFooterPlan(Option{}, false).Render()
	if got := ansi.Strip(lines[0]); strings.Contains(got, "Update shared") || strings.Contains(got, "Stage") {
		t.Fatal(got)
	}
}
