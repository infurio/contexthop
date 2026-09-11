package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type resourceTableBody struct {
	model    listModel
	title    string
	options  []Option
	showHead bool
}

func (body resourceTableBody) PreferredHeight() int {
	rows := max(1, min(10, len(body.options)))
	heading := 0
	if body.showHead {
		heading = 1
	}
	return rows + heading + len(body.model.resourcePanelHeader()) + 1
}

func (body resourceTableBody) Render(height int) []string {
	if height <= 0 {
		return nil
	}
	if height == 1 {
		return []string{body.model.resourcePanelBottom(body.title, len(body.options), 0, 0, false)}
	}
	if height == 2 {
		return []string{resourceListTop(body.title, body.model.contentWidth()), body.model.resourcePanelBottom(body.title, len(body.options), 0, 0, false)}
	}

	lines := body.model.resourcePanelHeader()
	lines = lines[:min(len(lines), max(1, height-3))]
	if body.showHead {
		header := headingStyle.Render("  " + body.model.detailRow(Option{}, true))
		lines = append(lines, resourceListRow(header, body.model.contentWidth()))
	}
	rowCapacity := max(0, height-len(lines)-1)
	start, end := visibleListRange(len(body.options), body.model.cursor, rowCapacity)
	if len(body.options) > 0 {
		for index := start; index < end; index++ {
			option := body.options[index]
			staged := body.model.composeSelection && (body.model.selectedName == option.Name || option.MatchesSelection)
			focused := index == body.model.cursor
			cursor := "  "
			if focused {
				cursor = "> "
			}
			if staged {
				cursor = "✓ "
				if option.Name != body.model.selectedName {
					cursor = "≈ "
				}
			}
			rowModel := body.model
			var style lipgloss.Style
			styled := false
			if staged && focused {
				style, styled = stagedCursorStyle, true
			} else if option.Hidden {
				style, styled = hiddenStyle, true
				if focused || staged {
					style = hiddenSelectedStyle
				}
			} else if focused {
				style, styled = selectedStyle, true
			} else if staged {
				style, styled = stagedStyle, true
			}
			if styled {
				rowModel.rowStyle = &style
			}
			line := rowModel.compactRow(option)
			if body.showHead {
				line = rowModel.detailRow(option, false)
			}
			row := cursor + line
			if styled {
				row = renderResourceHighlight(row, style, max(1, body.model.contentWidth()-3))
			}
			lines = append(lines, resourceListRow(row, body.model.contentWidth()))
		}
	} else if rowCapacity > 0 && (!body.model.hideSearch || len(body.model.options) > 0) {
		messages := sectionLines(wrapText(body.model.emptyListMessage(), max(1, body.model.contentWidth()-6)))
		for _, message := range messages[:min(len(messages), rowCapacity)] {
			lines = append(lines, resourceListRow(warnStyle.Render(message), body.model.contentWidth()))
		}
		end = min(len(messages), rowCapacity)
	}
	for rendered := end - start; rendered < rowCapacity; rendered++ {
		lines = append(lines, resourceListRow("", body.model.contentWidth()))
	}
	lines = append(lines, body.model.resourcePanelBottom(body.title, len(body.options), start, end, len(body.options) > rowCapacity && rowCapacity > 0))
	return lines
}

func (m listModel) resourceBrowserView() string {
	title := firstNonEmptyUI(m.pickerTitle, "Resources")
	filtered := m.filteredOptions()
	var selected Option
	hasSelection := len(filtered) > 0
	if hasSelection {
		selected = filtered[min(m.cursor, len(filtered)-1)]
	}

	top := m.resourceHeader()

	body := resourceTableBody{
		model: m, title: title, options: filtered,
		showHead: m.usesDetailTable() && (len(filtered) > 0 || m.resourceBrowser),
	}
	return (pageLayout{
		Width: m.contentWidth(), Height: m.height, Top: top, Body: body,
		Footer: m.resourceFooterPlan(selected, hasSelection).Render(),
	}).Render()
}

func (m listModel) emptyListMessage() string {
	if m.filter != "" {
		return "  No matches · Esc to clear search"
	}
	if m.resourceBrowser {
		if m.dimension == "workspace" {
			return "No workspaces · n: new\nTab: tabs · Space: stage\nCtrl+W: save workspace"
		}
		if m.dimension == "project" {
			return "  No projects in this scope · d to discover · n to add manually"
		}
		if m.dimension == "docker" {
			return "  No Docker contexts · l to import local contexts"
		}
		if m.dimension == "identity" {
			return "  No identities · n to add · l to import local contexts"
		}
		if m.dimension == "kubernetes" {
			return "  No clusters in this scope · d to discover · c to clear selection"
		}
		return "  No resources in this scope · n to create · c to clear selection"
	}
	return "  No options available"
}

// Tag colours are nested within the row style. Their resets must restore the
// row's foreground/background before separators, subsequent tags and SOURCE.
func renderResourceHighlight(row string, style lipgloss.Style, width int) string {
	base := ansi.NewStyle().ForegroundColor(style.GetForeground()).BackgroundColor(style.GetBackground())
	if style.GetBold() {
		base = base.Bold()
	}
	restore := ansi.ResetStyle + base.String()
	row = strings.NewReplacer(ansi.ResetStyle, restore, "\x1b[0m", restore).Replace(row)
	return style.Width(width).Render(row)
}
