package ui

import (
	"charm.land/lipgloss/v2"
	"strings"
)

// Semantic colors follow the supplied k9s skin: aqua data, blue controls,
// white headings, and muted metadata on black. Resource kinds share a palette.
var (
	backgroundColor     = lipgloss.Color("#000000")
	bodyColor           = lipgloss.Color("#5f9ea0")
	accentColor         = lipgloss.Color("#1e90ff")
	dataColor           = lipgloss.Color("#00ffff")
	mutedColor          = lipgloss.Color("#708090")
	titleStyle          = lipgloss.NewStyle().Bold(true).Foreground(dataColor)
	headingStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff"))
	labelStyle          = lipgloss.NewStyle().Foreground(bodyColor).Width(14)
	valueStyle          = lipgloss.NewStyle().Foreground(dataColor)
	dimStyle            = lipgloss.NewStyle().Foreground(mutedColor)
	borderStyle         = lipgloss.NewStyle().Foreground(accentColor)
	keyStyle            = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	selectedStyle       = lipgloss.NewStyle().Bold(true).Foreground(backgroundColor).Background(dataColor)
	stagedStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#98fb98")).Background(lipgloss.Color("#103528"))
	stagedCursorStyle   = selectedStyle.Background(stagedStyle.GetForeground())
	hiddenStyle         = lipgloss.NewStyle().Foreground(mutedColor)
	hiddenSelectedStyle = hiddenStyle.Bold(true).Background(lipgloss.Color("#10282a"))
	warnStyle           = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff8c00"))
	dangerStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff4500"))
)

func fieldStyle(kind, value string) lipgloss.Style {
	if value == "" || value == "—" || value == "none" || strings.HasPrefix(value, "No ") {
		return dimStyle
	}
	switch strings.ToLower(kind) {
	case "provider", "auth":
		return lipgloss.NewStyle().Foreground(bodyColor)
	case "status":
		if value == "Unsaved" || value == "Unmapped" {
			return warnStyle
		}
	}
	return valueStyle
}
