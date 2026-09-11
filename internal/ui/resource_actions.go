package ui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
)

func (m AppModel) resourceUtility(key string) (tea.Model, tea.Cmd) {
	frame := m.current()
	options := filteredPickerOptions(frame.picker, frame.filter)
	var selected Option
	if len(options) > 0 {
		selected = options[min(frame.cursor, len(options)-1)]
	}
	switch key {
	case "i":
		description := firstNonEmptyUI(selected.Summary, selected.Detail, selected.Label, selected.Name, "No resource highlighted")
		if status := m.pickerModel(frame).resourceStatus(); status.message != "" {
			description = status.message + "\n\n" + description
		}
		if m.composeSelection {
			preview := m.nextShell(frame)
			if notice := selectionNotice(&preview); notice.message != "" {
				description = notice.message + "\n\n" + description
			}
		}
		m.push(Picker{Screen: "resource-details", Title: firstNonEmptyUI(selected.Label, selected.Name, "Details"), ReadOnlyText: formatResourceDetails(description), HideSearch: true, DisableEnter: true}, m.draft)
	case "o":
		details, title := frame.picker.OperationDetails, "Last operation"
		if frame.screen == ScreenConfirm {
			title = "Change details"
		}
		if details == "" {
			details, title = frame.picker.Description, "Status"
		}
		if details == "" {
			return m, nil
		}
		m.push(Picker{Screen: "operation-details", Title: title, ReadOnlyText: details, HideSearch: true, DisableEnter: true}, m.draft)
	case "ctrl+w":
		return m.selectPickerOption(Option{}, "save-selection-workspace")
	}
	return m, nil
}

func formatResourceDetails(text string) string {
	return strings.NewReplacer("source:", "Source: ", "verified:", "Verified by: ", "observed:", "Last observed: ", "endpoint:", "Endpoint: ").Replace(strings.TrimSpace(text))
}

func (m AppModel) clearSelection() (tea.Model, tea.Cmd) {
	m.identityRecovery = nil
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker, ScreenWorkspace, ScreenWorkspaceSource, ScreenShellADCOverride} {
		delete(m.draft, screen)
	}
	if picker, ok := m.browserPicker(m.current().screen); ok {
		picker.Description = "Cleared all selected resources. Active shell unchanged."
		m.replaceBrowserRoot(picker)
	}
	return m, nil
}
