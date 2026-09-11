package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/state"
)

const (
	defaultTerminalWidth = 100
	maxContentWidth      = 118
	pageGutterWidth      = 1
	pageRightMargin      = 1
	pickerCursorWidth    = 2
)

type probeMessage state.ProbeResult

type listModel struct {
	version                   string
	selectionClearLabel       string
	nextShellPreview          *LaunchPreview
	canApplyShell             bool
	pendingPristine           bool
	selectedKubernetesContext string
	selectedNamespace         string
	rowStyle                  *lipgloss.Style
	tabScreen                 Screen
	browseAll                 bool
	operationActions          []KeyAction
	statusKind                string
	compactDialog             bool
	composeSelection          bool
	selectedName              string
	tabSelection              Draft
	selectedDocker            string
	showHidden                bool
	snapshot                  state.Snapshot
	options                   []Option
	probing                   bool
	probed                    bool
	probeErr                  []string
	width                     int
	height                    int
	filter                    string
	cursor                    int
	pickerTitle               string
	scopeLabel                string
	actionRows                bool
	contextFields             []PickerField
	pickerDescription         string
	operationDetails          string
	dimension                 string
	hideSearch                bool
	disableEnter              bool
	showSelectedInfo          bool
	modalActions              bool
	resourceBrowser           bool
	scoped                    bool
	enterLabel                string
	selectionPath             string
	selection                 [3]string
	searching                 bool
}

// KeyAction is an action available for the currently selected picker row.
// Action is passed to the flow without replacing the selected option.
type KeyAction struct {
	Key     string
	Label   string
	Action  string
	Aliases []string
	Footer  string
}

type Option struct {
	MenuGroup, MenuShortcut string
	FlowDraft               Draft // Request context for an operation; never staged resource selection.
	Cancellable             bool
	OpenScreen              Screen
	WorkLabel               string
	IdentityChoices         []Option // Eligible identities to choose when staging a resource.
	RequiresIdentity        bool     // Kubernetes target needs a cloud identity.
	Selection               Draft
	Source                  string
	Hidden                  bool

	Name                       string
	Label                      string
	Detail                     string
	Summary                    string
	Tags                       []Tag
	Risk                       string
	Provider                   string
	AuthStatus                 string
	SaveStatus                 string
	EnterLabel                 string
	Identity                   bool
	Project                    bool
	Kubernetes                 bool
	Docker                     bool
	KubernetesContext          string
	KubernetesCluster          string
	KubernetesLocation         string
	KubernetesNamespace        string
	KubernetesEffectiveContext string
	ProjectID                  string
	IdentityAccount            string
	DockerContext              string
	MatchesSelection           bool
	Actions                    []KeyAction
}

func (m listModel) View() tea.View {
	view := tea.NewView(m.destinationPickerView())
	view.AltScreen = true
	return view
}

func (m listModel) destinationPickerView() string {
	if m.resourceBrowser {
		return m.resourceBrowserView()
	}
	return m.standardPickerView()
}

func (m listModel) discoveryFooterLabel() string {
	if m.dimension == "docker" {
		return "Import"
	}
	return "Discover"
}

func (m listModel) selectionEnterLabel(selected Option) string {
	if isBrowserAction(selected) {
		return "Open"
	}
	if m.composeSelection && m.resourceBrowser {
		return "Subshell"
	}
	return firstNonEmptyUI(selected.EnterLabel, m.enterLabel, "Select")
}

func footerActionLabel(label string) string {
	switch label {
	case "Edit workspace":
		return "Edit"
	case "Save as new workspace", "Duplicate workspace":
		return "Duplicate"
	case "Delete workspace":
		return "Delete"
	case "Open web console", "Web console":
		return "Web"
	}

	for _, prefix := range []string{"Map ", "Unmap "} {
		if strings.HasPrefix(label, prefix) {
			return strings.TrimSpace(prefix)
		}
	}
	return label
}

func resourceListTop(title string, width int) string {
	return resourceListBorder(title, width, true)
}

func resourceListBorder(title string, width int, top bool) string {
	leftCorner, rightCorner, style := "┌", "┐", headingStyle
	if !top {
		leftCorner, rightCorner, style = "└", "┘", dimStyle
	}
	if width < 4 {
		return fitColumn(leftCorner+"─"+rightCorner, width)
	}
	if title == "" {
		return borderStyle.Render(leftCorner + strings.Repeat("─", width-2) + rightCorner)
	}
	label := " " + fitColumn(title, max(1, width-4)) + " "
	remaining := max(0, width-2-lipgloss.Width(label))
	left := min(1, remaining)
	right := remaining - left
	return borderStyle.Render(leftCorner+strings.Repeat("─", left)) + style.Render(label) + borderStyle.Render(strings.Repeat("─", right)+rightCorner)
}

func resourceListRow(value string, width int) string {
	inner := max(1, width-2)
	value = " " + value
	return borderStyle.Render("│") + lipgloss.NewStyle().Width(inner).Render(fitColumn(value, inner)) + borderStyle.Render("│")
}

func resourceListBottom(width int, label ...string) string {
	if len(label) > 0 && label[0] != "" {
		return resourceListBorder(label[0], width, false)
	}
	return borderStyle.Render("└" + strings.Repeat("─", max(0, width-2)) + "┘")
}

func (m listModel) usesDetailTable() bool {
	if m.contentWidth() < 72 {
		return false
	}
	switch m.dimension {
	case "identity", "project", "kubernetes", "docker", "workspace", "reuse":
		return true
	default:
		return false
	}
}

func (m listModel) detailRow(option Option, header bool) string {
	if m.dimension == "reuse" {
		contextName, identity, kubernetes, detail := option.Label, option.IdentityAccount, option.KubernetesContext, option.Detail
		if header {
			contextName, identity, kubernetes, detail = "CONTEXT", "IDENTITY", "KUBERNETES / NAMESPACE", "STARTED"
		}
		width := m.rowWidth()
		available := max(4, width-6)
		detailWidth := min(10, max(1, available/7))
		remaining := max(3, available-detailWidth)
		contextWidth := max(1, remaining*3/10)
		identityWidth := max(1, remaining*7/20)
		kubernetesWidth := max(1, remaining-contextWidth-identityWidth)
		return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s",
			contextWidth, fitColumn(displayTableValue(contextName), contextWidth),
			identityWidth, fitColumn(displayTableValue(identity), identityWidth),
			kubernetesWidth, fitColumn(displayTableValue(kubernetes), kubernetesWidth),
			detailWidth, fitColumn(detail, detailWidth))
	}
	headings, values := resourceTableValues(m.dimension, option)
	sizingOptions := m.options
	if m.resourceBrowser {
		sizingOptions = m.filteredOptions()
	}
	showSource := option.Source != ""
	for _, candidate := range sizingOptions {
		if candidate.Source != "" {
			showSource = true
		}
	}
	if (!showSource || m.contentWidth() < 110) && len(headings) > 0 && headings[len(headings)-1] == "SOURCE" {
		headings = headings[:len(headings)-1]
		values = values[:len(values)-1]
	}
	if header {
		values = headings
	}
	rows := make([][]string, 0, len(sizingOptions)+1)
	for _, candidate := range sizingOptions {
		_, row := resourceTableValues(m.dimension, candidate)
		rows = append(rows, row)
	}
	if !header {
		rows = append(rows, values)
	}
	return alignedStyledTableRow(values, headings, rows, m.rowWidth(), m.rowStyle, !header, m.resourceBrowser)
}

func resourceTableValues(dimension string, option Option) ([]string, []string) {
	module := moduleForDimension(dimension)
	if module == nil {
		return nil, nil
	}
	values := module.Values(option)

	return module.Columns(), values
}

func alignedTableRow(values, headings []string, rows [][]string, width int, colorize ...bool) string {
	return alignedStyledTableRow(values, headings, rows, width, nil, colorize...)
}

func alignedStyledTableRow(values, headings []string, rows [][]string, width int, rowStyle *lipgloss.Style, colorize ...bool) string {
	if len(headings) == 0 {
		return ""
	}
	widths := make([]int, len(headings))
	minimums := make([]int, len(headings))
	for index, heading := range headings {
		widths[index] = lipgloss.Width(heading)
		minimums[index] = min(widths[index], 8)
		if heading == "TAGS" {
			minimums[index] = 1
		}
	}
	for _, row := range rows {
		for index := range headings {
			if index < len(row) {
				widths[index] = max(widths[index], lipgloss.Width(tableCell(row[index], headings[index])))
			}
		}
	}
	available := max(len(headings), width-2*(len(headings)-1))
	if len(colorize) > 1 && colorize[1] {
		// When the visible rows overflow, trim the widest flexible column first.
		// Long names must not consume the space needed for short status fields.
		for total(widths) > available {
			widest := -1
			for index := range widths {
				if widths[index] > minimums[index] && (widest < 0 || widths[index]-minimums[index] > widths[widest]-minimums[widest]) {
					widest = index
				}
			}
			if widest < 0 {
				break
			}
			widths[widest]--
		}
	} else {
		for index := len(widths) - 1; index >= 0 && total(widths) > available; index-- {
			for widths[index] > minimums[index] && total(widths) > available {
				widths[index]--
			}
		}
	}
	parts := make([]string, len(headings))
	for index := range headings {
		value := ""
		if index < len(values) {
			value = tableCell(values[index], headings[index])
		}
		parts[index] = fitColumn(value, widths[index])
		if len(colorize) > 0 && colorize[0] && headings[index] != "TAGS" {
			if rowStyle != nil {
				parts[index] = ansi.Strip(parts[index])
			} else {
				parts[index] = fieldStyle(headings[index], value).Render(parts[index])
			}
		}
		parts[index] += strings.Repeat(" ", max(0, widths[index]-lipgloss.Width(parts[index])))
	}
	return fitColumn(strings.Join(parts, "  "), width)
}

func tableCell(value, heading string) string {
	if (heading == "RISK" || heading == "TAGS" || heading == "SOURCE") && value == "" {
		return ""
	}
	return singleLine(displayTableValue(value))
}

func total(values []int) int {
	result := 0
	for _, value := range values {
		result += value
	}
	return result
}

func (m listModel) compactRow(option Option) string {
	primary := firstNonEmptyUI(option.Label, option.Name)
	secondary := firstNonEmptyUI(option.Detail, option.Summary)
	switch m.dimension {
	case "identity":
		primary, secondary = option.IdentityAccount, option.Provider
	case "project":
		primary, secondary = option.ProjectID, option.IdentityAccount
	case "kubernetes":
		primary, secondary = firstNonEmptyUI(option.KubernetesCluster, option.KubernetesContext, option.Name), option.KubernetesContext
	case "docker":
		primary, secondary = option.DockerContext, ""
	}

	primary = singleLine(displayTableValue(primary))
	if moduleForDimension(m.dimension) != nil {
		primary = fieldStyle(m.dimension, primary).Render(primary)
	}
	secondary = singleLine(secondary)
	if secondary != "" && secondary != ansi.Strip(primary) {
		if strings.HasPrefix(m.dimension, "catalog") {
			labelWidth := m.catalogLabelWidth()
			primary = fmt.Sprintf("%-*s  %s", labelWidth, fitColumn(primary, labelWidth), secondary)
		} else {
			secondaryStyle := dimStyle
			switch m.dimension {
			case "project":
				secondaryStyle = fieldStyle("identity", secondary)
			case "kubernetes":
				secondaryStyle = fieldStyle("kubernetes", secondary)
			}
			primary += "  " + secondaryStyle.Render(secondary)
		}
	}
	if m.rowStyle != nil {
		primary = ansi.Strip(primary)
	}
	if len(option.Tags) > 0 && m.rowWidth() >= 12 {
		tags := fitColumn(RenderTags(option.Tags), m.rowWidth()/2)
		primary = fitColumn(primary, m.rowWidth()-lipgloss.Width(tags)-2) + "  " + tags
	}
	return fitColumn(primary, m.rowWidth())
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func (m listModel) catalogLabelWidth() int {
	width := 0
	for _, option := range m.options {
		label := firstNonEmptyUI(option.Label, option.Name)
		width = max(width, len([]rune(label)))
	}
	return min(width, max(8, m.rowWidth()/2))
}

func (m listModel) rowWidth() int {
	width := m.contentWidth() - pickerCursorWidth
	if m.resourceBrowser {
		width -= 3
	}
	return max(1, width)
}

func (m listModel) contentWidth() int {
	if m.resourceBrowser {
		terminalWidth := m.width
		if terminalWidth <= 0 {
			terminalWidth = defaultTerminalWidth
		}
		return max(1, terminalWidth-pageGutterWidth-pageRightMargin)
	}
	return terminalContentWidth(m.width)
}

func terminalContentWidth(terminalWidth int) int {
	if terminalWidth <= 0 {
		terminalWidth = defaultTerminalWidth
	}
	return min(maxContentWidth, max(1, terminalWidth-pageGutterWidth-pageRightMargin))
}

func shortcut(key, label string) string {
	return keyStyle.Render("["+displayShortcutKey(key)+"]") + " " + dimStyle.Render(label)
}

func shortcutRow(width int, shortcuts ...string) string {
	return fitColumn(strings.Join(shortcuts, "   "), width)
}

func textField(value, placeholder string, width int) string {
	return inputTextField(value, placeholder, width, 0)
}

func inputTextField(value, placeholder string, width, offset int) string {
	field := keyStyle.Render("▏") + " " + dimStyle.Render(placeholder)
	if value != "" {
		characters := []rune(value)
		position := len(characters) - min(offset, len(characters))
		before, after := string(characters[:position]), string(characters[position:])
		// Keep the insertion point visible for long commands and paths.
		available := max(1, width-5)
		if lipgloss.Width(before) >= available {
			before = ansi.Cut(before, lipgloss.Width(before)-available+1, lipgloss.Width(before))
		}
		field = valueStyle.Render(before) + keyStyle.Render("▏") + valueStyle.Render(after)
		field = ansi.Truncate(field, available+1, "")
	}
	return lipgloss.NewStyle().
		Width(width).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentColor).
		Render(field)
}

func withGutter(value string) string {
	value = strings.TrimSuffix(value, "\n")
	return " " + strings.ReplaceAll(value, "\n", "\n ") + "\n"
}

func optionScope(option Option) string {
	values := make([]string, 0, 3)
	for _, value := range []string{option.ProjectID, option.KubernetesContext, option.DockerContext} {
		if value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, " / ")
}

func displayTableValue(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func fitColumn(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, "…")
}

func wrapText(value string, width int) string {
	if width <= 0 {
		return value
	}
	return ansi.Wrap(value, width, "/._@")
}

func (m listModel) filteredOptions() []Option {
	filter := strings.ToLower(m.filter)
	result := make([]Option, 0, len(m.options))
	for _, option := range m.options {
		if m.resourceBrowser && option.Hidden && !m.showHidden {
			continue
		}
		if !optionHasDimension(option, m.dimension) {
			continue
		}
		if m.resourceBrowser {
			if resourceNameMatches(option, m.dimension, strings.TrimSpace(filter)) {
				result = append(result, option)
			}
			continue
		}
		candidate := strings.ToLower(strings.Join([]string{
			option.Name, option.Label, option.Detail, option.Summary, option.KubernetesContext, option.KubernetesCluster, option.KubernetesLocation,
			option.ProjectID, option.IdentityAccount, option.DockerContext,
		}, " "))
		if filter == "" || fuzzyMatch(candidate, filter) {
			result = append(result, option)
		}
	}
	return result
}

func firstNonEmptyUI(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nonEmptyUI(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func optionHasDimension(option Option, dimension string) bool {
	switch dimension {
	case "identity":
		return option.Identity
	case "project":
		return option.Project
	case "kubernetes":
		return option.Kubernetes
	case "docker":
		return option.Docker
	default:
		return true
	}
}

func fuzzyMatch(candidate, filter string) bool {
	filterCharacters := []rune(strings.ToLower(filter))
	position := 0
	for _, character := range []rune(strings.ToLower(candidate)) {
		if position < len(filterCharacters) && character == filterCharacters[position] {
			position++
		}
	}
	return position == len(filterCharacters)
}

func writeRow(builder *strings.Builder, label, value, suffix string, width int) {
	renderedLabel := labelStyle.Render(label)
	available := max(1, width-lipgloss.Width(renderedLabel))
	if suffix != "" {
		available = max(1, available-lipgloss.Width(suffix)-2)
	}
	builder.WriteString(renderedLabel)
	builder.WriteString(valueStyle.Render(fitColumn(value, available)))
	if suffix != "" {
		builder.WriteString("  ")
		builder.WriteString(suffix)
	}
	builder.WriteString("\n")
}

func display(value string) string {
	if value == "" || value == "contexthop-none" || strings.HasSuffix(value, "/disabled-google-credentials.json") {
		return "none"
	}
	return value
}

func displayPath(value string) string {
	if value == "" || strings.HasSuffix(value, "/disabled-google-credentials.json") {
		return "none"
	}
	if len(value) > 54 {
		return "..." + value[len(value)-51:]
	}
	return value
}

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "CONTEXT-DRIFT":
		return dangerStyle
	case "UNMANAGED":
		return warnStyle
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	}
}

func FormatText(snapshot state.Snapshot) string {
	contextName := snapshot.Destination
	if contextName == "" {
		if snapshot.Managed {
			contextName = "unnamed"
		} else {
			contextName = "unmanaged shell"
		}
	}
	return fmt.Sprintf(
		"Context: %s (%s)\nIdentity: %s\nCloud project: %s\nKubernetes: %s\nNamespace: %s\nDocker: %s\nADC: %s\n",
		contextName,
		snapshot.LocalStatus,
		display(snapshot.Observed.Identity),
		display(snapshot.Observed.Project),
		display(snapshot.Observed.Kubernetes),
		display(snapshot.Observed.Namespace),
		display(snapshot.Observed.Docker),
		display(snapshot.Observed.ADC),
	)
}

func (m listModel) hiddenToggleLabel() string {
	if m.showHidden {
		return "Hide hidden"
	}
	return "Show hidden"
}

// Resource filters search displayed identifiers, not provenance, relationships,
// or status text. Match each field independently to avoid cross-column matches.
func resourceNameMatches(option Option, dimension, filter string) bool {
	fields := []string{firstNonEmptyUI(option.Label, option.Name)}
	switch dimension {
	case "identity":
		fields = []string{firstNonEmptyUI(option.IdentityAccount, option.Label, option.Name)}
	case "project":
		fields = []string{firstNonEmptyUI(option.ProjectID, option.Label, option.Name)}
	case "kubernetes":
		fields = []string{firstNonEmptyUI(option.KubernetesCluster, option.KubernetesContext, option.Name), option.KubernetesContext}
	case "docker":
		fields = []string{firstNonEmptyUI(option.DockerContext, option.Label, option.Name)}
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), strings.ToLower(filter)) {
			return true
		}
	}
	return false
}
