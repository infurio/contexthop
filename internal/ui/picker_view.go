package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type pickerListBody struct {
	model     listModel
	options   []Option
	showHead  bool
	showCount bool
}

func (body pickerListBody) PreferredHeight() int {
	if body.model.actionRows {
		lines, _, _ := body.actionLines()
		return len(lines)
	}
	heading := 0
	if body.showHead {
		heading = 1
	}
	if body.showCount && len(body.options) > 10 {
		heading++
	}
	return heading + max(1, min(10, len(body.options)))
}

func (body pickerListBody) Render(height int) []string {
	if height <= 0 {
		return nil
	}
	if body.model.actionRows {
		lines, selectedStart, selectedEnd := body.actionLines()
		if len(body.options) > 0 && body.options[0].MenuGroup != "" && len(lines) > height && height > 2 {
			capacity := height - 1
			start := max(0, selectedEnd-capacity)
			start = min(start, selectedStart)
			visible := append([]string{}, lines[start:min(len(lines), start+capacity)]...)
			return append(visible, dimStyle.Render(fitColumn("↑/↓ More options", body.model.contentWidth())))
		}
		start := max(0, selectedEnd-height)
		start = min(start, selectedStart)
		return lines[start:min(len(lines), start+height)]
	}
	lines := make([]string, 0, height)
	if body.showHead {
		lines = append(lines, dimStyle.Render("  "+body.model.detailRow(Option{}, true)))
	}
	capacity := max(0, height-len(lines))
	count := body.showCount && len(body.options) > capacity && capacity > 1
	if count {
		capacity--
	}
	start, end := visibleListRange(len(body.options), body.model.cursor, capacity)
	if len(body.options) > 0 && capacity > 0 {
		for index := start; index < end; index++ {
			option := body.options[index]
			cursor := "  "
			style := lipgloss.NewStyle()
			if index == body.model.cursor {
				cursor = keyStyle.Render("> ")
				style = selectedStyle
			}
			line := body.model.compactRow(option)
			if body.model.compactDialog && !body.showHead {
				line = option.Label
				if option.Detail != "" {
					line += " · " + option.Detail
				}
				line = fitColumn(line, max(1, body.model.contentWidth()-2))
			}
			if body.showHead {
				line = body.model.detailRow(option, false)
			}
			if index == body.model.cursor {
				line = ansi.Strip(line)
			}
			lines = append(lines, cursor+style.Render(line))
		}
	} else if capacity > 0 && (!body.model.hideSearch || len(body.model.options) > 0) {
		lines = append(lines, warnStyle.Render(body.model.emptyListMessage()))
		end = 1
	}
	for rendered := end - start; len(lines) < height && rendered < capacity; rendered++ {
		lines = append(lines, "")
	}
	if count {
		lines = lines[:min(len(lines), height-1)]
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  %d–%d of %d · ↑/↓ more", start+1, end, len(body.options))))
	}
	return lines
}

// Action menus give descriptions their own lines and keep focus on the title.
func (body pickerListBody) actionLines() ([]string, int, int) {
	var lines []string
	selectedStart, selectedEnd := 0, 0
	width := max(1, body.model.contentWidth()-2)
	for index, option := range body.options {
		if option.MenuGroup != "" {
			if index == 0 || option.MenuGroup != body.options[index-1].MenuGroup {
				if index > 0 {
					lines = append(lines, "")
				}
				lines = append(lines, dimStyle.Render(fitColumn(option.MenuGroup, width)))
			}
		} else if index > 0 {
			lines = append(lines, "")
		}
		start := len(lines)
		marker, style := "  ", headingStyle
		if index == body.model.cursor {
			marker, style = keyStyle.Render("> "), headingStyle.Foreground(accentColor)
		}
		label := fitColumn(option.Label, width)
		if option.MenuGroup != "" {
			label = menuShortcutLabel(option.Label, option.MenuShortcut, width)
		}
		lines = append(lines, marker+style.Render(label))
		// Options show the focused action's explanation without crowding out other actions.
		if option.Detail != "" && (option.MenuGroup == "" || index == body.model.cursor) {
			for _, line := range strings.Split(wrapText(option.Detail, width), "\n") {
				lines = append(lines, "  "+dimStyle.Render(line))
			}
		}
		if index == body.model.cursor {
			selectedStart, selectedEnd = start, len(lines)
		}
	}
	return lines, selectedStart, selectedEnd
}

func (m listModel) standardPickerView() string { return m.pickerPage(false) }

func (m listModel) pickerPage(modal bool) string {
	title := firstNonEmptyUI(m.pickerTitle, "Select workspace")
	top := []string{}
	if modal {
		parts := strings.Split(title, " › ")
		top = append(top, headingStyle.Render(wrapText(parts[len(parts)-1], m.contentWidth())))
		if len(parts) > 1 {
			context := parts[:len(parts)-1]
			if context[0] == "Catalog" {
				context = context[1:]
			}
			if len(context) > 0 {
				top = append(top, dimStyle.Render(wrapText(strings.Join(context, " · "), m.contentWidth())))
			}
		}
	} else {
		top = sectionLines(headingStyle.Render(wrapText(title, m.contentWidth())))
	}
	top = sectionLines(strings.Join(top, "\n"))
	top = append(top, "")
	if m.pickerDescription != "" {
		top = append(top, sectionLines(wrapText(m.pickerDescription, m.contentWidth()))...)
		top = append(top, "")
	}
	if len(m.contextFields) > 0 {
		labelWidth := 0
		for _, field := range m.contextFields {
			labelWidth = max(labelWidth, lipgloss.Width(field.Label))
		}
		for _, field := range m.contextFields {
			label := dimStyle.Render(field.Label + strings.Repeat(" ", labelWidth-lipgloss.Width(field.Label)) + "  ")
			valueWidth := max(1, m.contentWidth()-labelWidth-2)
			for index, line := range strings.Split(wrapText(field.Value, valueWidth), "\n") {
				prefix := label
				if index > 0 {
					prefix = strings.Repeat(" ", labelWidth+2)
				}
				top = append(top, prefix+line)
			}
		}
		top = append(top, "", dimStyle.Render(strings.Repeat("─", m.contentWidth())), "")
	}
	if !m.hideSearch {
		placeholder := "type to filter"
		if m.modalActions && !m.searching {
			placeholder = "press / to filter"
		}
		if modal {
			filterWidth := max(1, m.contentWidth()-8)
			query := singleLine(m.filter)
			query = ansi.Cut(query, max(0, ansi.StringWidth(query)-filterWidth+1), ansi.StringWidth(query))
			value := query + keyStyle.Render("▏")
			if m.filter == "" {
				value += " " + dimStyle.Render(placeholder)
			}
			top = append(top, dimStyle.Render("Filter  ")+fitColumn(value, max(1, m.contentWidth()-8)))
			top = append(top, dimStyle.Render(strings.Repeat("─", m.contentWidth())))
		} else {
			top = append(top, headingStyle.Render("Search"))
			top = append(top, sectionLines(textField(m.filter, placeholder, m.contentWidth()))...)
		}
	}

	filtered := m.filteredOptions()
	var selected Option
	hasSelection := len(filtered) > 0
	if hasSelection {
		selected = filtered[min(m.cursor, len(filtered)-1)]
	}
	bottom := []string{}
	if hasSelection && m.showSelectedInfo && selected.Summary != "" {
		bottom = append(bottom, "")
		bottom = append(bottom, sectionLines(dimStyle.Render(wrapText(selected.Summary, m.contentWidth())))...)
		bottom = append(bottom, "")
	}

	firstFooter := []string{}
	secondFooter := []string{shortcut("esc", "Back"), shortcut("ctrl+c", "Close")}
	if m.modalActions && m.searching {
		firstFooter = []string{shortcut("enter", "Done"), shortcut("esc", "Clear search")}
		secondFooter = []string{shortcut("ctrl+c", "Close")}
	} else {
		if m.modalActions && !m.hideSearch {
			firstFooter = append(firstFooter, shortcut("/", "Search"))
		}
		if hasSelection {
			for index := len(selected.Actions) - 1; index >= 0; index-- {
				action := selected.Actions[index]
				firstFooter = append([]string{shortcut(action.Key, action.Label)}, firstFooter...)
			}
		}
		if hasSelection && !m.disableEnter {
			firstFooter = append([]string{shortcut("enter", firstNonEmptyUI(m.enterLabel, "Select"))}, firstFooter...)
		}
	}
	if m.showSelectedInfo && hasSelection && selected.Summary != "" {
		firstFooter = append(firstFooter, shortcut("i", "Details"))
	}
	details := []string{}
	if m.hideSearch && m.operationDetails != "" {
		details = append(details, shortcut("i", "Full details"))
	}
	if modal {
		bottom = append(bottom, dimStyle.Render(strings.Repeat("─", m.contentWidth())))
		if len(filtered) > 1 {
			firstFooter = append(firstFooter, shortcut("↑/↓", "Move"))
		}
	}
	footer := [footerHeight]string{
		completeShortcutRow(m.contentWidth(), firstFooter, details),
		shortcutRow(m.contentWidth(), secondFooter...),
	}
	footerRows := 0
	if m.compactDialog {
		items := []string{}
		if hasSelection && !m.disableEnter {
			items = append(items, shortcut("enter", firstNonEmptyUI(selected.EnterLabel, m.enterLabel, "Select")))
		}
		if len(filtered) > 1 {
			items = append(items, shortcut("↑/↓", "Move"))
		}
		items = append(items, shortcut("esc", "Back"))
		footer = [footerHeight]string{completeShortcutRow(m.contentWidth(), items, details), ""}
		footerRows = 1
		if len(filtered) > 0 && filtered[0].MenuGroup != "" {
			footer[0] = completeShortcutRow(m.contentWidth(), []string{shortcut("enter", "Run"), shortcut("↑/↓", "Move")}, []string{shortcut("esc", "Close")})
		}
	}
	body := pickerListBody{
		model: m, options: filtered, showCount: modal,
		showHead: m.usesDetailTable() && len(filtered) > 0,
	}
	return (pageLayout{
		Width: m.contentWidth(), Height: m.height, Top: top, Body: body,
		Bottom: bottom, Footer: footer, FooterRows: footerRows,
	}).Render()
}
