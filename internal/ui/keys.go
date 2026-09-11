package ui

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

func (m AppModel) updateKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if shortcutKey(message) == "f1" || shortcutKey(message) == "?" && (m.helpVisible || !m.acceptsText()) {
		return m.toggleHelp()
	}
	if m.helpVisible && shortcutKey(message) != "ctrl+c" {
		if shortcutKey(message) == "esc" {
			m.helpVisible = false
		}
		lines, height := m.helpViewport()
		limit := max(0, len(lines)-height)
		switch shortcutKey(message) {
		case "down":
			m.helpOffset = min(limit, m.helpOffset+1)
		case "up":
			m.helpOffset = max(0, m.helpOffset-1)
		case "pgdown":
			m.helpOffset = min(limit, m.helpOffset+height)
		case "pgup":
			m.helpOffset = max(0, m.helpOffset-height)
		case "home":
			m.helpOffset = 0
		case "end":
			m.helpOffset = limit
		}
		return m, nil
	}
	if m.working {
		if shortcutKey(message) == "esc" && m.workCancel != nil {
			m.workCancel()
			m.workStopping = true
			m.workLabel = "Stopping operation · retaining completed results…"
			return m, nil
		}
		if shortcutKey(message) == "ctrl+c" || shortcutKey(message) == "q" {
			if m.workCancel != nil {
				m.workCancel()
				m.workStopping = true
				m.quitAfterWork = true
				m.workLabel = "Stopping operation before closing…"
				return m, nil
			}
			return m, tea.Quit
		}
		return m, nil
	}
	if shortcutKey(message) == "ctrl+c" {
		return m, tea.Quit
	}

	return m.updatePickerKey(message)
}

func (m AppModel) updatePickerKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	frame := &m.stack[len(m.stack)-1]
	if frame.screen == screenOptions {
		key := shortcutKey(message)
		for _, action := range frame.picker.optionsCommands {
			if key != "enter" && action.Key != "" && action.matches(key) {
				return m.chooseOption(action.Action)
			}
		}
	}
	if frame.picker.ReadOnlyText != "" {
		return m.updateReadOnlyKey(message)
	}
	if frame.picker.Input != nil {
		return m.updateInputKey(message)
	}
	if frame.picker.HideSearch && frame.picker.OperationDetails != "" && shortcutKey(message) == "i" {
		return m.resourceUtility("o")
	}

	if !frame.picker.ResourceBrowser && frame.picker.ShowSelectedInfo && !frame.searching && shortcutKey(message) == "i" {
		return m.resourceUtility("i")
	}
	filtered := filteredPickerOptions(frame.picker, frame.filter)
	key := shortcutKey(message)
	if frame.picker.ResourceBrowser {
		switch key {
		case "/":
			if frame.picker.ModalActions && !frame.picker.HideSearch && !frame.searching {
				frame.searching = true
				return m, nil
			}
		case KeyClearSearch:
			frame.filter = ""
			frame.cursor, frame.cursorKey = 0, ""
			return m, nil
		}
	}
	if frame.picker.ResourceBrowser && (key == "tab" || key == "shift+tab" || key == "left" || key == "right") {
		m.switchResourceTab(browserTarget(frame.screen, key))
		return m, nil
	}
	if frame.picker.ResourceBrowser {
		selected := Option{}
		if len(filtered) > 0 {
			selected = filtered[min(frame.cursor, len(filtered)-1)]
		}
		for _, action := range m.pickerModel(*frame).browserCommands(selected, len(filtered) > 0) {
			if frame.searching && !action.availableDuringSearch() {
				continue
			}
			if action.matches(key) {
				return m.executeBrowserCommand(action, selected)
			}
		}
	}
	if frame.picker.ModalActions {
		if frame.searching && (key == "tab" || key == "shift+tab") {
			frame.searching = false
			return m, nil
		}
		if frame.searching && key == "esc" {
			frame.searching = false
			if !frame.picker.ResourceBrowser {
				frame.filter = ""
				frame.cursor, frame.cursorKey = 0, ""
			}
			return m, nil
		}
		if frame.searching && key == "enter" {
			if frame.picker.ResourceBrowser && m.composeSelection {
				return m, nil
			}
			frame.searching = false
			if frame.picker.ResourceBrowser && len(filtered) > 0 && !frame.picker.DisableEnter {
				return m.selectPickerOption(filtered[min(frame.cursor, len(filtered)-1)], "")
			}
			return m, nil
		}
		if !frame.searching && key == "/" {
			frame.searching = true
			return m, nil
		}
	}
	if frame.picker.ResourceBrowser && !frame.searching {
		if target := browserTarget(frame.screen, key); target != "" {
			m.switchResourceTab(target)
			return m, nil
		}
	}

	if len(filtered) > 0 && (!frame.picker.ModalActions || !frame.searching) {
		frame.cursor = min(frame.cursor, len(filtered)-1)
		selected := filtered[frame.cursor]
		for _, action := range selected.Actions {
			if key == action.Key {
				return m.selectPickerOption(selected, action.Action)
			}
		}
	}
	switch key {
	case "esc":
		if frame.picker.ResourceBrowser {
			m.identityRecovery = nil
		}
		if frame.picker.ResourceBrowser && frame.filter != "" {
			frame.filter = ""
			frame.cursor, frame.cursorKey = 0, ""
			return m, nil
		}
		if frame.picker.EscapeAction != "" {
			return m.selectPickerOption(Option{Name: frame.picker.EscapeAction}, "")
		}
		if frame.picker.ResourceBrowser && m.popResourceSelection() {
			if picker, ok := m.browserPicker(frame.screen); ok {
				picker.Focus = m.draft[frame.screen]
				m.replaceBrowserRoot(picker)
			}
			return m, nil
		}
		if frame.picker.ResourceBrowser && (m.composeSelection || len(m.stack) == 1) {
			return m, nil
		}
		if len(m.stack) == 1 {
			return m, tea.Quit
		}
		if !frame.picker.KeepDraftOnCancel {
			m.draft = cloneDraft(frame.draftBefore)
		}
		m.stack = m.stack[:len(m.stack)-1]
		if m.current().picker.ResourceBrowser {
			m.identityRecovery = nil
		}
		return m, nil
	case "home":
		frame.cursor = 0
	case "end":
		frame.cursor = max(0, len(filtered)-1)
	case "pgup":
		frame.cursor = max(0, frame.cursor-max(1, m.height/2))
	case "pgdown":
		frame.cursor = min(max(0, len(filtered)-1), frame.cursor+max(1, m.height/2))
	case "up":
		if frame.cursor > 0 {
			frame.cursor--
		}
	case "down":
		if frame.cursor+1 < len(filtered) {
			frame.cursor++
		}
	case "enter":
		if len(filtered) == 0 || frame.picker.DisableEnter {
			return m, nil
		}
		frame.cursor = min(frame.cursor, len(filtered)-1)
		return m.selectPickerOption(filtered[frame.cursor], "")
	case "backspace", "ctrl+h":
		if frame.picker.HideSearch || (frame.picker.ModalActions && !frame.searching) {
			return m, nil
		}
		characters := []rune(frame.filter)
		if len(characters) > 0 {
			frame.filter = string(characters[:len(characters)-1])
			frame.cursor, frame.cursorKey = 0, ""
		}
	default:
		if frame.picker.HideSearch || (frame.picker.ModalActions && !frame.searching) {
			return m, nil
		}
		if message.Mod&tea.ModCtrl != 0 {
			return m, nil
		}
		text := message.Key().Text
		characters := []rune(text)
		if len(characters) == 1 && unicode.IsPrint(characters[0]) && !unicode.IsControl(characters[0]) {
			frame.filter += text
			frame.cursor, frame.cursorKey = 0, ""
		}
	}
	filtered = filteredPickerOptions(frame.picker, frame.filter)
	if len(filtered) == 0 {
		frame.cursor = 0
	} else if frame.cursor >= len(filtered) {
		frame.cursor = len(filtered) - 1
	}
	if len(filtered) > 0 {
		frame.cursorKey = filtered[frame.cursor].Name
	}
	return m, nil
}
