package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// A text viewport has a scroll offset, never selectable rows or a cursor.
type readOnlyBody struct {
	lines  []string
	offset int
}

func (body readOnlyBody) PreferredHeight() int { return len(body.lines) }
func (body readOnlyBody) Render(height int) []string {
	if height <= 0 {
		return nil
	}
	start := min(body.offset, max(0, len(body.lines)-height))
	return body.lines[start:min(len(body.lines), start+height)]
}

func (m AppModel) readOnlyPage(frame navigationFrame, width, height int) string {
	lines := sectionLines(wrapText(frame.picker.ReadOnlyText, max(1, width)))
	return (pageLayout{Width: width, Height: height,
		Top:        []string{headingStyle.Render(fitColumn(frame.picker.Title, width)), ""},
		Body:       readOnlyBody{lines: lines, offset: frame.textOffset},
		FooterRows: 1,
		Footer:     [footerHeight]string{shortcutRow(width, shortcut("↑/↓", "Scroll"), shortcut("esc", "Close"))},
	}).Render()
}

func (m AppModel) updateReadOnlyKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	frame := &m.stack[len(m.stack)-1]
	width, height := m.width, m.height
	if width <= 0 {
		width = defaultTerminalWidth
	}
	if height <= 0 {
		height = 28
	}
	contentWidth := (listModel{width: width}).contentWidth()
	if _, dialog := m.resourceBrowserParent(); dialog && width >= minimumDialogTerminalWidth && height >= minimumDialogHeight {
		contentWidth = min(74, width-8) - 4
		natural := len(sectionLines(wrapText(frame.picker.ReadOnlyText, contentWidth))) + 3
		height = min(height-6, max(6, natural))
	}
	lines := sectionLines(wrapText(frame.picker.ReadOnlyText, max(1, contentWidth)))
	page := max(1, height-3)
	limit := max(0, len(lines)-page)
	frame.textOffset = min(frame.textOffset, limit)
	switch strings.ToLower(message.String()) {
	case "esc", "q":
		if len(m.stack) > 1 {
			m.draft = cloneDraft(frame.draftBefore)
			m.stack = m.stack[:len(m.stack)-1]
		}
	case "up":
		frame.textOffset = max(0, frame.textOffset-1)
	case "down":
		frame.textOffset = min(limit, frame.textOffset+1)
	case "pgup":
		frame.textOffset = max(0, frame.textOffset-page)
	case "pgdown":
		frame.textOffset = min(limit, frame.textOffset+page)
	case "home":
		frame.textOffset = 0
	case "end":
		frame.textOffset = limit
	}
	return m, nil
}
