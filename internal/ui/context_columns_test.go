package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/state"
)

func TestContextADCBadgesAreIndependentAndSurviveTruncation(t *testing.T) {
	for _, width := range []int{100, 124, 164} {
		for _, active := range []bool{false, true} {
			for _, selected := range []bool{false, true} {
				path := "/session/disabled-google-credentials.json"
				if active {
					path = "/identity/application_default_credentials.json"
				}
				mode := "off"
				if selected {
					mode = "identity"
				}
				m := listModel{width: width, height: 24, resourceBrowser: true,
					snapshot:         state.Snapshot{Observed: state.Component{Identity: strings.Repeat("a", 80), ADC: path}},
					nextShellPreview: &LaunchPreview{Available: true, Fields: []PickerField{{Label: "id", Value: strings.Repeat("b", 80)}, {Label: "ADC", Value: mode}}}}
				lines := strings.Split(ansi.Strip(m.resourceSelection()), "\n")
				start := ansi.StringWidth(strings.Split(lines[0], "Selected")[0])
				left, right := ansi.Cut(lines[1], 0, start), ansi.Cut(lines[1], start, width)
				if strings.Contains(left, "[ADC]") != active || strings.Contains(right, "[ADC]") != selected {
					t.Fatalf("%d active=%t selected=%t: %s", width, active, selected, lines[1])
				}
				if len(lines) != 5 || !strings.HasPrefix(lines[4], "Docker") {
					t.Fatal("header rows moved", lines)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > m.contentWidth() {
						t.Fatal("header overflow", line)
					}
				}
			}
		}
	}
}

func TestContextErrorsStayVisibleAndPreviewIsNotChanged(t *testing.T) {
	p := &LaunchPreview{Available: true, Error: "project does not match identity", Fields: []PickerField{{Label: "identity", Value: "user@example.com"}, {Label: "ADC", Value: "identity"}}}
	m := listModel{width: 100, height: 24, nextShellPreview: p}
	view := ansi.Strip(strings.Join(m.resourceHeader(), "\n"))
	if !strings.Contains(view, "Needs attention: "+p.Error) || !strings.Contains(view, "user@example.com [ADC]") || p.Fields[0].Value != "user@example.com" {
		t.Fatal(view)
	}
}

func TestCompactHeaderKeepsEnabledADCBadge(t *testing.T) {
	m := listModel{width: 35, height: 12, resourceBrowser: true,
		nextShellPreview: &LaunchPreview{Available: true, Fields: []PickerField{
			{Label: "id", Value: strings.Repeat("long-account", 10)},
			{Label: "project", Value: "project"}, {Label: "ADC", Value: "identity"},
		}}}
	lines := strings.Split(ansi.Strip(m.resourceSelection()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "[ADC]") || ansi.StringWidth(lines[0]) > m.contentWidth() {
		t.Fatal("compact header obscured ADC state", lines)
	}
}

func TestSingleRowTabsDoNotMoveOnSelection(t *testing.T) {
	for _, width := range []int{35, 60, 80, 164} {
		baseline := ""
		for _, tab := range applicationTabs {
			lines := resourcePanelTabs(string(tab.screen), width)
			plain := strings.NewReplacer("[", "─", "]", "─").Replace(ansi.Strip(lines[0]))
			if len(lines) != 1 || strings.ContainsAny(plain, "│[]") || (baseline != "" && plain != baseline) {
				t.Fatal("tab geometry changes with selection", width, lines)
			}
			baseline = plain
			if ansi.StringWidth(lines[0]) != width || !strings.HasPrefix(plain, "┌─") || !strings.HasSuffix(plain, "─┐") {
				t.Fatal("tabs do not fill the panel border", lines)
			}
		}
	}
}

func TestFooterVersionUsesOnlySpareSpace(t *testing.T) {
	m := NewAppModel(AppOptions{Version: "v1.2.3", ResourceBrowser: true, ComposeSelection: true, Pickers: map[Screen]Picker{ScreenWorkspace: {Screen: ScreenWorkspace, ResourceBrowser: true, Dimension: "workspace"}}})
	m.width, m.height = 164, 24
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(m.View().Content), "\n"), "\n")
	if !strings.HasSuffix(lines[len(lines)-1], "ContextHop · v1.2.3") || ansi.StringWidth(lines[len(lines)-1]) != 163 {
		t.Fatal("build version missing or not right aligned", lines[len(lines)-1])
	}
	shortcuts := "[/] Filter  [o] Options  [?] Help"
	if brandFooter(shortcuts, "1.2.3", ansi.StringWidth(shortcuts)+5) != shortcuts {
		t.Fatal("branding displaced shortcuts")
	}
}

func TestResizePrioritizesSelectedOverComparisons(t *testing.T) {
	preview := &LaunchPreview{Available: true, Fields: []PickerField{
		{Label: "id", Value: "selected@example.com"},
		{Label: "project", Value: "selected-project"},
		{Label: "k8s", Value: "selected-cluster/default"},
	}}
	m := listModel{resourceBrowser: true, nextShellPreview: preview,
		snapshot: state.Snapshot{Scope: "local", SharedConfig: &state.Manifest{
			Expected: state.Component{Identity: "shared@example.com"}},
			Observed: state.Component{Identity: "active@example.com"}}}
	// Shrink each dimension independently, then expand to restore comparisons.
	for _, size := range []struct {
		width, height  int
		active, shared bool
	}{
		{164, 30, true, true},
		{120, 30, true, false},
		{75, 30, false, false},
		{164, 20, true, false},
		{164, 17, false, false},
		{75, 12, false, false},
		{164, 30, true, true},
	} {
		m.width, m.height = size.width, size.height
		view := ansi.Strip(m.resourceSelection())
		if strings.Contains(view, "Active") != size.active || strings.Contains(view, "Shared config") != size.shared || !strings.Contains(view, "Selected") {
			t.Fatalf("size %+v: %s", size, view)
		}
		if size.height >= 16 {
			for _, field := range preview.Fields {
				if !strings.Contains(view, field.Value) {
					t.Fatalf("selected value obscured at %+v: %s", size, view)
				}
			}
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > m.contentWidth() {
				t.Fatalf("overflow at %+v: %s", size, line)
			}
		}
		if m.nextShellPreview != preview || m.snapshot.SharedConfig.Expected.Identity != "shared@example.com" {
			t.Fatal("resizing changed context state")
		}
	}
}
