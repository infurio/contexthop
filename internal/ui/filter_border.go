package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m listModel) resourcePanelBottom(title string, matches, start, end int, scrolling bool) string {
	width := m.contentWidth()
	if width < 10 {
		return resourceListBottom(width)
	}
	unfiltered := m
	unfiltered.filter = ""
	total := len(unfiltered.filteredOptions())
	units := map[string]string{"workspace": "workspaces", "identity": "identities", "project": "projects", "kubernetes": "Kubernetes targets", "docker": "Docker contexts"}[m.dimension]
	count := title
	if units != "" {
		count = fmt.Sprintf("%d %s", matches, units)
	}
	filtered := strings.TrimSpace(m.filter) != ""
	if filtered {
		if units == "" {
			units = "resources"
		}
		count = fmt.Sprintf("%d of %d %s", matches, total, units)
	}
	label := m.resourcePanelCount(count)
	if scrolling {
		if units != "" {
			label = fmt.Sprintf("%d–%d of %d %s", start+1, end, matches, units)
			if filtered {
				label += fmt.Sprintf(" · %d total", total)
			}
			label = m.resourcePanelCount(label)
		} else {
			label = fmt.Sprintf("%d–%d of %d · %s", start+1, end, matches, label)
		}
		label += " · ↑/↓ scroll"
	}
	input := ""
	edge := borderStyle
	if !m.hideSearch && (m.searching || m.filter != "") {
		// Keep short queries whole. For long queries, shorten count metadata
		// first, then show the query's tail so the insertion point stays visible.
		minimumInput := min(12, 2+ansi.StringWidth(m.filter)+1)
		for _, candidate := range []string{label, count, fmt.Sprintf("%d/%d", matches, total), ""} {
			label = candidate
			if width-10-ansi.StringWidth(label) >= minimumInput {
				break
			}
		}
		input = m.borderFilterInput(max(1, width-10-ansi.StringWidth(label)))
		if m.searching {
			edge = valueStyle
		}
	}
	label = fitColumn(singleLine(label), width-6)
	left := ""
	if input != "" {
		left = edge.Render("─ ") + input + " "
	}
	right := ""
	if label != "" {
		right = dimStyle.Render(" " + label + " ")
	}
	fill := max(0, width-3-lipgloss.Width(left)-lipgloss.Width(right))
	return edge.Render("└") + left + edge.Render(strings.Repeat("─", fill)) + right + edge.Render("─┘")
}

func (m listModel) borderFilterInput(width int) string {
	prompt, queryStyle := dimStyle, dimStyle
	if m.searching {
		prompt, queryStyle = titleStyle, headingStyle.Bold(false)
	}
	if width <= 2 {
		text := "/"
		if m.searching {
			text += "▏"
		}
		return prompt.Render(fitColumn(text, width))
	}
	budget := width - 2
	query := singleLine(m.filter)
	if m.searching {
		query += "▏"
	}
	if ansi.StringWidth(query) > budget {
		end := ansi.StringWidth(query)
		if budget > 1 {
			query = "…" + ansi.Cut(query, end-budget+1, end)
		} else {
			query = ansi.Cut(query, end-1, end)
		}
	}
	input := prompt.Render("/ ") + queryStyle.Render(query)
	if m.searching && m.filter == "" && budget >= ansi.StringWidth("▏ type to filter") {
		input += dimStyle.Render(" type to filter")
	}
	if m.modalActions {
		hints := []string{"  /: edit · esc: clear", "  /: edit"}
		if m.searching {
			hints = []string{"  esc: keep"}
		}
		for _, hint := range hints {
			if ansi.StringWidth(input)+ansi.StringWidth(hint) <= width {
				input += dimStyle.Render(hint)
				break
			}
		}
	}
	return input
}
