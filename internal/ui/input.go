package ui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

func (m AppModel) updateInputKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	frame := &m.stack[len(m.stack)-1]
	switch message.String() {
	case "esc":
		if len(m.stack) == 1 {
			return m, tea.Quit
		}
		m.draft = cloneDraft(frame.draftBefore)
		m.stack = m.stack[:len(m.stack)-1]
		return m, nil
	case "enter":
		value := strings.TrimSpace(frame.input)
		if value == "" && !frame.picker.Input.AllowEmpty {
			frame.inputErr = "A value is required."
			return m, nil
		}
		if validate := frame.picker.Input.Validate; validate != nil {
			if err := validate(value); err != nil {
				frame.inputErr = err.Error()
				return m, nil
			}
		}
		return m.selectPickerOption(Option{Name: value, Label: value}, "")
	case "left":
		frame.inputOffset = min(len([]rune(frame.input)), frame.inputOffset+1)
	case "right":
		frame.inputOffset = max(0, frame.inputOffset-1)
	case "home", "ctrl+a":
		frame.inputOffset = len([]rune(frame.input))
	case "end", "ctrl+e":
		frame.inputOffset = 0
	case "ctrl+u":
		characters := []rune(frame.input)
		frame.input = string(characters[len(characters)-frame.inputOffset:])
		frame.inputErr = ""
	case "delete", "backspace", "ctrl+h":
		characters := []rune(frame.input)
		position := len(characters) - frame.inputOffset
		if message.String() == "delete" {
			if position < len(characters) {
				frame.input = string(append(characters[:position], characters[position+1:]...))
				frame.inputOffset--
			}
		} else if position > 0 {
			frame.input = string(append(characters[:position-1], characters[position:]...))
		}
		frame.inputErr = ""
	default:
		frame.insertInput(message.Key().Text)
	}
	return m, nil
}

func (m AppModel) inputView(frame navigationFrame) tea.View {
	view := tea.NewView(m.inputPage(frame, terminalContentWidth(m.width), m.height))
	view.AltScreen = true
	return view
}

func (m AppModel) inputPage(frame navigationFrame, contentWidth, height int) string {
	title := frame.picker.Title
	if title == "" {
		title = "Enter value"
	}
	top := []string{titleStyle.Render(fitColumn(title, contentWidth)), ""}
	prompt := frame.picker.Input.Prompt
	if prompt == "" {
		prompt = "Value"
	}
	field := []string{headingStyle.Render(fitColumn(prompt, contentWidth))}
	placeholder := frame.picker.Input.Placeholder
	if placeholder == "" {
		placeholder = "type a value"
	}
	if frame.input == "" {
		placeholder = "e.g. " + placeholder
	}
	field = append(field, sectionLines(inputTextField(frame.input, placeholder, contentWidth, frame.inputOffset))...)
	if frame.inputErr != "" {
		errors := sectionLines(wrapText(frame.inputErr, contentWidth))
		if height > 0 {
			errors = boundedSection(errors, max(1, height-footerHeight-len(top)-len(field)-1))
		}
		field = append(field, "")
		for _, line := range errors {
			field = append(field, warnStyle.Render(line))
		}
	}
	if frame.picker.Description != "" {
		description := append(sectionLines(wrapText(frame.picker.Description, contentWidth)), "")
		if height > 0 {
			description = boundedSection(description, max(0, height-footerHeight-len(top)-len(field)))
		}
		top = append(top, description...)
	}
	top = append(top, field...)
	footer := [footerHeight]string{
		completeShortcutRow(contentWidth, []string{shortcut("enter", "Continue")}, []string{shortcut("F1", "Help")}),
		shortcutRow(contentWidth, shortcut("esc", "Back"), shortcut("ctrl+c", "Close")),
	}
	return (pageLayout{Width: contentWidth, Height: height, Top: top, Footer: footer}).Render()
}

// Paste is text, never a sequence of navigation or action keys.
func printableText(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsPrint(r) && !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}

func (frame *navigationFrame) insertInput(text string) {
	characters := []rune(frame.input)
	position := len(characters) - frame.inputOffset
	frame.input = string(characters[:position]) + printableText(text) + string(characters[position:])
	frame.inputErr = ""
}
