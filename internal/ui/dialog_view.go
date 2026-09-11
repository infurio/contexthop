package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	minimumDialogTerminalWidth = 64
	minimumDialogHeight        = 14
	maximumDialogWidth         = 78
)

// dialogView keeps the resource browser visible behind a child workflow. This
// makes mapping, add, input, and confirmation steps read as operations on the
// highlighted row instead of unrelated full-screen pages.
func (m AppModel) dialogView(parent, frame navigationFrame) tea.View {
	terminalWidth := m.width
	if terminalWidth <= 0 {
		terminalWidth = defaultTerminalWidth
	}
	terminalHeight := m.height
	if terminalHeight <= 0 {
		terminalHeight = 28
	}

	// On small terminals the full page remains more usable than a cramped layer.
	if terminalWidth < minimumDialogTerminalWidth || terminalHeight < minimumDialogHeight {
		if frame.picker.ReadOnlyText != "" {
			view := tea.NewView(m.readOnlyPage(frame, (listModel{width: terminalWidth}).contentWidth(), terminalHeight))
			view.AltScreen = true
			return view
		}
		if frame.picker.Input != nil {
			return m.inputView(frame)
		}
		return m.pickerModel(frame).View()
	}

	backgroundModel := m.pickerModel(parent)
	backgroundModel.width = terminalWidth
	backgroundModel.height = terminalHeight
	background := backgroundModel.resourceBrowserView()

	panelWidth := min(maximumDialogWidth, terminalWidth-8)
	if frame.picker.HideSearch || frame.picker.Input != nil {
		panelWidth = min(74, terminalWidth-8)
	}
	contentWidth := max(1, panelWidth-4)
	render := func(height int) string {
		var content string
		if frame.picker.ReadOnlyText != "" {
			content = m.readOnlyPage(frame, contentWidth, height)
		} else if frame.picker.Input != nil {
			content = m.inputPage(frame, contentWidth, height)
		} else {
			foregroundModel := m.pickerModel(frame)
			foregroundModel.width = contentWidth + pageGutterWidth + pageRightMargin
			foregroundModel.height = height
			content = foregroundModel.pickerPage(true)
		}
		// Page layouts include a terminal gutter; the dialog owns its padding.
		lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
		for index := range lines {
			lines[index] = strings.TrimPrefix(lines[index], " ")
		}
		return strings.Join(lines, "\n")
	}
	natural := render(0)
	contentHeight := min(terminalHeight-6, max(6, lipgloss.Height(natural)))
	content := render(contentHeight)

	// Parent shortcuts are inactive while the dialog has focus.
	backgroundLines := strings.Split(strings.TrimSuffix(background, "\n"), "\n")
	for index := max(0, len(backgroundLines)-footerHeight); index < len(backgroundLines); index++ {
		backgroundLines[index] = ""
	}
	background = strings.Join(backgroundLines, "\n")

	// Inherit the terminal background throughout: nested field styles reset
	// their attributes, so an outer-only fill would leave mismatched patches.
	// The overlay replaces every panel cell; the dimmed browser cannot bleed through.
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Width(panelWidth).
		Padding(0, 1).
		Render(strings.TrimSuffix(content, "\n"))
	view := tea.NewView(overlayPanel(background, panel, terminalWidth, terminalHeight))
	view.AltScreen = true
	return view
}

func overlayPanel(background, panel string, width, height int) string {
	backgroundLines := strings.Split(strings.TrimSuffix(ansi.Strip(background), "\n"), "\n")
	for len(backgroundLines) < height {
		backgroundLines = append(backgroundLines, "")
	}
	if len(backgroundLines) > height {
		backgroundLines = backgroundLines[:height]
	}
	for index, line := range backgroundLines {
		line = lipgloss.NewStyle().Width(width).Render(fitColumn(line, width))
		backgroundLines[index] = dimStyle.Faint(true).Render(line)
	}

	panelLines := strings.Split(strings.TrimSuffix(panel, "\n"), "\n")
	panelWidth := 0
	for _, line := range panelLines {
		panelWidth = max(panelWidth, lipgloss.Width(line))
	}
	panelWidth = min(panelWidth, width)
	for index, line := range panelLines {
		panelLines[index] = lipgloss.NewStyle().Width(panelWidth).Render(fitColumn(line, panelWidth))
	}

	startY := max(0, (height-len(panelLines))/2)
	startX := max(0, (width-panelWidth)/2)
	for index, line := range panelLines {
		y := startY + index
		if y >= len(backgroundLines) {
			break
		}
		backgroundLine := backgroundLines[y]
		left := ansi.Cut(backgroundLine, 0, startX)
		right := ansi.Cut(backgroundLine, startX+panelWidth, width)
		backgroundLines[y] = left + line + right
	}
	return strings.Join(backgroundLines, "\n") + "\n"
}
