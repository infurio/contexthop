package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// A footer plan orders hints and reserves essential controls before fitting.
// Command definitions continue to own action availability and key mappings.
type footerHintRow struct{ hints, pinned []string }

type resourceFooterPlan struct {
	width   int
	version string
	rows    [footerHeight]footerHintRow
}

func (p resourceFooterPlan) Render() [footerHeight]string {
	var lines [footerHeight]string
	for i, row := range p.rows {
		lines[i] = completeShortcutRow(p.width, row.hints, row.pinned)
	}
	lines[1] = brandFooter(lines[1], p.version, p.width)
	return lines
}

func (m listModel) resourceFooterPlan(selected Option, hasSelection bool) resourceFooterPlan {
	width := m.contentWidth()
	plan := resourceFooterPlan{width: width, version: m.version}
	if m.composeSelection {
		commands := m.browserCommands(selected, hasSelection)
		primary := []string{}
		if (hasSelection || m.hasSelectedContext()) && !m.disableEnter {
			primary = launchKeyHints(width, m.canApplyShell)
		}
		if hasSelection && !m.searching {
			label := "Stage"
			if selected.Name == m.selectedName {
				label = "Unstage"
			}
			primary = append(primary, commandHint(commands, "stage", label, width))
		}
		if hasSelection && (isBrowserAction(selected) || selected.OpenScreen != "") {
			primary = []string{shortcut("enter", "Open")}
		}
		primary = append(primary, commandHint(commands, "open-console", "Browser", width))
		secondary := []string{shortcut("/", "Filter"), commandHint(commands, "save-selection-workspace", "Save workspace", width), commandHint(commands, "ui:options", "", width)}
		help := shortcut("?", "Help")
		if m.searching {
			secondary = []string{shortcut("esc", "Keep filter"), shortcut("tab", "Next tab"), commandHint(commands, "save-selection-workspace", "Save workspace", width)}
			help = shortcut("F1", "Help")
		}
		joinShared := m.snapshot.Scope != "shared" && m.snapshot.SharedConfig != nil && m.snapshot.SharedConfigError == ""
		if joinShared {
			secondary = append([]string{commandHint(commands, "follow-shared", "", width)}, secondary...)
		}
		plan.rows[0] = footerHintRow{hints: primary}
		if width < 60 && !m.searching {
			secondary = []string{}
			if hasSelection {
				label := "Stage"
				if selected.Name == m.selectedName {
					label = "Unset"
				}
				secondary = append(secondary, commandHint(commands, "stage", label, width))
			}
			secondary = append(secondary, commandHint(commands, "ui:options", "", width))
			if joinShared {
				secondary = []string{commandHint(commands, "follow-shared", "", width), keyStyle.Render("[o]")}
			}
			plan.rows[1] = footerHintRow{hints: secondary, pinned: []string{keyStyle.Render("[?]")}}
		} else {
			plan.rows[1] = footerHintRow{hints: secondary, pinned: []string{help}}
		}
	} else if m.modalActions && m.searching {
		hints := []string{}
		if hasSelection && !m.disableEnter {
			hints = append(hints, shortcut("enter", m.selectionEnterLabel(selected)))
		}
		plan.rows[0] = footerHintRow{hints: hints, pinned: []string{shortcut("esc", "Keep filter")}}
		plan.rows[1] = footerHintRow{hints: []string{shortcut("↑/↓", "Move")}, pinned: []string{shortcut("F1", "Help"), shortcut("ctrl+c", "Quit")}}
	} else {
		contextual, global := m.pickerFooterHints(selected, hasSelection)
		plan.rows = [footerHeight]footerHintRow{{hints: contextual}, {hints: global}}
	}
	return plan
}

func (m listModel) pickerFooterHints(selected Option, hasSelection bool) ([]string, []string) {
	if !m.resourceBrowser {
		return nil, nil
	}
	contextual := []string{}

	if m.filter != "" {
		contextual = append(contextual, shortcut("esc", "Clear search"))
	} else if m.selectionClearLabel != "" {
		contextual = append(contextual, shortcut("esc", "Clear "+m.selectionClearLabel))
	}
	if m.modalActions && !m.hideSearch {
		contextual = append([]string{shortcut("/", "Search")}, contextual...)
	}
	if hasSelection {
		for index := len(selected.Actions) - 1; index >= 0; index-- {
			action := selected.Actions[index]
			contextual = append([]string{shortcut(action.Key, action.footerLabel())}, contextual...)
		}
	}
	if hasSelection && !m.disableEnter {
		contextual = append([]string{shortcut("enter", firstNonEmptyUI(selected.EnterLabel, m.enterLabel, "Select"))}, contextual...)
	}
	global := []string{
		shortcut("n", "New"), shortcut("h", m.hiddenToggleLabel()),
		shortcut("ctrl+r", "Reuse"), shortcut("r", "Refresh"), shortcut("?", "Help"), shortcut("q", "Close"),
	}
	if m.contentWidth() < 110 {
		global = []string{shortcut("h", m.hiddenToggleLabel()), shortcut("n", "New"), shortcut("?", "Help"), shortcut("q", "Close")}
	}

	if m.dimension == "workspace" {
		global = []string{shortcut("i", "Info"), shortcut("d", "Discover"), shortcut("/", "Search"), shortcut("?", "Help"), shortcut("q", "Close")}
	}
	if m.operationDetails != "" {
		global = append([]string{shortcut("o", "Results")}, global...)
	}
	return contextual, global
}

// Keep whole hints and reserve Help/Close so overflow is discoverable rather
// than clipping the last action halfway through its label.
func completeShortcutRow(width int, hints, pinned []string) string {
	chosen := []string{}
	for _, hint := range hints {
		candidate := append(append(append([]string{}, chosen...), hint), pinned...)
		if lipgloss.Width(strings.Join(candidate, "  ")) <= width {
			chosen = append(chosen, hint)
		}
	}
	return fitColumn(strings.Join(append(chosen, pinned...), "  "), width)
}
