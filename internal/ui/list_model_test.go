package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFuzzyMatch(t *testing.T) {
	for _, test := range []struct {
		candidate string
		filter    string
		want      bool
	}{
		{candidate: "team-a development", filter: "tadev", want: true},
		{candidate: "team production", filter: "teamp", want: true},
		{candidate: "acme-sandbox", filter: "prod", want: false},
	} {
		if got := fuzzyMatch(test.candidate, test.filter); got != test.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", test.candidate, test.filter, got, test.want)
		}
	}
}

func TestOptionHasDimension(t *testing.T) {
	option := Option{Identity: true, Project: true, Kubernetes: true}
	for _, dimension := range []string{"identity", "project", "kubernetes", ""} {
		if !optionHasDimension(option, dimension) {
			t.Errorf("expected option to match %q", dimension)
		}
	}
	if optionHasDimension(option, "docker") {
		t.Error("expected option not to match docker")
	}
}

func TestViewsUseAlternateScreen(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	home := listModel{}.View()
	if !home.AltScreen {
		t.Fatal("home view must use alternate screen")
	}
	picker := listModel{}.View()
	if !picker.AltScreen {
		t.Fatal("picker view must use alternate screen")
	}
}

func TestViewsUseOneCharacterGutter(t *testing.T) {
	view := listModel{}.View().Content
	for _, line := range strings.Split(strings.TrimSuffix(view, "\n"), "\n") {
		if !strings.HasPrefix(line, " ") {
			t.Fatalf("line has no gutter: %q", line)
		}
	}
}

func TestDetailTableStartsWithSelectedDimension(t *testing.T) {
	option := Option{
		Name:              "work",
		IdentityAccount:   "person@example.com",
		ProjectID:         "example-project",
		KubernetesContext: "example-cluster",
		DockerContext:     "desktop-linux",
	}
	for _, test := range []struct {
		dimension string
		prefix    string
	}{
		{dimension: "identity", prefix: "person@example.com"},
		{dimension: "project", prefix: "example-project"},
		{dimension: "kubernetes", prefix: "example-cluster"},
		{dimension: "docker", prefix: "desktop-linux"},
		{dimension: "workspace", prefix: "work"},
	} {
		row := ansi.Strip(listModel{dimension: test.dimension, width: 120}.detailRow(option, false))
		if !strings.HasPrefix(row, test.prefix) {
			t.Errorf("%s row = %q", test.dimension, row)
		}
	}
}

func TestResourceTabsExposeEveryResourceType(t *testing.T) {
	raw := resourcePanelTabs("kubernetes", 100)[0]
	view := ansi.Strip(raw)
	for _, label := range []string{"Identities", "Projects", "Kubernetes", "Docker", "Workspaces"} {
		if !strings.Contains(view, label) {
			t.Fatalf("tabs missing %q: %q", label, view)
		}
	}
	if raw == view || !strings.Contains(view, "[ Kubernetes ]") {
		t.Fatalf("active tab is not visually selected: %q", view)
	}
}

func TestResourceListBoxEmbedsScopedTitleAndFillsWidth(t *testing.T) {
	top := resourceListTop("Kubernetes(identity → project)[2]", 80)
	row := resourceListRow("  CLUSTER  PROJECT", 80)
	bottom := resourceListBottom(80)
	for name, line := range map[string]string{"top": top, "row": row, "bottom": bottom} {
		if got := lipgloss.Width(line); got != 80 {
			t.Fatalf("%s width = %d, want 80: %q", name, got, ansi.Strip(line))
		}
	}
	if clean := ansi.Strip(top); !strings.HasPrefix(clean, "┌") || !strings.Contains(clean, "Kubernetes(identity → project)[2]") || !strings.HasSuffix(clean, "┐") {
		t.Fatalf("list title border = %q", clean)
	}
}

func TestResourceBrowserFooterKeepsActionsAndNavigationOnStableRows(t *testing.T) {
	model := listModel{
		resourceBrowser: true, width: 220, height: 30,
		pickerTitle: "Kubernetes(all)[1]", dimension: "kubernetes", modalActions: true, enterLabel: "Launch",
		options: []Option{{
			Name: "cluster", Kubernetes: true, KubernetesCluster: "cluster",
			Actions: []KeyAction{{Key: "H", Label: "Hide", Action: "hide"}},
		}},
	}
	lines := strings.Split(ansi.Strip(model.View().Content), "\n")
	actionLine, navigationLine := -1, -1
	for index, line := range lines {
		if strings.Contains(line, "[enter] Launch") {
			actionLine = index
		}
		if strings.Contains(line, "[?] Help") {
			navigationLine = index
		}
	}
	if actionLine < 0 || navigationLine != actionLine+1 {
		t.Fatalf("footer rows = action %d navigation %d: %q", actionLine, navigationLine, ansi.Strip(model.View().Content))
	}
	if strings.Contains(lines[actionLine], "[n] New") || strings.Contains(lines[navigationLine], "[shift+h] Hide") {
		t.Fatalf("contextual and global shortcuts were mixed: %q / %q", lines[actionLine], lines[navigationLine])
	}
}

func TestResourceBrowserUsesFullTerminalDimensions(t *testing.T) {
	browser := listModel{
		resourceBrowser: true, width: 180, height: 40,
		pickerTitle: "Docker(all)[1]", dimension: "docker",
		options: []Option{{Name: "local", Docker: true, DockerContext: "desktop-linux"}},
	}
	if got := browser.contentWidth(); got != 178 {
		t.Fatalf("browser content width = %d, want 178", got)
	}
	if got := (listModel{width: 180}).contentWidth(); got != maxContentWidth {
		t.Fatalf("non-browser content width = %d, want capped width %d", got, maxContentWidth)
	}
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(browser.View().Content), "\n"), "\n")
	if len(lines) < 39 {
		t.Fatalf("browser rendered %d lines in a 40-line terminal", len(lines))
	}
	footer := -1
	for index, line := range lines {
		if strings.Contains(line, "Help") && strings.Contains(line, "hidden") {
			footer = index
		}
	}
	if footer < len(lines)-3 {
		t.Fatalf("footer is not anchored near terminal bottom: line %d of %d", footer, len(lines))
	}
}

func TestResourceBrowserFooterStaysVisibleForRowWithoutSummary(t *testing.T) {
	const terminalHeight = 28
	options := make([]Option, 0, 11)
	for index := range 10 {
		options = append(options, Option{
			Name: fmt.Sprintf("project-%d", index), Project: true,
			ProjectID: fmt.Sprintf("project-%d", index), Summary: "source:GCP\nMappings: identity:work",
		})
	}
	options = append(options, Option{
		Name: "\x00__search__", Project: true, ProjectID: "Search accessible projects…",
		IdentityAccount: "choose identity", KubernetesContext: "discover",
	})
	model := listModel{
		resourceBrowser: true, modalActions: true, showSelectedInfo: true,
		pickerTitle: "Projects(all)[10]", dimension: "project", width: 140, height: terminalHeight,
		cursor: len(options) - 1, options: options,
	}
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(model.destinationPickerView()), "\n"), "\n")
	if len(lines) > terminalHeight {
		t.Fatalf("view rendered %d lines into a %d-line terminal", len(lines), terminalHeight)
	}
	if len(lines) < 2 || !strings.Contains(lines[len(lines)-2], "[enter]") || !strings.Contains(lines[len(lines)-1], "[?]") {
		t.Fatalf("footer is not the final two visible rows: %#v", lines[max(0, len(lines)-4):])
	}
}

func TestResourceLayoutContractAcrossViewportAndContentStates(t *testing.T) {
	dimensions := []struct {
		name   string
		option Option
	}{
		{"identity", Option{Name: "identity", Identity: true, IdentityAccount: "person@example.com", Provider: "gcp", AuthStatus: "checked on select"}},
		{"project", Option{Name: "project", Project: true, ProjectID: "example-project", IdentityAccount: "person@example.com", KubernetesContext: "2 clusters"}},
		{"kubernetes", Option{Name: "cluster", Kubernetes: true, KubernetesCluster: "example-cluster", KubernetesContext: "friendly-context", ProjectID: "example-project", IdentityAccount: "person@example.com"}},
		{"docker", Option{Name: "docker", Docker: true, DockerContext: "desktop-linux"}},
		{"workspace", Option{Name: "workspace", Label: "development", ProjectID: "example-project", IdentityAccount: "person@example.com"}},
	}
	for _, dimension := range dimensions {
		for _, width := range []int{48, 72, 100, 140} {
			for _, height := range []int{12, 18, 24, 32} {
				for _, searching := range []bool{false, true} {
					option := dimension.option
					if !searching {
						option.Summary = "source:GCP verified:GCP\nMappings: identity:person"
					}
					model := listModel{
						resourceBrowser: true, modalActions: true,
						pickerTitle: dimension.name + "(all)[1]", dimension: dimension.name,
						width: width, height: height, searching: searching, showSelectedInfo: true,
						options: []Option{option},
					}
					lines := strings.Split(strings.TrimSuffix(ansi.Strip(model.destinationPickerView()), "\n"), "\n")
					if len(lines) != height {
						t.Fatalf("%s %dx%d search=%t rendered %d rows", dimension.name, width, height, searching, len(lines))
					}
					for _, line := range lines {
						if got := lipgloss.Width(line); got > width-pageRightMargin {
							t.Fatalf("%s %dx%d line width %d: %q", dimension.name, width, height, got, line)
						}
					}
					secondKey := "[?]"
					if searching {
						secondKey = "[ctrl+c]"
					}
					if !strings.Contains(lines[height-2], "[enter]") || !strings.Contains(lines[height-1], secondKey) {
						t.Fatalf("%s %dx%d footer moved: %#v", dimension.name, width, height, lines[height-2:])
					}
				}
			}
		}
	}
}

func TestDockerPickerOmitsUnrelatedCloudColumns(t *testing.T) {
	model := listModel{
		pickerTitle: "Select Docker", dimension: "docker", width: 120,
		options: []Option{{Name: "local", Label: "local", Docker: true, DockerContext: "desktop-linux"}},
	}
	view := model.destinationPickerView()
	if !strings.Contains(view, "desktop-linux") {
		t.Fatalf("Docker context missing from picker: %q", view)
	}
	for _, unwanted := range []string{"PROJECT ID", "IDENTITY", "none"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("Docker picker contains unrelated %q column: %q", unwanted, view)
		}
	}
}

func TestPickerSearchLooksLikeAnEditableField(t *testing.T) {
	model := listModel{
		pickerTitle: "Select project", dimension: "project", width: 100,
		options: []Option{{Name: "project", Project: true, ProjectID: "project"}},
	}
	view := model.destinationPickerView()
	for _, expected := range []string{"Search", "╭", "▏", "type to filter"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("search field should contain %q, view %q", expected, view)
		}
	}
}

func TestPickerUsesOneConsistentResponsiveWidth(t *testing.T) {
	const terminalWidth = 80
	model := listModel{
		pickerTitle: "Select project for 123456789012-abcdefghijklmnopqrstuvwxyz123456@developer.gserviceaccount.com",
		dimension:   "project",
		width:       terminalWidth,
		options: []Option{{
			Name: "none", Project: true, ProjectID: "No project",
			IdentityAccount:   "123456789012-abcdefghijklmnopqrstuvwxyz123456@developer.gserviceaccount.com",
			KubernetesContext: "No Kubernetes",
		}},
	}
	view := model.destinationPickerView()
	contentWidth := terminalContentWidth(terminalWidth)
	for _, line := range strings.Split(strings.TrimSuffix(view, "\n"), "\n") {
		if got := lipgloss.Width(line); got > terminalWidth-pageRightMargin {
			t.Fatalf("line width %d exceeds viewport width %d: %q", got, terminalWidth-pageRightMargin, line)
		}
		if strings.HasPrefix(line, " ╭") && lipgloss.Width(line) != pageGutterWidth+contentWidth {
			t.Fatalf("search field width = %d, want %d: %q", lipgloss.Width(line), pageGutterWidth+contentWidth, line)
		}
	}
}

func TestWidePickerUsesReadableMaximumWidth(t *testing.T) {
	const terminalWidth = 220
	model := listModel{
		pickerTitle: "Select identity", dimension: "identity", width: terminalWidth,
		options: []Option{{
			Name: "work", Identity: true, IdentityAccount: "developer@team-a.example.com",
			Provider: "gcp", AuthStatus: "checked on select",
		}},
	}
	view := model.destinationPickerView()
	for _, line := range strings.Split(strings.TrimSuffix(view, "\n"), "\n") {
		if got := lipgloss.Width(line); got > pageGutterWidth+maxContentWidth {
			t.Fatalf("line width %d exceeds readable maximum %d: %q", got, pageGutterWidth+maxContentWidth, line)
		}
		if strings.HasPrefix(line, " ╭") && lipgloss.Width(line) != pageGutterWidth+maxContentWidth {
			t.Fatalf("wide search field width = %d, want %d: %q", lipgloss.Width(line), pageGutterWidth+maxContentWidth, line)
		}
	}
}

func TestIdentityTablePreservesFullAccountsBeforeSecondaryColumns(t *testing.T) {
	account := "123456789012-abcdefghijklmnopqrstuvwxyz123456@developer.gserviceaccount.com"
	options := []Option{{
		Name: "service-account", Identity: true, IdentityAccount: account,
		Provider: "gcp", AuthStatus: "checked on select",
	}}

	wide := listModel{dimension: "identity", width: 220, options: options}
	if row := wide.detailRow(options[0], false); !strings.Contains(row, account) || !strings.Contains(row, "gcp") || !strings.Contains(row, "checked on select") {
		t.Fatalf("wide identity row lost content: %q", row)
	}

	medium := listModel{dimension: "identity", width: 100, options: options}
	if row := medium.detailRow(options[0], false); !strings.Contains(row, account) {
		t.Fatalf("medium identity row cropped primary account: %q", row)
	}
}

func TestNarrowListWrapsShortcutBars(t *testing.T) {
	const terminalWidth = 42
	view := listModel{width: terminalWidth}.View().Content
	for _, line := range strings.Split(strings.TrimSuffix(view, "\n"), "\n") {
		if got := lipgloss.Width(line); got > terminalWidth-pageRightMargin {
			t.Fatalf("line width %d exceeds viewport width %d: %q", got, terminalWidth-pageRightMargin, line)
		}
	}
}

func TestKubernetesTableSeparatesPhysicalClusterAndContext(t *testing.T) {
	option := Option{
		Name: "target", Kubernetes: true, KubernetesCluster: "payments-api",
		KubernetesContext: "Payments-Prod", KubernetesLocation: "us-central1",
		ProjectID: "payments-prod", IdentityAccount: "person@example.com",
	}
	model := listModel{dimension: "kubernetes", width: 132}
	header := model.detailRow(Option{}, true)
	for _, column := range []string{"CLUSTER", "CONTEXT", "PROJECT ID", "IDENTITY", "TAGS"} {
		if !strings.Contains(header, column) {
			t.Fatalf("header %q does not include %q", header, column)
		}
	}
	row := ansi.Strip(model.detailRow(option, false))
	for _, value := range []string{"payments-api", "Payments-Prod", "payments-prod", "person@example.com"} {
		if !strings.Contains(row, value) {
			t.Fatalf("row %q does not include %q", row, value)
		}
	}
}

func TestKubernetesTableHasAlignedRiskColumn(t *testing.T) {
	options := []Option{{
		KubernetesCluster: "physical-cluster", KubernetesContext: "Friendly Alias",
		ProjectID: "project-id", IdentityAccount: "not mapped", Risk: "sandbox",
	}}
	model := listModel{dimension: "kubernetes", width: 132, options: options}
	header := model.detailRow(Option{}, true)
	for _, tc := range []struct {
		risk string
		want string
	}{
		{risk: "production", want: "production"},
		{risk: "sandbox", want: "sandbox"},
	} {
		option := Option{
			KubernetesCluster: "physical-cluster", KubernetesContext: "Friendly Alias",
			ProjectID: "project-id", IdentityAccount: "not mapped", Tags: []Tag{{Name: tc.risk, Color: "#f87171"}},
		}
		model.options = []Option{option}
		row := ansi.Strip(model.detailRow(option, false))
		header = model.detailRow(Option{}, true)
		if strings.Index(header, "TAGS") != strings.Index(row, tc.want) {
			t.Fatalf("risk column is not aligned for %s: header %q, row %q", tc.risk, header, row)
		}
	}
}

func TestLegacyRiskDoesNotAddBadges(t *testing.T) {
	option := Option{Name: "sandbox", Label: "sandbox", Detail: "isolated", Risk: "sandbox"}
	if row := (listModel{dimension: "catalog", options: []Option{option}, width: 80}).compactRow(option); strings.Contains(row, "SANDBOX") {
		t.Fatalf("sandbox row = %q", row)
	}
}

func TestProjectTableHasAlignedRiskColumn(t *testing.T) {
	option := Option{
		ProjectID: "example-prod-1234", IdentityAccount: "2 identities",
		KubernetesContext: "1 cluster", Tags: []Tag{{Name: "production", Color: "#f87171"}},
	}
	model := listModel{dimension: "project", width: 100, options: []Option{option}}
	header := model.detailRow(Option{}, true)
	row := ansi.Strip(model.detailRow(option, false))
	if !strings.Contains(header, "TAGS") || !strings.Contains(row, "production") {
		t.Fatalf("project table header %q, row %q", header, row)
	}
	if strings.Index(header, "TAGS") != strings.Index(row, "production") {
		t.Fatalf("risk column is not aligned: header %q, row %q", header, row)
	}
}

func TestNarrowKubernetesRowKeepsPhysicalClusterFirst(t *testing.T) {
	option := Option{Name: "target", Kubernetes: true, KubernetesCluster: "physical-cluster", KubernetesContext: "Friendly Alias", Risk: "production"}
	model := listModel{dimension: "kubernetes", width: 48}
	row := ansi.Strip(model.compactRow(option))
	if !strings.HasPrefix(row, "physical-cluster") || len([]rune(row)) > model.rowWidth() {
		t.Fatalf("narrow row = %q (width %d)", row, model.rowWidth())
	}
}

func TestMediumKubernetesTableKeepsConsistentColumns(t *testing.T) {
	model := listModel{dimension: "kubernetes", width: 80}
	header := model.detailRow(Option{}, true)
	if !strings.Contains(header, "CLUSTER") || !strings.Contains(header, "CONTEXT") || !strings.Contains(header, "PROJECT ID") || !strings.Contains(header, "IDENTITY") || !strings.Contains(header, "TAGS") {
		t.Fatalf("medium header = %q", header)
	}
}

func TestResourceBrowserShowsCommittedSelectionInDedicatedStrip(t *testing.T) {
	model := listModel{
		resourceBrowser: true, pickerTitle: "Projects(all)[1]",
		dimension: "project", width: 140, selectionPath: "person@example.com → example-project → example-cluster",
		options: []Option{{Name: "project", ProjectID: "example-project"}},
	}
	lines := strings.Split(ansi.Strip(model.destinationPickerView()), "\n")
	if !strings.Contains(lines[0], "Active") || !strings.Contains(lines[0], "UNMANAGED") || strings.Contains(lines[0], "ContextHop") {
		t.Fatalf("resource header = %q", lines[0])
	}
	selection := strings.Join(lines[:min(5, len(lines))], "\n")
	for _, expected := range []string{"Pending", "person@example.com", "example-project", "example-cluster"} {
		if !strings.Contains(selection, expected) {
			t.Fatalf("selection strip should contain %q: %q", expected, selection)
		}
	}
}

func TestEveryListViewUsesExactlyTwoShortcutRows(t *testing.T) {
	cases := map[string]string{
		"picker": (listModel{
			pickerTitle: "Select", dimension: "project", width: 120,
			options: []Option{{Name: "project", Project: true, ProjectID: "project"}},
		}).View().Content,
		"browser": (listModel{
			resourceBrowser: true, modalActions: true,
			pickerTitle: "Projects(all)[1]", dimension: "project", width: 120,
			options: []Option{{Name: "project", Project: true, ProjectID: "project"}},
		}).View().Content,
		"browser search": (listModel{
			resourceBrowser: true, modalActions: true, searching: true,
			pickerTitle: "Projects(all)[1]", dimension: "project", width: 120,
			options: []Option{{Name: "project", Project: true, ProjectID: "project"}},
		}).View().Content,
	}
	for name, view := range cases {
		lines := shortcutLines(ansi.Strip(view))
		if len(lines) != 2 {
			t.Fatalf("%s shortcut rows = %d, want 2: %#v\n%s", name, len(lines), lines, ansi.Strip(view))
		}
	}
}

func shortcutLines(view string) []string {
	result := []string{}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "[enter]") || strings.Contains(line, "[esc]") ||
			strings.Contains(line, "[ctrl+c]") || strings.Contains(line, "[?]") ||
			strings.Contains(line, "[i] Identity") || strings.Contains(line, "[q] Close") {
			result = append(result, line)
		}
	}
	return result
}

func TestDescriptionWrapsWithoutLosingPreviewLines(t *testing.T) {
	got := wrapText("map identity:work to project:payments\nworkspace prod remains valid", 24)
	if !strings.Contains(got, "identity:work") || !strings.Contains(got, "workspace prod") || strings.Count(got, "\n") < 2 {
		t.Fatalf("wrapped description = %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > 24 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
}

func TestWrapTextPreservesLongAccountIdentifiers(t *testing.T) {
	account := "123456789012-abcdefghijklmnopqrstuvwxyz123456@developer.gserviceaccount.com"
	original := "Select project for " + account
	got := wrapText(original, 32)
	if strings.Join(strings.Fields(got), "") != strings.Join(strings.Fields(original), "") {
		t.Fatalf("wrapped title lost content: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if lipgloss.Width(line) > 32 {
			t.Fatalf("wrapped line is too wide: %q", line)
		}
	}
}

func TestResourceFooterDescribesSelectedAction(t *testing.T) {
	model := listModel{resourceBrowser: true, enterLabel: "Continue", width: 100}
	for _, label := range []string{"Discover", "Launch", "Create"} {
		footer := model.resourceFooterPlan(Option{EnterLabel: label}, true).Render()
		if !strings.Contains(ansi.Strip(footer[0]), label) {
			t.Fatalf("footer omitted %s", label)
		}
	}
}
