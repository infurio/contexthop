package ui

import tea "charm.land/bubbletea/v2"

// Shared shortcuts retain their meaning across all application tabs.
const (
	KeyHelp        = "f1"
	KeyConsole     = "ctrl+o"
	KeyClearSearch = "ctrl+u"
	KeyReuse       = "ctrl+r"
)

func shortcutKey(message tea.KeyPressMsg) string {
	if message.Code == tea.KeyEnter && message.Mod&tea.ModAlt != 0 {
		return "alt+enter"
	}
	if message.Code == tea.KeyEnter && message.Mod&tea.ModShift != 0 {
		return "shift+enter"
	}
	if message.Mod&tea.ModCtrl != 0 {
		return message.Keystroke()
	}
	return message.String()
}

// Spell out shifted letter keys instead of relying on uppercase notation.
func displayShortcutKey(key string) string {
	if key == "alt+enter" {
		return "option+enter"
	}
	if len(key) == 1 && key[0] >= 'A' && key[0] <= 'Z' {
		return "shift+" + string(key[0]+('a'-'A'))
	}
	return key
}
