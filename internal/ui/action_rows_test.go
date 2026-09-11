package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestActionRowsKeepFocusedActionVisibleInShortViewport(t *testing.T) {
	for _, width := range []int{28, 74} {
		model := listModel{width: width, actionRows: true, options: []Option{
			{Label: "Application credentials (ADC)", Detail: "Disabled · workspace setting"},
			{Label: "Start subshell", Detail: "Exit to return to your original shell."},
			{Label: "Use in current shell", Detail: "Switch the shell that opened ContextHop."},
		}}
		for cursor := range model.options {
			model.cursor = cursor
			body := pickerListBody{model: model, options: model.options}
			for _, height := range []int{1, 3, 8, 16} {
				lines := body.Render(height)
				if len(lines) > height {
					t.Fatal("viewport overflow")
				}
				if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "> ") {
					t.Fatalf("focus missing at width %d height %d cursor %d", width, height, cursor)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > model.contentWidth() {
						t.Fatalf("line too wide: %q", line)
					}
				}
			}
		}
	}
}
