package ui

import (
	"reflect"
	"testing"

	"github.com/infurio/contexthop/internal/state"
)

func TestPendingProjectionCompatibility(t *testing.T) {
	active := state.Component{Identity: "user", Project: "project", Kubernetes: "canonical", Namespace: "default", Docker: "desktop"}
	for _, tc := range []struct {
		name    string
		input   pendingContextInput
		changed bool
		values  map[contextComponent]string
	}{
		{name: "empty draft", input: pendingContextInput{observed: active}},
		{name: "canonical alias matches", input: pendingContextInput{selection: [3]string{"user", "project", "friendly"}, docker: "desktop", kubernetesContext: "canonical", namespace: "default", observed: active}},
		{name: "namespace differs", input: pendingContextInput{selection: [3]string{"user", "project", "friendly"}, docker: "desktop", kubernetesContext: "canonical", namespace: "apps", observed: active}, changed: true, values: map[contextComponent]string{contextIdentity: "user", contextProject: "project", contextKubernetes: "friendly/apps", contextDocker: "desktop"}},
		{name: "unspecified namespace", input: pendingContextInput{selection: [3]string{"user", "project", "friendly"}, docker: "desktop", kubernetesContext: "canonical", observed: active}},
		{name: "namespace without cluster", input: pendingContextInput{namespace: "apps", observed: active}},
		{name: "pristine partial selection", input: pendingContextInput{selection: [3]string{"user"}, pristine: true, observed: active}},
		{name: "edited partial selection", input: pendingContextInput{selection: [3]string{"user"}, observed: active}, changed: true, values: map[contextComponent]string{contextIdentity: "user"}},
		{name: "changed value overrides pristine", input: pendingContextInput{selection: [3]string{"other"}, pristine: true, observed: active}, changed: true, values: map[contextComponent]string{contextIdentity: "other"}},
		{name: "namespace overrides pristine", input: pendingContextInput{selection: [3]string{"", "", "friendly"}, kubernetesContext: "canonical", namespace: "apps", pristine: true, observed: active}, changed: true, values: map[contextComponent]string{contextKubernetes: "friendly/apps"}},
		{name: "path fallback", input: pendingContextInput{path: "user → project → friendly → ignored", namespace: "apps", docker: "remote"}, changed: true, values: map[contextComponent]string{contextIdentity: "user", contextProject: "project", contextKubernetes: "friendly/apps", contextDocker: "remote"}},
		{name: "explicit selection precedes path", input: pendingContextInput{selection: [3]string{"explicit"}, path: "ignored → ignored → ignored"}, changed: true, values: map[contextComponent]string{contextIdentity: "explicit"}},
		{name: "docker only", input: pendingContextInput{docker: "remote", namespace: "apps"}, changed: true, values: map[contextComponent]string{contextDocker: "remote"}},
		{name: "disabled active components", input: pendingContextInput{selection: [3]string{"user"}, observed: state.Component{Identity: "user", Kubernetes: "contexthop-none", Docker: "contexthop-none"}}},
		{name: "disabled pending component", input: pendingContextInput{selection: [3]string{"", "", "contexthop-none"}, namespace: "apps", observed: active}, changed: true, values: map[contextComponent]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.input
			got := projectPendingContext(tc.input)
			if got.changed != tc.changed || got.display.label != "Pending" {
				t.Fatalf("got %+v", got)
			}
			if len(got.display.fields) != len(tc.values) {
				t.Fatalf("got fields %+v, want %v", got.display.fields, tc.values)
			}
			for key, value := range tc.values {
				if got.display.field(key).value != value {
					t.Fatalf("%s: got %+v, want %q", key, got.display.field(key), value)
				}
			}
			if !reflect.DeepEqual(tc.input, before) {
				t.Fatal("projection changed its input")
			}
		})
	}
}

func TestSelectedComponentAliasesRetainLabelsAndStates(t *testing.T) {
	for _, labels := range [][2]string{{"id", "k8s"}, {"identity", "kubernetes"}} {
		m := listModel{nextShellPreview: &LaunchPreview{Fields: []PickerField{{Label: labels[0], Value: "user"}, {Label: labels[1], Value: "cluster"}, {Label: "ADC", Value: "identity"}}, FieldStates: map[string]string{labels[0]: "staged", labels[1]: "highlighted"}}}
		d := m.selectedContextDisplay()
		id, k8s := d.field(contextIdentity), d.field(contextKubernetes)
		if id.component != contextIdentity || id.label != labels[0] || id.state != contextStaged || !id.adc {
			t.Fatal(id)
		}
		if k8s.component != contextKubernetes || k8s.label != labels[1] || k8s.state != contextHighlighted || k8s.adc {
			t.Fatal(k8s)
		}
	}
}
