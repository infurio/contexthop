package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTabsConnectToPanelWithScopeOnlyInSelectionHeader(t *testing.T) {
	for _, width := range []int{35, 80, 120} {
		m := listModel{width: width, height: 25, resourceBrowser: true, dimension: "project", pickerTitle: "1 project", scopeLabel: "Projects for developer@example.com", scoped: true,
			selection: [3]string{"developer@example.com", "", ""},
			options:   []Option{{Name: "acme-sandbox", Project: true, ProjectID: "acme-sandbox", SaveStatus: "Saved"}}}
		view := ansi.Strip(m.resourceBrowserView())
		lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
		tabIndex, scopeIndex := -1, -1
		for index, line := range lines {
			if strings.Contains(line, " Projects ") || strings.Contains(line, " P ") {
				tabIndex = index
			}
			if strings.Contains(line, "[c] Clear scope") {
				scopeIndex = index
			}
			if lipgloss.Width(line) > width {
				t.Fatalf("panel exceeds width %d: %s", width, line)
			}
		}
		if tabIndex < 0 || scopeIndex >= 0 {
			t.Fatalf("tabs must remain visible without a clear-scope footer hint: %s", view)
		}
		if strings.Contains(view, "Projects for") {
			t.Fatalf("scope repeats browsing selection: %s", view)
		}
		if width >= 80 && strings.Count(view, "developer@example.com") != 1 {
			t.Fatalf("selected identity should appear once: %s", view)
		}
		if !strings.HasPrefix(strings.TrimSpace(lines[tabIndex]), "┌─") || strings.Contains(lines[tabIndex], "┴") {
			t.Fatalf("navigation lacks a continuous panel border: %s", lines[tabIndex])
		}
		if !strings.Contains(view, "Active") || !strings.Contains(view, "Pending") {
			t.Fatal("header lacks separate active and browsing labels")
		}
	}
}

func TestClearScopeFooterOnlyWhenAvailable(t *testing.T) {
	for _, state := range []struct{ scoped, searching bool }{{false, false}, {true, true}, {true, false}} {
		m := listModel{width: 120, resourceBrowser: true, modalActions: true, scoped: state.scoped, searching: state.searching}
		if strings.Contains(ansi.Strip(m.resourceSelection()), "[c]") {
			t.Fatal("clear scope remains in header")
		}
		footer := m.resourceFooterPlan(Option{}, false).Render()
		shown := strings.Contains(ansi.Strip(strings.Join(footer[:], " ")), "[c] Clear scope")
		if shown {
			t.Fatalf("clear scope availability for %+v: %t", state, shown)
		}
	}
}

func TestResourcePanelGeometryDoesNotDependOnHighlightedDetails(t *testing.T) {
	for _, width := range []int{55, 120} {
		for _, height := range []int{16, 28} {
			m := listModel{
				width: width, height: height, resourceBrowser: true, showSelectedInfo: true,
				dimension: "project", pickerTitle: "25 projects",
				options: []Option{
					{Name: "project", Project: true, ProjectID: "project", Summary: "source:GCP\nMappings: identity:work"},
					{Name: "long", Project: true, ProjectID: "long", Summary: strings.Repeat("long mapping ", 40)},
					{Name: "\x00__discover__", Project: true, ProjectID: "Discover accessible projects…"},
				},
			}
			baseline := -1
			for _, state := range []struct {
				cursor         int
				filter, notice string
			}{
				{0, "", ""}, {1, "", "Discovery complete. All results saved."},
				{2, "", strings.Repeat("Long notice ", 30)}, {0, "no-matches", ""},
			} {
				m.cursor, m.filter, m.pickerDescription = state.cursor, state.filter, state.notice
				lines := strings.Split(strings.TrimSuffix(ansi.Strip(m.resourceBrowserView()), "\n"), "\n")
				if len(lines) != height {
					t.Fatalf("%dx%d: got %d rows", width, height, len(lines))
				}
				bottom := -1
				for i, line := range lines {
					if strings.Contains(line, "└") {
						bottom = i
						expected := "3 projects"
						if state.filter != "" {
							expected = "0 of 3 projects"
						}
						if !strings.Contains(line, expected) {
							t.Fatalf("counts missing from panel border: %q", line)
						}
					}
				}
				if baseline == -1 {
					baseline = bottom
				}
				if bottom < 0 || bottom != baseline {
					t.Fatalf("%dx%d: panel moved from %d to %d for %+v", width, height, baseline, bottom, state)
				}
			}
		}
	}
}

func TestResourceColumnsImmediatelyFollowTabs(t *testing.T) {
	m := listModel{width: 120, height: 25, resourceBrowser: true, dimension: "project",
		pickerTitle: "1 project",
		options:     []Option{{Name: "example", Project: true, ProjectID: "example"}}}
	lines := strings.Split(strings.TrimSuffix(ansi.Strip(m.resourceBrowserView()), "\n"), "\n")
	for i, line := range lines {
		if strings.Contains(line, "PROJECT ID") {
			if i == 0 || !strings.Contains(lines[i-1], "┐") || strings.Contains(lines[i-1], "┴") {
				t.Fatalf("columns are separated from tab seam: %v", lines[:i+1])
			}
			return
		}
	}
	t.Fatal("column headings not rendered")
}

func TestArrowTabsStopAtEdges(t *testing.T) {
	for _, test := range []struct {
		screen Screen
		key    string
		want   Screen
	}{
		{ScreenWorkspace, "left", ""}, {ScreenDocker, "right", ""},
		{ScreenIdentity, "right", ScreenProject}, {ScreenDocker, "left", ScreenKubernetes},
		{ScreenDocker, "tab", ScreenWorkspace}, {ScreenWorkspace, "shift+tab", ScreenDocker},
	} {
		if got := browserTarget(test.screen, test.key); got != test.want {
			t.Fatalf("%s %s: got %s, want %s", test.screen, test.key, got, test.want)
		}
	}
}

func TestFooterPreservesCompleteHintsAndHelp(t *testing.T) {
	for _, width := range []int{35, 55, 80, 110} {
		row := ansi.Strip(completeShortcutRow(width,
			[]string{shortcut("←/→", "Tabs"), shortcut("d", "Discover"), shortcut("h", "Show hidden"), shortcut("n", "New")},
			[]string{shortcut("?", "Help"), shortcut("q", "Close")}))
		if strings.Contains(row, "…") || !strings.Contains(row, "[?] Help") || !strings.Contains(row, "[q] Close") || lipgloss.Width(row) > width {
			t.Fatalf("broken footer at %d: %q", width, row)
		}
	}
}

func TestEntityNavigationUsesUppercaseTabInitials(t *testing.T) {
	pickers := map[Screen]Picker{}
	targets := map[string]Screen{"W": ScreenWorkspace, "I": ScreenIdentity, "P": ScreenProject, "K": ScreenKubernetes, "D": ScreenDocker}
	for _, screen := range targets {
		pickers[screen] = Picker{Screen: screen, ResourceBrowser: true, ModalActions: true}
	}
	for key, screen := range targets {
		model := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenWorkspace, Pickers: pickers})
		model, _ = updateApp(model, textKey(key))
		if model.CurrentScreen() != screen {
			t.Fatalf("%s navigated to %s", key, model.CurrentScreen())
		}
		if browserTarget(ScreenWorkspace, strings.ToLower(key)) != "" {
			t.Fatalf("lowercase %s still navigates", key)
		}
		model, _ = updateApp(model, textKey("/"))
		model, _ = updateApp(model, textKey(key))
		if model.CurrentScreen() != screen || model.current().filter != key {
			t.Fatal("uppercase search text switched tabs")
		}
	}
	for _, width := range []int{60, 120} {
		labels := ansi.Strip(resourcePanelTabs("workspace", width)[0])
		for key := range targets {
			if !strings.Contains(labels, key) {
				t.Fatalf("tab hotkey %s missing at width %d: %s", key, width, labels)
			}
		}
	}
}

func TestOperationMessagesUseReservedHeaderLine(t *testing.T) {
	m := listModel{width: 100, height: 22, resourceBrowser: true, composeSelection: true, showSelectedInfo: true, dimension: "project", pickerTitle: "1 project",
		pickerDescription: "Discovery complete. Projects and identity mappings were saved successfully. Open Details to inspect the results.",
		options:           []Option{{Name: "example", Project: true, ProjectID: "example", Summary: "source:PRIVATE-METADATA\nMappings: PRIVATE-MAPPING"}},
	}
	view := ansi.Strip(m.resourceBrowserView())
	if !strings.Contains(view, "Success:") || !strings.Contains(view, "i Details") || strings.Contains(view, "PRIVATE-METADATA") || strings.Contains(view, "PRIVATE-MAPPING") {
		t.Fatalf("incorrect status presentation: %s", view)
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, "┌") {
			if i == 0 || !strings.Contains(lines[i-1], "Success:") || !strings.Contains(lines[i+1], "PROJECT ID") {
				t.Fatal("status is not above tab border")
			}
		}
	}
	m.pickerDescription = ""
	if status := m.resourceStatus(); status.message != "" {
		t.Fatal("empty status rendered")
	}
}
