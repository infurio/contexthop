package ui

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestTagCellRendersNamesAndSharedColours(t *testing.T) {
	tags := []Tag{{Name: "personal", Color: "#a78bfa"}, {Name: "work", Color: "#60a5fa"}}
	got := RenderTags(tags)
	if ansi.Strip(got) != "personal · work" || strings.Contains(got, "environment=") {
		t.Fatal(got)
	}
	if !strings.Contains(got, lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")).Render("personal")) {
		t.Fatal("missing user colour")
	}
	option := Option{Tags: tags, IdentityAccount: "developer@example.com"}
	model := listModel{dimension: "identity", width: 100, options: []Option{option}}
	row := model.detailRow(option, false)
	if !strings.Contains(ansi.Strip(row), "personal") || !strings.Contains(model.detailRow(Option{}, true), "TAGS") {
		t.Fatal(row)
	}
	if ansi.StringWidth(fitColumn(got, 10)) > 10 {
		t.Fatal("coloured tags overflow narrow cells")
	}
}

// Inspect the final selected/staged row, not just the isolated tag cell. The
// outer selection style previously stripped every configured tag colour.
func TestResourceRowSelectionPreservesTagColourAndLongName(t *testing.T) {
	for _, width := range []int{35, 80, 110, 180} {
		for _, dimension := range []string{"workspace", "identity", "project", "kubernetes", "docker"} {
			for _, staged := range []bool{false, true} {
				name := strings.Repeat("fictional-long-name-", 8)
				option := Option{Name: name, Label: name, IdentityAccount: name, ProjectID: name, KubernetesContext: name, DockerContext: name, Tags: []Tag{{Name: "demo", Color: "#e345ab"}}}
				m := listModel{dimension: dimension, width: width, resourceBrowser: true, composeSelection: true, options: []Option{option}}
				if staged {
					m.selectedName = name
					m.cursor = 1
				}
				body := resourceTableBody{model: m, options: []Option{option}, showHead: width >= 80}
				rows := body.Render(12)
				found := false
				for _, row := range rows {
					if strings.Contains(ansi.Strip(row), "demo") {
						found = true
						if !strings.Contains(row, lipgloss.NewStyle().Foreground(lipgloss.Color("#e345ab")).Render("demo")) {
							t.Fatalf("%s/%d staged=%v: tag colour lost: %q", dimension, width, staged, row)
						}
					}
				}
				if !found {
					t.Fatalf("%s/%d: tag clipped by name", dimension, width)
				}
			}
		}
	}
}
