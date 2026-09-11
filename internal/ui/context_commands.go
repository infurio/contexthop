package ui

import "strings"

func (a KeyAction) matches(key string) bool {
	if a.Key != "" && a.Key == key {
		return true
	}
	for _, alias := range a.Aliases {
		if alias == key {
			return true
		}
	}
	return false
}
func (a KeyAction) footerLabel() string {
	if a.Footer != "" {
		return a.Footer
	}
	return footerActionLabel(a.Label)
}
func (a KeyAction) helpKey() string {
	keys := append([]string{a.Key}, a.Aliases...)
	for i := range keys {
		keys[i] = displayShortcutKey(keys[i])
	}
	return strings.Join(keys, " / ")
}

// Available actions are also the dispatch table: omitted actions cannot execute.

// Footer priority is presentation policy; availability and labels come from commands.
func commandHints(commands []KeyAction, keys ...string) []string {
	var hints []string
	for _, key := range keys {
		for _, action := range commands {
			if action.Key == key {
				hints = append(hints, shortcut(key, action.footerLabel()))
				break
			}
		}
	}
	return hints
}

// All main tabs share the same activation and staging bindings.
func contextLaunchCommands() []KeyAction {
	return []KeyAction{
		{Key: "enter", Label: "Update shared", Footer: "Update shared", Action: "next-default"},
		{Key: "shift+enter", Aliases: []string{"ctrl+a"}, Label: "Pin", Footer: "Pin", Action: "next-apply"},
		{Key: "alt+enter", Label: "Start a subshell", Footer: "Subshell", Action: "next-launch"},
		{Key: "space", Label: "Stage or unstage this resource", Footer: "Stage", Action: "stage"},
	}
}

// Text entry suppresses printable commands. These operations intentionally
// remain available through their control or Enter bindings while filtering.
func (a KeyAction) availableDuringSearch() bool {
	switch a.Action {
	case "follow-shared", "next-default", "next-launch", "next-apply", "save-selection-workspace", "ui:reuse", "open-console":
		return true
	}
	return false
}

func commandHint(actions []KeyAction, name, label string, width int) string {
	for _, action := range actions {
		if action.Action != name {
			continue
		}
		key := action.Key
		if width < 90 {
			switch key {
			case "enter":
				key = "↵"
			case "shift+enter":
				key = "⇧↵"
			case "alt+enter":
				key = "⌥↵"
			}
		}
		if label == "" {
			label = action.footerLabel()
		}
		return shortcut(key, label)
	}
	return ""
}
