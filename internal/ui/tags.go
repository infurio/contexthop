package ui

import (
	"charm.land/lipgloss/v2"
	"strings"
)

type Tag struct{ Name, Color string }

func RenderTags(tags []Tag) string {
	parts := make([]string, 0, len(tags))
	for _, tag := range tags {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color(tag.Color)).Render(tag.Name))
	}
	return strings.Join(parts, " · ")
}
