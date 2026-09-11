package ui

// Reserve the same header height for every preview state, including a blank
// message row. Page layout receives that row explicitly so it is not trimmed.
func (m listModel) resourceHeader() []string {
	lines := sectionLines(m.resourceSelection())
	rows := m.contextHeaderLayout().rows
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return append(lines, m.resourceHeaderNotice().Render(m.contentWidth()))
}
