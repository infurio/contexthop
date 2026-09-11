package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

const screenOptions Screen = "entity-options"

// The menu and direct shortcuts use the same command definitions and dispatch.
func (m AppModel) openOptions(selected Option) (tea.Model, tea.Cmd) {
	frame := m.current()
	rows := filteredPickerOptions(frame.picker, frame.filter)
	commands := m.pickerModel(frame).browserCommands(selected, len(rows) > 0)
	rowActions := map[string]bool{"ui:info": true}
	for _, action := range selected.Actions {
		rowActions[action.Action] = true
	}
	groups := map[string][]KeyAction{}
	seen := map[string]bool{}
	for _, action := range commands {
		switch action.Action {
		case "ui:options":
			continue
		}
		if action.Action == "ui:clear" && !m.hasStagedSelection() {
			continue
		}
		if action.Action == "ui:reuse" && len(m.pickers[ScreenReuse].Options) == 0 {
			continue
		}
		if seen[action.Action] {
			continue
		}
		seen[action.Action] = true
		group := "This tab"
		if rowActions[action.Action] {
			group = "Highlighted item"
		}
		switch action.Action {
		case "follow-shared", "next-default", "next-launch", "next-apply", "stage", "save-selection-workspace", "ui:clear", "ui:reuse", "ui:toggle-adc", "ui:reset-adc":
			group = "Selected context"
		}
		if action.Action == "next-apply" && !m.canApplyShell {
			action.Label = "Set up current shell"
		}
		if action.Action == "stage" {
			action.Label = "Stage highlighted resource"
			if selected.Name == m.pickerModel(frame).selectedName {
				action.Label = "Unstage highlighted resource"
			}
		}
		groups[group] = append(groups[group], action)
	}
	picker := Picker{Screen: screenOptions, Title: "Options · " + tabFor(frame.screen).label, HideSearch: true, CompactDialog: true, ActionRows: true, optionsTarget: selected}
	if len(rows) > 0 {
		picker.Title = "Options · " + firstNonEmptyUI(selected.Label, selected.Name)
	}
	for _, group := range []string{"Selected context", "Highlighted item", "This tab"} {
		for _, action := range groups[group] {
			picker.optionsCommands = append(picker.optionsCommands, action)
			keys := action.helpKey()
			detail := ""
			if action.Action == "next-default" {
				detail = "Update the shared config and follow it in this terminal. Pinned shells and subshells stay unchanged."
			}
			if action.Action == "follow-shared" {
				detail = "Follow the existing shared config without changing it or publishing Selected."
			}
			picker.Options = append(picker.Options, Option{Name: action.Action, Label: action.Label, Detail: detail, MenuGroup: group, MenuShortcut: keys})
		}
	}
	m.push(picker, m.draft)
	return m, nil
}

func (m AppModel) chooseOption(name string) (tea.Model, tea.Cmd) {
	picker := m.current().picker
	for _, action := range picker.optionsCommands {
		if action.Action == name {
			m.stack = m.stack[:len(m.stack)-1]
			return m.executeBrowserCommand(action, picker.optionsTarget)
		}
	}
	return m, nil
}

func menuShortcutLabel(label, keys string, width int) string {
	if keys == "" {
		return fitColumn(label, width)
	}
	keys = fitColumn(keys, max(1, width/2))
	left := max(1, width-ansi.StringWidth(keys)-2)
	value := fitColumn(label, left)
	return value + strings.Repeat(" ", max(1, width-ansi.StringWidth(value)-ansi.StringWidth(keys))) + keys
}
