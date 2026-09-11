package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type helpBinding struct{ key, description string }
type helpSection struct {
	title    string
	bindings []helpBinding
}

const helpIntroduction = "Chop brings cloud identities, projects, Kubernetes and Docker contexts together. Choose the resources you need, then apply them to your shell or save the combination as a workspace."

func (m AppModel) helpSections() []helpSection {
	frame := m.current()
	common := helpSection{"Application", []helpBinding{{"✓ Green", "Staged with Space; stays fixed while browsing"}, {"› Blue", "Cursor selection for an unstaged component"}, {"— Grey", "No component selected"}, {"F1", "Help"}, {"Ctrl+C", "Quit the application"}}}
	if m.working {
		bindings := []helpBinding{}
		if m.workCancel != nil {
			bindings = append(bindings, helpBinding{"Esc", "Stop operation and retain completed results"})
		}
		return []helpSection{{"Operation", bindings}, common}
	}
	if frame.picker.Input != nil {
		return []helpSection{{"Text input", []helpBinding{{"Enter", "Continue"}, {"Esc", "Cancel this field"}, {"← / →", "Move text cursor"}, {"Home / Ctrl+A", "Start of field"}, {"End / Ctrl+E", "End of field"}, {"Ctrl+U", "Clear text before cursor"}, {"Backspace / Delete", "Delete previous / next character"}, {"Printable keys", "Insert text, including ?"}}}, common}
	}
	if frame.picker.ReadOnlyText != "" {
		return []helpSection{{"Details", []helpBinding{{"↑ / ↓", "Scroll"}, {"PgUp / PgDown", "Scroll a page"}, {"Home / End", "Start / end"}, {"Esc / q", "Close details"}}}, common}
	}
	if frame.screen == screenOptions {
		bindings := []helpBinding{{"Enter", "Run highlighted option"}, {"Esc", "Close Options without changing selection"}}
		for _, action := range frame.picker.optionsCommands {
			bindings = append(bindings, helpBinding{action.helpKey(), action.Label})
		}
		return []helpSection{{"Options", bindings}, common}
	}
	navigation := helpSection{"Navigate", []helpBinding{{"↑ / ↓", "Move through rows"}, {"Home / End", "First / last row"}, {"PgUp / PgDown", "Page through rows"}}}
	if frame.picker.ResourceBrowser {
		common.bindings = append(common.bindings, helpBinding{"Ctrl+U", "Clear search"})

	}

	if m.acceptsText() {
		bindings := []helpBinding{{"Printable keys", "Filter this list"}, {"Backspace", "Remove a search character"}, {"Enter", "Select the highlighted result"}, {"Esc", "Close search or go back"}, {"Tab / Shift+Tab", "Finish filtering; keep the query"}}

		if frame.picker.ModalActions && !frame.picker.ResourceBrowser {
			bindings[2].description = "Finish filtering"
		}
		if frame.picker.ResourceBrowser {
			bindings[3].description = "Finish editing; keep filter (Esc again clears it)"
			bindings[len(bindings)-1].description = "Switch resource tabs; keep each filter"
			bindings = append(bindings, helpBinding{"← / →", "Previous / next resource tab; keep each filter"})
			rows := filteredPickerOptions(frame.picker, frame.filter)
			selected := Option{}
			if len(rows) > 0 {
				selected = rows[min(frame.cursor, len(rows)-1)]
			}
			if m.composeSelection || len(rows) == 0 || frame.picker.DisableEnter {
				bindings = append(bindings[:2], bindings[3:]...)
			}
			for _, action := range m.pickerModel(frame).browserCommands(selected, len(rows) > 0) {
				if action.availableDuringSearch() && action.Key != "" {
					bindings = append(bindings, helpBinding{action.helpKey(), action.Label})
				}
			}
		}
		return []helpSection{navigation, {"Search", bindings}, common}
	}
	if !frame.picker.ResourceBrowser {
		bindings := []helpBinding{{"Esc", "Go back or close dialog"}}
		if frame.picker.HideSearch && frame.picker.OperationDetails != "" {
			bindings = append(bindings, helpBinding{"i", "Full details"})
		}
		if frame.picker.ShowSelectedInfo {
			bindings = append(bindings, helpBinding{"i", "Highlighted item details"})
		}
		if !frame.picker.DisableEnter && len(frame.picker.Options) > 0 {
			bindings = append(bindings, helpBinding{"Enter", "Choose highlighted option"})
		}
		if frame.picker.ModalActions && !frame.picker.HideSearch {
			bindings = append(bindings, helpBinding{"/", "Search"})
		}
		options := filteredPickerOptions(frame.picker, frame.filter)
		if len(options) > 0 {
			for _, action := range options[min(frame.cursor, len(options)-1)].Actions {
				bindings = append(bindings, helpBinding{action.Key, action.Label})
			}
		}
		return []helpSection{navigation, {"This dialog", bindings}, common}
	}
	navigation.bindings = append(navigation.bindings, helpBinding{"Tab / Shift+Tab", "Next / previous resource tab"}, helpBinding{"← / →", "Previous / next resource tab"}, helpBinding{"Shift+W/I/P/K/D", "Workspaces / Identities / Projects / Kubernetes / Docker"}, helpBinding{"/", "Start or resume filter editing"}, helpBinding{"Esc", "Clear search, then selections one level at a time"})
	options := filteredPickerOptions(frame.picker, frame.filter)
	selected := Option{}
	if len(options) > 0 {
		selected = options[min(frame.cursor, len(options)-1)]
	}

	actions := helpSection{"Available actions", nil}
	for _, action := range m.pickerModel(frame).browserCommands(selected, len(options) > 0) {
		key := action.helpKey()
		if key == "" {
			continue
		}
		actions.bindings = append(actions.bindings, helpBinding{key, action.Label})
	}
	common.bindings = append([]helpBinding{{"?", "Help"}, {"q", "Quit the application"}}, common.bindings...)
	return []helpSection{navigation, actions, common}
}

// Plain text is also useful for checking shortcut coverage without presentation.
func (m AppModel) helpText() string {
	var parts []string
	parts = append(parts, helpIntroduction)
	for _, section := range m.helpSections() {
		if len(section.bindings) == 0 {
			continue
		}
		parts = append(parts, section.title)
		for _, binding := range section.bindings {
			parts = append(parts, binding.key+"  "+binding.description)
		}
	}
	return strings.Join(parts, "\n")
}

func renderHelpSection(section helpSection, width int) []string {
	if len(section.bindings) == 0 {
		return nil
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#65d6c5"))
	description := lipgloss.NewStyle().Foreground(lipgloss.Color("#d8e2ec"))
	keys := keyStyle.Foreground(lipgloss.Color("#69b9ff"))
	lines := []string{title.Render(strings.ToUpper(section.title)), ""}
	keyWidth := 16
	for _, binding := range section.bindings {
		if width < 38 || ansi.StringWidth(binding.key) > keyWidth {
			lines = append(lines, keys.Render(binding.key))
			for _, line := range sectionLines(wrapText(binding.description, max(1, width-2))) {
				lines = append(lines, "  "+description.Render(line))
			}
			lines = append(lines, "")
			continue
		}
		wrapped := sectionLines(wrapText(binding.description, width-keyWidth-2))
		for i, line := range wrapped {
			key := strings.Repeat(" ", keyWidth)
			if i == 0 {
				key = keys.Render(binding.key) + strings.Repeat(" ", max(0, keyWidth-ansi.StringWidth(binding.key)))
			}
			lines = append(lines, key+"  "+description.Render(line))
		}
	}
	return append(lines, "")
}

func (m AppModel) helpWidth() int {
	if m.width <= 0 {
		return 100
	}
	return max(1, min(132, m.width-4))
}

func (m AppModel) helpViewport() ([]string, int) {
	width := m.helpWidth()
	sections := m.helpSections()
	var lines []string
	if width >= 108 {
		columnWidth := (width - 5) / 2
		var left, right []string
		for _, section := range sections {
			if len(left) <= len(right) {
				left = append(left, renderHelpSection(section, columnWidth)...)
			} else {
				right = append(right, renderHelpSection(section, columnWidth)...)
			}
		}
		for i := 0; i < max(len(left), len(right)); i++ {
			a, b := "", ""
			if i < len(left) {
				a = left[i]
			}
			if i < len(right) {
				b = right[i]
			}
			lines = append(lines, a+strings.Repeat(" ", max(0, columnWidth-ansi.StringWidth(a)))+"     "+b)
		}
	} else {
		for _, section := range sections {
			lines = append(lines, renderHelpSection(section, width)...)
		}
	}
	height := max(1, m.height-len(m.helpHeader())-2)
	if m.height <= 0 {
		height = len(lines)
	}
	return lines, height
}

func (m AppModel) helpHeader() []string {
	width := m.helpWidth()
	lines := []string{headingStyle.Render("Chop  /  Keyboard shortcuts"), ""}
	lines = append(lines, sectionLines(lipgloss.NewStyle().Foreground(lipgloss.Color("#aebfce")).Render(wrapText(helpIntroduction, width)))...)
	return append(lines, "", borderStyle.Render(strings.Repeat("─", width)), "")
}

func (m AppModel) helpPanel(_ string) string {
	lines, height := m.helpViewport()
	offset := min(m.helpOffset, max(0, len(lines)-height))
	visible := lines[offset:min(len(lines), offset+height)]
	top := append(m.helpHeader(), visible...)
	footer := shortcut("? / esc", "Close help")
	if len(lines) > height {
		footer = shortcut("↑/↓", "Scroll") + "   " + footer
	}
	width := m.helpWidth()
	if ansi.StringWidth(footer) > width {
		footer = keyStyle.Render("↑/↓ scroll · ?/esc close")
	}
	return (pageLayout{Width: width, Height: m.height, Top: top, Footer: [footerHeight]string{"", footer}}).Render()
}
