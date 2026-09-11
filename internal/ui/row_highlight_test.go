package ui

import (
	"regexp"
	"strings"
	"testing"
)

// Track terminal SGR state, rather than merely checking that a colour escape
// exists somewhere in the row. A tag reset used to leave SOURCE unhighlighted.
func rowColoursAt(text string, offset int) (string, string) {
	fg, bg := "", ""
	for _, match := range regexp.MustCompile("\x1b\\[([0-9;]*)m").FindAllStringSubmatch(text[:offset], -1) {
		codes := strings.Split(match[1], ";")
		for i := 0; i < len(codes); i++ {
			switch codes[i] {
			case "", "0":
				fg, bg = "", ""
			case "39":
				fg = ""
			case "49":
				bg = ""
			case "38", "48":
				if i+4 < len(codes) && codes[i+1] == "2" {
					value := strings.Join(codes[i+2:i+5], ",")
					if codes[i] == "38" {
						fg = value
					} else {
						bg = value
					}
					i += 4
				}
			}
		}
	}
	return fg, bg
}

func TestWorkspaceHighlightContinuesAfterTags(t *testing.T) {
	for _, test := range []struct {
		name, fg, bg   string
		staged, hidden bool
	}{
		{"cursor", "0,0,0", "0,255,255", false, false},
		{"staged", "152,251,152", "16,53,40", true, false},
		{"hidden", "112,128,144", "16,40,42", false, true},
		{"staged-cursor", "0,0,0", "152,251,152", true, false},
		{"hidden-staged-cursor", "0,0,0", "152,251,152", true, true},
	} {
		for _, width := range []int{120, 180} {
			t.Run(test.name, func(t *testing.T) {
				option := Option{Name: "fictional", Label: "Fictional workspace", IdentityAccount: "person@example.invalid", Source: "MANUAL", Hidden: test.hidden,
					Tags: []Tag{{Name: "Prod", Color: "#e345ab"}, {Name: "Work", Color: "#123456"}}}
				m := listModel{dimension: "workspace", resourceBrowser: true, composeSelection: true, width: width, options: []Option{option}, showHidden: true}
				if test.staged {
					m.selectedName = option.Name
					if !strings.Contains(test.name, "cursor") {
						m.cursor = 1
					}
				}
				for _, row := range (resourceTableBody{model: m, options: []Option{option}, showHead: true}).Render(12) {
					source := strings.Index(row, "MANUAL")
					if source < 0 {
						continue
					}
					fg, bg := rowColoursAt(row, source)
					if fg != test.fg || bg != test.bg {
						t.Fatalf("SOURCE lost row styling: fg=%s bg=%s; %q", fg, bg, row)
					}
					for _, tag := range []struct{ name, color string }{{"Prod", "227,69,171"}, {"Work", "18,52,86"}} {
						pos := strings.Index(row, tag.name)
						fg, bg = rowColoursAt(row, pos)
						if fg != tag.color || bg != test.bg {
							t.Fatalf("tag %s lost colour/highlight: fg=%s bg=%s", tag.name, fg, bg)
						}
					}
					return
				}
				t.Fatal("missing workspace row")
			})
		}
	}
}
