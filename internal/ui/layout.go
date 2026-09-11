package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const footerHeight = 2

// pageLayout is the only component allowed to allocate terminal rows. Screen
// modules provide content sections and a flexible body; they do not calculate
// viewport overhead themselves.
type pageLayout struct {
	Width      int
	Height     int
	Top        []string
	Body       flexiblePageBody
	Bottom     []string
	Footer     [footerHeight]string
	FooterRows int // Zero uses the standard two-row footer.
}

type flexiblePageBody interface {
	Render(height int) []string
	PreferredHeight() int
}

func (page pageLayout) Render() string {
	footerRows := footerHeight
	if page.FooterRows > 0 {
		footerRows = min(page.FooterRows, footerHeight)
	}
	height := page.Height
	if height <= 0 {
		height = len(page.Top) + len(page.Bottom) + footerRows
		if page.Body != nil {
			height += page.Body.PreferredHeight()
		}
	}

	footerLines := page.Footer[:footerRows]
	if height < footerRows {
		footerLines = footerLines[:max(0, height)]
	}
	available := max(0, height-len(footerLines))
	top := append([]string(nil), page.Top...)
	bottom := append([]string(nil), page.Bottom...)
	if page.Body != nil {
		// Keep at least one selectable row visible when descriptions or details
		// grow. Omitted context has an explicit continuation marker.
		reserve := min(4, max(0, available-1), page.Body.PreferredHeight())
		budget := max(0, available-reserve)
		top = boundedSection(top, budget)
		bottom = boundedSection(bottom, max(0, budget-len(top)))
	}
	if len(top)+len(bottom) > available {
		// Preserve the primary header first, then as much lower contextual
		// information as fits. The footer is never part of this trade-off.
		top = top[:min(len(top), available)]
		bottom = bottom[:min(len(bottom), max(0, available-len(top)))]
	}
	bodyHeight := max(0, available-len(top)-len(bottom))
	body := []string{}
	if page.Body != nil {
		body = page.Body.Render(bodyHeight)
	}
	if len(body) > bodyHeight {
		body = body[:bodyHeight]
	}
	for len(body) < bodyHeight {
		body = append(body, "")
	}

	lines := make([]string, 0, height)
	lines = append(lines, top...)
	lines = append(lines, body...)
	lines = append(lines, bottom...)
	for len(lines) < available {
		lines = append(lines, "")
	}
	lines = append(lines, footerLines...)
	if page.Width > 0 {
		for index := range lines {
			// A screen module cannot escape its allocated row by returning an
			// embedded newline. Explicit multiline regions arrive as separate
			// section lines before composition.
			lines[index] = strings.NewReplacer("\r", " ", "\n", " ").Replace(lines[index])
			lines[index] = fitColumn(lines[index], page.Width)
		}
	}
	return withGutter(strings.Join(lines, "\n"))
}

func sectionLines(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(value, "\n"), "\n")
}

func boundedSection(lines []string, height int) []string {
	if len(lines) <= height {
		return lines
	}
	if height == 0 {
		return nil
	}
	result := append([]string(nil), lines[:height]...)
	result[height-1] = dimStyle.Render("…")
	return result
}

// contextHeaderLayout owns progressive hiding, column widths and reserved rows.
// Unknown height uses the full layout; Selected always outlives comparisons.
type contextHeaderLayout struct {
	columns, active, shared, wrap bool
	labelWidth, gap, rows         int
	widths                        []int
}

func (m listModel) contextHeaderLayout() contextHeaderLayout {
	width := m.contentWidth()
	atLeast := func(height int) bool { return m.height == 0 || m.height >= height }
	p := contextHeaderLayout{labelWidth: 10, gap: 2, rows: 2}
	if m.nextShellPreview == nil {
		return p
	}
	p.columns = width >= 35 && atLeast(16)
	p.wrap = width >= 58 && atLeast(14)
	if p.columns {
		p.rows = 5
	} else if p.wrap {
		p.rows = 3
	}
	p.active = p.columns && width >= 90 && atLeast(18)
	p.shared = p.active && width >= 140 && atLeast(22) && m.snapshot.Scope != "shared" && (m.snapshot.Scope == "local" || m.snapshot.SharedConfigChecked || m.snapshot.SharedConfig != nil)
	count := 1
	if p.active {
		count++
	}
	if p.shared {
		count++
	}
	available := width - p.labelWidth - count*p.gap
	for i := 0; i < count; i++ {
		p.widths = append(p.widths, available/count)
	}
	p.widths[count-1] += available % count
	return p
}

// Compact previews balance two lines only when the reserved height permits it.
func (p contextHeaderLayout) fieldBreak(fields []contextDisplayField, budget int) int {
	if !p.wrap || len(fields) < 2 || headerFieldsWidth(fields) <= budget {
		return 0
	}
	split, best := 1, headerFieldsWidth(fields)
	for i := 1; i < len(fields); i++ {
		if size := max(headerFieldsWidth(fields[:i]), headerFieldsWidth(fields[i:])); size < best {
			split, best = i, size
		}
	}
	return split
}

// The untruncated width lets the Selected preview wrap before shortening values.
func headerFieldsWidth(fields []contextDisplayField) int {
	width := max(0, len(fields)-1) * 3
	for _, field := range fields {
		width += lipgloss.Width(field.label) + 2 + lipgloss.Width(field.value)
		if field.adc {
			width += 6
		}
	}
	return width
}

// headerFieldLayout assigns widths and trailing omissions before styling.
// Labels and ADC badges retain their minimum space as values are shortened.
type headerFieldLayout struct {
	width   int
	sizes   []int
	omitted bool
}

func allocateHeaderFields(fields []contextDisplayField, width int) headerFieldLayout {
	plan := headerFieldLayout{width: width}
	if width <= 0 {
		return plan
	}
	omitted := false
	minimum := func() int {
		n := max(0, len(fields)-1) * 3
		for _, f := range fields {
			n += len(f.label) + 3
			if f.adc {
				n += 6
			}
		}
		if omitted {
			n += 4
		}
		return n
	}
	for len(fields) > 0 && minimum() > width {
		fields = fields[:len(fields)-1]
		omitted = true
	}
	if len(fields) == 0 {
		plan.omitted = true
		return plan
	}
	sizes := make([]int, len(fields))
	total := max(0, len(fields)-1) * 3
	for i, f := range fields {
		sizes[i] = lipgloss.Width(f.value)
		if f.adc {
			sizes[i] += 6
		}
		total += len(f.label) + 2 + sizes[i]
	}
	if omitted {
		total += 4
	}
	for total > width {
		largest := -1
		for i := range sizes {
			minimum := 1
			if fields[i].adc {
				minimum = 7
			}
			if sizes[i] > minimum && (largest < 0 || sizes[i] > sizes[largest]) {
				largest = i
			}
		}
		if largest < 0 {
			break
		}
		sizes[largest]--
		total--
	}

	plan.sizes, plan.omitted = sizes, omitted
	return plan
}

func compactHeaderBudget(width int, prefix, suffix string) int {
	return max(1, width-lipgloss.Width(prefix)-lipgloss.Width(suffix))
}

// visibleListRange keeps the cursor visible for lists with one row per item.
// Multi-line action menus retain their own selected-block scrolling.
func visibleListRange(count, cursor, capacity int) (start, end int) {
	if count <= 0 || capacity <= 0 {
		return 0, 0
	}
	start = max(0, cursor-capacity+1)
	return start, min(start+capacity, count)
}
