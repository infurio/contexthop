package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/state"
)

func TestContextHeaderThresholds(t *testing.T) {
	for _, tc := range []struct {
		width, height, rows int
		active, shared      bool
	}{
		{34, 24, 2, false, false}, {35, 24, 5, false, false},
		{57, 14, 2, false, false}, {58, 14, 3, false, false},
		{140, 13, 2, false, false}, {140, 15, 3, false, false},
		{140, 16, 5, false, false}, {140, 17, 5, false, false},
		{89, 24, 5, false, false}, {90, 24, 5, true, false},
		{140, 18, 5, true, false}, {140, 21, 5, true, false},
		{139, 24, 5, true, false}, {140, 22, 5, true, true},
		{140, 0, 5, true, true},
	} {
		m := listModel{resourceBrowser: true, width: tc.width + pageGutterWidth + pageRightMargin, height: tc.height, snapshot: state.Snapshot{Scope: "local"}, nextShellPreview: &LaunchPreview{}}
		lines := m.resourceHeader()
		view := ansi.Strip(strings.Join(lines, "\n"))
		if len(lines) != tc.rows+1 || strings.Contains(view, "Active") != tc.active || strings.Contains(view, "Shared config") != tc.shared || !strings.Contains(view, "Selected") {
			t.Fatalf("%+v: %q", tc, view)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > tc.width {
				t.Fatalf("%+v overflow: %q", tc, line)
			}
		}
	}
}

func TestContextDisplayProjectionDoesNotMutateSources(t *testing.T) {
	p := &LaunchPreview{Available: true, Fields: []PickerField{{Label: "identity", Value: "selected"}, {Label: "kubernetes", Value: "cluster/apps"}, {Label: "ADC", Value: "identity"}}, FieldStates: map[string]string{"identity": "staged", "kubernetes": "highlighted"}}
	before := *p
	before.Fields = append([]PickerField(nil), p.Fields...)
	before.FieldStates = map[string]string{"identity": "staged", "kubernetes": "highlighted"}
	manifest := &state.Manifest{Expected: state.Component{Identity: "shared", Kubernetes: "cluster", Namespace: "apps"}, ADCMode: "identity"}
	saved := *manifest
	m := listModel{nextShellPreview: p, snapshot: state.Snapshot{Scope: "local", SharedConfig: manifest}}
	for _, width := range []int{35, 75, 164} {
		for _, height := range []int{12, 14, 30} {
			m.width, m.height = width, height
			m.resourceHeader()
		}
	}
	if !reflect.DeepEqual(*p, before) || !reflect.DeepEqual(*manifest, saved) {
		t.Fatal("rendering mutated source state")
	}
	selected := m.selectedContextDisplay()
	if f := selected.field(contextIdentity); f.value != "selected" || !f.adc || f.state != "staged" {
		t.Fatal(f)
	}
	if f := selected.field(contextKubernetes); f.value != "cluster/apps" || f.state != "highlighted" {
		t.Fatal(f)
	}
	if f := m.sharedContextDisplay().field(contextKubernetes); f.value != "cluster/apps" {
		t.Fatal(f)
	}
	m.snapshot.SharedConfigError = "read failed"
	if f := m.sharedContextDisplay().field(contextIdentity); f.value != "Unavailable" || f.adc {
		t.Fatal(f)
	}
	m.snapshot.SharedConfigError, m.snapshot.SharedConfig = "", nil
	if f := m.sharedContextDisplay().field(contextIdentity); f.value != "Not set" || f.adc {
		t.Fatal(f)
	}
}

func TestAllContextBadgesSurviveLongValues(t *testing.T) {
	m := listModel{width: 164, height: 30, resourceBrowser: true,
		snapshot:         state.Snapshot{Scope: "local", Observed: state.Component{Identity: strings.Repeat("active", 30), ADC: "/credentials.json"}, SharedConfig: &state.Manifest{Expected: state.Component{Identity: strings.Repeat("shared", 30)}, ADCMode: "identity"}},
		nextShellPreview: &LaunchPreview{Available: true, Fields: []PickerField{{Label: "identity", Value: strings.Repeat("selected", 30)}, {Label: "ADC", Value: "identity"}}},
	}
	lines := strings.Split(ansi.Strip(m.resourceSelection()), "\n")
	if strings.Count(lines[1], "[ADC]") != 3 || ansi.StringWidth(lines[1]) > m.contentWidth() {
		t.Fatal(lines[1])
	}
	m.snapshot.Observed.Identity = "contexthop-none"
	m.snapshot.SharedConfig.Expected.Identity = ""
	m.nextShellPreview.Fields[0].Value = "none"
	if view := ansi.Strip(m.resourceSelection()); strings.Contains(view, "[ADC]") {
		t.Fatal("ADC without an identity", view)
	}
}
