package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func compactContextFields(d contextDisplay) []contextDisplayField {
	fields := []contextDisplayField{}
	for _, field := range d.fields {
		if field.value == "" || field.value == "contexthop-none" {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

// Keep component labels and status visible; shorten values before omitting a
// trailing field. An ellipsis explicitly marks fields that cannot fit.
func fitHeaderFields(fields []contextDisplayField, width int, pending bool) string {
	plan := allocateHeaderFields(fields, width)
	if plan.width <= 0 {
		return ""
	}
	if len(plan.sizes) == 0 {
		return fitColumn("…", plan.width)
	}
	parts := []string{}
	for i, f := range fields[:len(plan.sizes)] {
		style := fieldStyle(string(f.component), f.value)
		if pending {
			style = stagedStyle
		}
		switch f.state {
		case contextStaged:
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
		case contextHighlighted:
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#60a5fa"))
		case contextEmpty:
			style = dimStyle
		}
		value := style.Render(fitColumn(f.value, plan.sizes[i]))
		if f.adc {
			value = contextValue(f.value, style, plan.sizes[i], true)
		}
		parts = append(parts, dimStyle.Render(f.label+": ")+value)
	}
	result := strings.Join(parts, dimStyle.Render(" · "))
	if plan.omitted {
		result += dimStyle.Render(" · …")
	}
	return fitColumn(result, plan.width)
}

func contextHeaderPrefix(label string) string {
	return dimStyle.Render(label + strings.Repeat(" ", max(1, 9-lipgloss.Width(label))))
}

func (m listModel) selectionHeading() string {
	display := m.activeContextDisplay()
	fields := compactContextFields(display)
	prefix := contextHeaderPrefix(display.label)
	suffix := dimStyle.Render(" · ") + statusStyle(display.status).Render(display.status)
	budget := compactHeaderBudget(m.contentWidth(), prefix, suffix)
	content := dimStyle.Render("none")
	if len(fields) > 0 {
		content = fitHeaderFields(fields, budget, false)
	}
	return fitColumn(prefix+fitColumn(content, budget)+suffix, m.contentWidth())
}

func (m listModel) resourceSelection() string {
	if m.contextHeaderLayout().columns {
		return m.columnContextHeader()
	}
	return m.compactResourceSelection()
}

func (m listModel) compactResourceSelection() string {
	if preview := m.nextShellPreview; preview != nil {
		prefix := contextHeaderPrefix("Selected")
		content := "Highlight an item"
		if preview.Available || len(preview.Fields) > 0 {
			fields := []contextDisplayField{}
			for _, field := range m.selectedContextDisplay().fields {
				state := field.state
				symbol := "› "
				if state == contextStaged {
					symbol = "✓ "
				} else if state == contextEmpty {
					symbol = "— "
				}
				value := symbol + field.value
				if state == contextEmpty && (field.value == "none" || field.value == "") {
					value = "—"
				}
				field.value = value
				fields = append(fields, field)
			}
			budget := compactHeaderBudget(m.contentWidth(), prefix, "")
			content = fitHeaderFields(fields, budget, true)
			if split := m.contextHeaderLayout().fieldBreak(fields, budget); split > 0 {
				content = fitHeaderFields(fields[:split], budget, true) + "\n" + strings.Repeat(" ", lipgloss.Width(prefix)) + fitHeaderFields(fields[split:], budget, true)
			}
		}
		return prefix + content
	}
	pending := m.pendingContextDisplay()
	prefix := contextHeaderPrefix(pending.display.label)
	content := dimStyle.Render("No changes")
	if pending.changed {
		content = fitHeaderFields(compactContextFields(pending.display), compactHeaderBudget(m.contentWidth(), prefix, ""), true)
	}
	return m.selectionHeading() + "\n" + fitColumn(prefix+content, m.contentWidth())
}
