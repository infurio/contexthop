package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Tabs share the panel's top border. Reserve the active brackets in every
// cell so switching tabs changes emphasis without moving the labels.
func resourcePanelTabs(active string, width int, selections ...Draft) []string {
	labels := make([]string, len(applicationTabs))
	total := 4 + len(labels) - 1 // corners, edge strokes, and separators
	for i, tab := range applicationTabs {
		labels[i] = tab.label
		total += lipgloss.Width(tab.label) + 4
	}
	if total > width {
		total = 4 + len(labels) - 1
		for i, tab := range applicationTabs {
			labels[i] = tab.key
			total += len(tab.key) + 4
		}
	}
	cell := func(label string, screen Screen, selected bool) string {
		staged := len(selections) > 0 && selections[0][screen] != ""
		labelStyle, bracketStyle := dimStyle, valueStyle
		if selected {
			labelStyle = headingStyle
		}
		if staged {
			labelStyle = labelStyle.Foreground(lipgloss.Color("#739b79"))
			if selected {
				labelStyle = headingStyle.Foreground(stagedStyle.GetForeground())
				bracketStyle = valueStyle.Foreground(stagedStyle.GetForeground())
			}
		}
		if selected {
			return bracketStyle.Render("[ ") + labelStyle.Render(label) + bracketStyle.Render(" ]")
		}
		return borderStyle.Render("─ ") + labelStyle.Render(label) + borderStyle.Render(" ─")
	}
	parts := []string{}
	if total > width {
		if width < 9 {
			return []string{resourceListTop("", width)}
		}
		parts = append(parts, cell(fitColumn(tabFor(Screen(active)).label, width-8), Screen(active), true))
	} else {
		for i, tab := range applicationTabs {
			parts = append(parts, cell(labels[i], tab.screen, string(tab.screen) == active))
		}
	}
	line := borderStyle.Render("┌─") + strings.Join(parts, borderStyle.Render("─"))
	hint := " Tab / ⇧Tab "
	remaining := width - lipgloss.Width(line) - 1
	if remaining >= lipgloss.Width(hint)+2 {
		line += borderStyle.Render(strings.Repeat("─", remaining-lipgloss.Width(hint)-1)) + dimStyle.Render(hint) + borderStyle.Render("─")
	} else {
		line += borderStyle.Render(strings.Repeat("─", max(0, remaining)))
	}
	return []string{line + borderStyle.Render("┐")}
}

func (m listModel) resourcePanelCount(title string) string {
	if m.dimension == "workspace" {
		for _, option := range m.options {
			if option.MatchesSelection && option.Name != m.selectedName {
				title += " · ≈ same resources"
				break
			}
		}
	}
	hidden := 0
	for _, option := range m.options {
		if option.Hidden && optionHasDimension(option, m.dimension) {
			hidden++
		}
	}
	if hidden > 0 {
		visibility := "hidden"
		if m.showHidden {
			visibility = "hidden shown"
		}
		title += fmt.Sprintf(" · %d %s", hidden, visibility)
	}
	return title
}

func (m listModel) resourcePanelHeader() []string {
	return resourcePanelTabs(m.dimension, m.contentWidth(), m.tabSelection)
}
