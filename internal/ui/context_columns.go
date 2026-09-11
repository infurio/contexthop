package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The five-row header compares effective values without changing the draft.
// Comparisons disappear before Selected as the terminal shrinks.
func (m listModel) columnContextHeader() string {
	plan := m.contextHeaderLayout()
	active, shared, selected := m.activeContextDisplay(), m.sharedContextDisplay(), m.selectedContextDisplay()
	columns := []contextDisplay{}
	if plan.active {
		columns = append(columns, active)
	}
	if plan.shared {
		columns = append(columns, shared)
	}
	columns = append(columns, selected)
	row := func(label string, cells []string) string {
		result := padContextCell(dimStyle.Render(label), plan.labelWidth)
		for i, cell := range cells {
			result += strings.Repeat(" ", plan.gap)
			if i == len(cells)-1 {
				result += fitColumn(cell, plan.widths[i])
			} else {
				result += padContextCell(cell, plan.widths[i])
			}
		}
		return result
	}
	headings := []string{}
	for _, column := range columns {
		heading := headingStyle.Render(column.label)
		if column.status != "" {
			heading += dimStyle.Render(" · ") + statusStyle(column.status).Render(column.status)
		}
		headings = append(headings, heading)
	}
	lines := []string{row("", headings)}
	for i, key := range contextComponents {
		cells := []string{}
		for j, column := range columns {
			field := column.field(key)
			value, style := field.value, valueStyle
			if display(value) == "none" {
				value, style = "—", dimStyle
			}
			isSelected := j == len(columns)-1
			if plan.active && j == 0 && value != "—" && value == selected.field(key).value {
				style = dimStyle
			}
			if isSelected && value != "—" {
				symbol := "› "
				if value == active.field(key).value {
					style = dimStyle
				}
				if field.state == contextStaged {
					symbol, style = "✓ ", valueStyle.Foreground(stagedStyle.GetForeground())
				}
				value = symbol + value
			}
			cells = append(cells, contextValue(value, style, plan.widths[j], field.adc))
		}
		lines = append(lines, row([]string{"Identity", "Project", "Kubernetes", "Docker"}[i], cells))
	}
	return strings.Join(lines, "\n")
}

func padContextCell(value string, width int) string {
	value = fitColumn(value, width)
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func contextValue(value string, style lipgloss.Style, width int, adc bool) string {
	if adc {
		return style.Render(fitColumn(value, max(1, width-6))) + keyStyle.Render(" [ADC]")
	}
	return style.Render(fitColumn(value, width))
}
