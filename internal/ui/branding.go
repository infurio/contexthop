package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Branding uses spare footer space; it never displaces an action or Help.
func brandFooter(shortcuts, version string, width int) string {
	label := "ContextHop"
	if version != "" {
		label += " · v" + strings.TrimPrefix(singleLine(version), "v")
	}
	gap := width - lipgloss.Width(shortcuts) - lipgloss.Width(label)
	if gap < 2 {
		return shortcuts
	}
	return shortcuts + strings.Repeat(" ", gap) + dimStyle.Render(label)
}
