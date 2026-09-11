package ui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestFilterBorderKeepsColumnsFixed(t *testing.T) {
	m := listModel{width: 100, height: 24, resourceBrowser: true, dimension: "project", pickerTitle: "2 projects", options: []Option{{Name: "test", Project: true, ProjectID: "test"}, {Name: "other", Project: true, ProjectID: "other"}}}
	baseline := -1
	for _, state := range []struct {
		query   string
		editing bool
	}{{"", false}, {"", true}, {"test", true}, {"test", false}, {"missing", true}} {
		m.filter, m.searching = state.query, state.editing
		lines := strings.Split(ansi.Strip(m.resourceBrowserView()), "\n")
		for i, line := range lines {
			if strings.Contains(line, "PROJECT ID") {
				if baseline < 0 {
					baseline = i
				}
				if i != baseline || !strings.Contains(lines[i-1], "┐") {
					t.Fatalf("filter moved headings: %s", strings.Join(lines, "\n"))
				}
			}
			if strings.Contains(line, "/ test") || strings.Contains(line, "/ missing") || strings.Contains(line, "type to filter") {
				if !strings.Contains(line, "└") {
					t.Fatalf("filter outside bottom border: %q", line)
				}
			}
			if strings.Contains(line, "└") && state.query != "" {
				want := "1 of 2 projects"
				if state.query == "missing" {
					want = "0 of 2 projects"
				}
				if !strings.Contains(line, want) {
					t.Fatalf("incorrect count: %q", line)
				}
				if strings.Contains(line, "▏") != state.editing {
					t.Fatalf("incorrect cursor state: %q", line)
				}
			}
		}
	}
	if baseline < 0 {
		t.Fatal("missing headings")
	}
}

func TestFilterBorderNarrowUnicodeAndColours(t *testing.T) {
	for _, editing := range []bool{false, true} {
		for width := 15; width <= 180; width++ {
			m := listModel{width: width, resourceBrowser: true, dimension: "project", filter: strings.Repeat("世界é", 40) + "tail", searching: editing}
			rendered := m.resourcePanelBottom("98 projects", 0, 0, 0, false)
			plain := ansi.Strip(rendered)
			if ansi.StringWidth(plain) != m.contentWidth() || strings.Contains(plain, "\n") {
				t.Fatalf("width %d: %q", width, plain)
			}
			if strings.Contains(plain, "▏") != editing {
				t.Fatalf("cursor at width %d: %q", width, plain)
			}
			if width >= 25 && !strings.Contains(plain, "tail") {
				t.Fatalf("query tail lost: %q", plain)
			}
		}
		m := listModel{width: 100, resourceBrowser: true, filter: "test", searching: editing}
		line := m.resourcePanelBottom("98 projects", 1, 0, 1, false)
		prompt, _ := rowColoursAt(line, strings.Index(line, "/"))
		query, _ := rowColoursAt(line, strings.Index(line, "test"))
		if editing && (prompt != "0,255,255" || query != "255,255,255") {
			t.Fatalf("editing colours: %s %s", prompt, query)
		}
		if !editing && (prompt != "112,128,144" || query != "112,128,144") {
			t.Fatalf("retained colours: %s %s", prompt, query)
		}
	}
}

func TestFilterCountExcludesHiddenAndOtherDimensions(t *testing.T) {
	m := listModel{width: 100, resourceBrowser: true, dimension: "project", filter: "test", options: []Option{{Name: "test", Project: true}, {Name: "test-hidden", Project: true, Hidden: true}, {Name: "test-identity", Identity: true}}}
	line := ansi.Strip(m.resourcePanelBottom("2 projects", len(m.filteredOptions()), 0, 1, false))
	if !strings.Contains(line, "1 of 1 projects") {
		t.Fatalf("incorrect eligible total: %s", line)
	}
	m.showHidden = true
	line = ansi.Strip(m.resourcePanelBottom("2 projects", len(m.filteredOptions()), 0, 2, false))
	if !strings.Contains(line, "2 of 2 projects") {
		t.Fatalf("incorrect visible total: %s", line)
	}
}

func TestEscapeKeepsFilterThenClears(t *testing.T) {
	m := workspaceCursorModel()
	m.stack[len(m.stack)-1].picker.ModalActions = true
	m, _ = updateApp(m, textKey("/"))
	for _, ch := range "second" {
		m, _ = updateApp(m, textKey(string(ch)))
	}
	if !m.current().searching || m.current().filter != "second" {
		t.Fatal("filter did not start")
	}
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if m.current().searching || m.current().filter != "second" || focusedResource(m) != "second" {
		t.Fatal("escape lost filter or focus")
	}
	m, _ = updateApp(m, textKey(" "))
	if m.draft[ScreenWorkspace] != "second" || m.current().filter != "second" {
		t.Fatal("stage lost filtered selection")
	}
	m, _ = updateApp(m, textKey("/"))
	if !m.current().searching || m.current().filter != "second" {
		t.Fatal("slash did not resume editing")
	}
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if m.current().searching || m.current().filter != "second" {
		t.Fatal("first escape cleared query")
	}
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if m.current().searching || m.current().filter != "" {
		t.Fatal("escape did not clear filter")
	}
	if m.draft[ScreenWorkspace] != "second" {
		t.Fatal("filter escape changed staged selection")
	}
	before := m.selectionClearLabel()
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if m.selectionClearLabel() == before {
		t.Fatal("third escape did not resume normal selection clearing")
	}
}

func TestFilterHintsFitBesideQuery(t *testing.T) {
	for _, editing := range []bool{false, true} {
		m := listModel{modalActions: true, searching: editing, filter: "test"}
		want := "/: edit · esc: clear"
		if editing {
			want = "esc: keep"
		}
		if !strings.Contains(ansi.Strip(m.borderFilterInput(80)), want) {
			t.Fatal("missing toggle and clear hints")
		}
		for width := 1; width <= 80; width++ {
			line := ansi.Strip(m.borderFilterInput(width))
			if ansi.StringWidth(line) > width {
				t.Fatalf("hint overflow at %d: %s", width, line)
			}
			if width >= 7 && !strings.Contains(line, "test") {
				t.Fatalf("hint displaced query: %s", line)
			}
		}
	}
}

func TestArrowTabsPreserveFilterAndRow(t *testing.T) {
	for _, key := range []rune{tea.KeyLeft, tea.KeyRight} {
		options := testAppOptions(ScreenProject, nil)
		options.ResourceBrowser = true
		for screen, picker := range options.Pickers {
			picker.ResourceBrowser = true
			picker.ModalActions = true
			options.Pickers[screen] = picker
		}
		options.Pickers[ScreenProject] = Picker{Screen: ScreenProject, Dimension: "project", ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "one", Project: true, ProjectID: "match-one"}, {Name: "two", Project: true, ProjectID: "match-two"}}}
		m := NewAppModel(options)
		m, _ = updateApp(m, textKey("/"))
		updated, _ := m.Update(tea.PasteMsg{Content: "match"})
		m = updated.(AppModel)
		m, _ = updateApp(m, specialKey(tea.KeyDown))
		m, _ = updateApp(m, specialKey(key))
		want := ScreenIdentity
		back := tea.KeyRight
		if key == tea.KeyRight {
			want = ScreenKubernetes
			back = tea.KeyLeft
		}
		if m.CurrentScreen() != want {
			t.Fatalf("arrow did not switch to %s", want)
		}
		m, _ = updateApp(m, specialKey(back))
		if m.CurrentScreen() != ScreenProject || m.current().filter != "match" || focusedResource(m) != "two" || m.current().searching {
			t.Fatal("return lost filtered browsing position")
		}
	}
	for _, test := range []struct {
		screen Screen
		key    rune
	}{{ScreenWorkspace, tea.KeyLeft}, {ScreenDocker, tea.KeyRight}} {
		m := workspaceCursorModel()
		m.switchResourceTab(test.screen)
		frame := &m.stack[len(m.stack)-1]
		frame.picker.ModalActions = true
		frame.searching = true
		frame.filter = "query"
		m, _ = updateApp(m, specialKey(test.key))
		if m.CurrentScreen() != test.screen || !m.current().searching || m.current().filter != "query" {
			t.Fatal("edge arrow changed filtering state")
		}
	}
}

func TestAllTabCountsUseSameFormat(t *testing.T) {
	for _, tab := range []struct{ dimension, units string }{{"workspace", "workspaces"}, {"identity", "identities"}, {"project", "projects"}, {"kubernetes", "Kubernetes targets"}, {"docker", "Docker contexts"}} {
		for _, scrolling := range []bool{false, true} {
			for _, filtered := range []bool{false, true} {
				for _, showHidden := range []bool{false, true} {
					m := listModel{width: 180, resourceBrowser: true, dimension: tab.dimension, showHidden: showHidden}
					for i := 0; i < 30; i++ {
						m.options = append(m.options, Option{Name: "match", Identity: true, Project: true, Kubernetes: true, Docker: true, Hidden: i >= 28})
					}
					matches, total := 28, 28
					hidden := "2 hidden"
					if showHidden {
						matches, total = 30, 30
						hidden = "2 hidden shown"
					}
					if filtered {
						m.filter = "match"
						matches = 20
					}
					want := fmt.Sprintf("%d %s", matches, tab.units)
					if filtered {
						want = fmt.Sprintf("%d of %d %s", matches, total, tab.units)
					}
					if scrolling {
						want = fmt.Sprintf("1–10 of %d %s", matches, tab.units)
						if filtered {
							want += fmt.Sprintf(" · %d total", total)
						}
					}
					want += " · " + hidden
					if scrolling {
						want += " · ↑/↓ scroll"
					}
					line := ansi.Strip(m.resourcePanelBottom("inconsistent picker title", matches, 0, 10, scrolling))
					if !strings.HasSuffix(line, " "+want+" ─┘") {
						t.Fatalf("%s: expected %q, got %q", tab.dimension, want, line)
					}
				}
			}
		}
	}
}

func TestSlashIsTextWhileEditingFilter(t *testing.T) {
	m := workspaceCursorModel()
	m.stack[len(m.stack)-1].picker.ModalActions = true
	m, _ = updateApp(m, textKey("/"))
	for _, ch := range "context/namespace" {
		m, _ = updateApp(m, textKey(string(ch)))
	}
	if !m.current().searching || m.current().filter != "context/namespace" {
		t.Fatal("slash did not remain query text")
	}
}
