package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCompactFieldAllocationPreservesBadgesAndMarksOmissions(t *testing.T) {
	fields := []contextDisplayField{{component: contextIdentity, label: "id", value: "abcdefghij", adc: true}, {component: contextProject, label: "project", value: "payments"}}
	before := append([]contextDisplayField(nil), fields...)
	for _, tc := range []struct {
		width int
		text  string
	}{
		{0, ""}, {10, "…"}, {11, "…"}, {14, "…"},
		{15, "id: … [ADC] · …"},
		{24, "id: … [ADC] · project: …"},
		{40, "id: abcdefghij [ADC] · project: payments"},
	} {
		got := ansi.Strip(fitHeaderFields(fields, tc.width, false))
		if got != tc.text || ansi.StringWidth(got) > tc.width {
			t.Fatalf("width %d: got %q, want %q", tc.width, got, tc.text)
		}
	}
	// Without a trailing field, the identity and badge fit in eleven columns.
	if got := ansi.Strip(fitHeaderFields(fields[:1], 11, false)); got != "id: … [ADC]" {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(fields, before) {
		t.Fatal("allocation changed display data")
	}
}

func TestCompactWrappingKeepsSelectionMarkers(t *testing.T) {
	m := listModel{resourceBrowser: true, width: 60, height: 14, nextShellPreview: &LaunchPreview{Available: true, Fields: []PickerField{{Label: "id", Value: strings.Repeat("account", 8)}, {Label: "project", Value: strings.Repeat("project", 8)}, {Label: "ADC", Value: "identity"}}, FieldStates: map[string]string{"id": "staged", "project": "highlighted"}}}
	lines := strings.Split(ansi.Strip(m.resourceSelection()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "✓") || !strings.Contains(lines[0], "[ADC]") || !strings.Contains(lines[1], "›") {
		t.Fatal(lines)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > m.contentWidth() {
			t.Fatal("overflow", line)
		}
	}
	m.height = 12
	if lines := strings.Split(m.resourceSelection(), "\n"); len(lines) != 1 {
		t.Fatal("short terminal must reserve filter space", lines)
	}
}
