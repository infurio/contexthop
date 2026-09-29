package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHelpFitsStandardTerminal(t *testing.T) {
	for n, line := range strings.Split(helpText, "\n") {
		if width := utf8.RuneCountInString(line); width > 80 {
			t.Errorf("help line %d is %d columns; limit is 80", n+1, width)
		}
		if strings.ContainsAny(line, "\t\x1b") {
			t.Errorf("help line %d contains tabs or terminal escapes", n+1)
		}
	}
}
