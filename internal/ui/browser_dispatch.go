package ui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/infurio/contexthop/internal/state"
)

// Local navigation commands stay here; domain actions cross the controller flow
// boundary with the highlighted option and never execute provider code in a view.
func (m AppModel) executeBrowserCommand(action KeyAction, selected Option) (tea.Model, tea.Cmd) {
	frame := &m.stack[len(m.stack)-1]
	switch action.Action {
	case "ui:toggle-adc":
		return m.toggleADC()
	case "ui:reset-adc":
		delete(m.draft, ScreenShellADCOverride)
		return m, nil
	case "ui:options":
		return m.openOptions(selected)
	case "follow-shared":
		if !m.canApplyShell {
			m.push(Picker{Screen: "shell-integration", Title: "Shell integration required", Description: "To use the shared config here, enable Zsh integration:\n\neval \"$(chop shell-init zsh --in-place)\"", HideSearch: true, DisableEnter: true}, m.draft)
			return m, nil
		}
		return m.selectPickerOption(Option{}, "follow-shared")
	case "next-default":
		return m.useNextShell("default-shell")
	case "next-launch":
		return m.useNextShell("launch-shell")
	case "next-apply":
		return m.useNextShell("apply-shell")
	case "stage":
		return m.useNextShell("stage")
	case "ui:quit":
		return m, tea.Quit
	case "ui:info":
		return m.resourceUtility("i")
	case "ui:status":
		return m.resourceUtility("o")
	case "ui:clear":
		return m.clearSelection()
	case "ui:reuse":
		m.pushConfigured(ScreenReuse)
		return m, nil
	case "ui:refresh":
		m.snapshot = state.InspectLocal()
		m.probing, m.probed, m.probeErr = true, false, nil
		return m, func() tea.Msg { return probeMessage(state.Probe(context.Background())) }
	case "ui:scope":
		m.browseAll = !m.browseAll
		if picker, ok := m.browserPicker(frame.screen); ok {
			m.replaceBrowserRoot(picker)
		}
		return m, nil
	case "ui:hidden":
		if m.showHidden == nil {
			m.showHidden = map[Screen]bool{}
		}
		frame.picker.ShowHidden = !frame.picker.ShowHidden
		m.showHidden[frame.screen] = frame.picker.ShowHidden
		frame.cursor, frame.cursorKey = 0, ""
		frame.focusOption(selected.Name)
		return m, nil
	case "save-selection-workspace":
		if m.nextShell(m.current()).Available {
			return m.useNextShell(action.Action)
		}
		selected = Option{}
	case "import-local-contexts":
		selected.WorkLabel, selected.Cancellable = "Reading local contexts", true
	case "add-resource":
		selected = Option{}
		if m.flow == nil {
			m.pushConfigured(ScreenCatalogAction)
			return m, nil
		}
	}
	for _, operation := range frame.picker.OperationActions {
		if operation.Action == action.Action {
			selected = Option{FlowDraft: frame.picker.OperationDraft}
			break
		}
	}
	return m.selectPickerOption(selected, action.Action)
}
