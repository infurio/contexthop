package ui

import (
	"strings"

	"github.com/infurio/contexthop/internal/state"
)

// contextDisplay is a rendering projection, never a source of selection policy.
// ADC and selection state stay separate from values so truncation cannot hide
// badges or change comparisons.
type contextDisplay struct {
	label, status string
	fields        []contextDisplayField
}

type contextComponent string

const (
	contextIdentity   contextComponent = "identity"
	contextProject    contextComponent = "project"
	contextKubernetes contextComponent = "kubernetes"
	contextDocker     contextComponent = "docker"
)

var contextComponents = [...]contextComponent{contextIdentity, contextProject, contextKubernetes, contextDocker}

func componentKey(label string) contextComponent {
	switch label {
	case "id":
		return contextIdentity
	case "k8s":
		return contextKubernetes
	default:
		return contextComponent(label)
	}
}

func (key contextComponent) compactLabel() string {
	switch key {
	case contextIdentity:
		return "id"
	case contextKubernetes:
		return "k8s"
	default:
		return string(key)
	}
}

type contextFieldState string

const (
	contextStaged      contextFieldState = "staged"
	contextHighlighted contextFieldState = "highlighted"
	contextEmpty       contextFieldState = "empty"
)

type contextDisplayField struct {
	component    contextComponent
	label, value string
	state        contextFieldState
	adc          bool
}

func componentDisplay(c state.Component, adc bool) []contextDisplayField {
	values := [4]string{c.Identity, c.Project, c.Kubernetes, c.Docker}
	if display(c.Kubernetes) != "none" && c.Namespace != "" {
		values[2] += "/" + c.Namespace
	}
	fields := make([]contextDisplayField, 4)
	for i, value := range values {
		fields[i] = contextDisplayField{component: contextComponents[i], label: contextComponents[i].compactLabel(), value: singleLine(value), adc: i == 0 && display(value) != "none" && adc}
	}
	return fields
}

func (m listModel) activeContextDisplay() contextDisplay {
	d := contextDisplay{label: "Active", status: "UNMANAGED", fields: componentDisplay(m.snapshot.Observed, display(m.snapshot.Observed.ADC) != "none")}
	if m.snapshot.Managed {
		d.status = firstNonEmptyUI(m.snapshot.LocalStatus, "MANAGED")
	}
	switch m.snapshot.Scope {
	case "shared":
		d.label += " · Shared"
	case "local":
		d.label += " · This shell"
	}
	return d
}

func (m listModel) sharedContextDisplay() contextDisplay {
	d := contextDisplay{label: "Shared config", fields: componentDisplay(state.Component{}, false)}
	switch {
	case m.snapshot.SharedConfigError != "":
		d.fields[0].value = "Unavailable"
	case m.snapshot.SharedConfig == nil:
		d.fields[0].value = "Not set"
	default:
		manifest := m.snapshot.SharedConfig
		d.fields = componentDisplay(manifest.Expected, manifest.ADCMode == "identity")
	}
	return d
}

func (m listModel) selectedContextDisplay() contextDisplay {
	d := contextDisplay{label: "Selected"}
	if p := m.nextShellPreview; p != nil {
		for _, f := range p.Fields {
			if f.Label == "ADC" {
				continue
			}
			key := componentKey(f.Label)
			d.fields = append(d.fields, contextDisplayField{component: key, label: f.Label, value: singleLine(f.Value), state: contextFieldState(p.FieldStates[f.Label]), adc: key == contextIdentity && display(f.Value) != "none" && p.adcEnabled()})
		}
	}
	return d
}

func (d contextDisplay) field(key contextComponent) contextDisplayField {
	var result contextDisplayField
	for _, field := range d.fields {
		if field.component == key {
			result = field
		}
	}
	return result
}

// pendingContextInput contains only the legacy picker's resource state.
// Canonical Kubernetes names are used for comparison; display aliases remain
// in the result. This projection does not resolve, stage or activate anything.
type pendingContextInput struct {
	selection                                  [3]string
	docker, path, kubernetesContext, namespace string
	pristine                                   bool
	observed                                   state.Component
}

type pendingContextProjection struct {
	display contextDisplay
	changed bool
}

func projectPendingContext(input pendingContextInput) pendingContextProjection {
	values := [4]string{input.selection[0], input.selection[1], input.selection[2], input.docker}
	if input.selection == [3]string{} && input.path != "" {
		parts := strings.Split(input.path, " → ")
		for i := 0; i < min(3, len(parts)); i++ {
			values[i] = parts[i]
		}
	}
	compare := values
	compare[2] = firstNonEmptyUI(input.kubernetesContext, values[2])
	o := input.observed
	active := [4]string{o.Identity, o.Project, o.Kubernetes, o.Docker}
	for i := range active {
		if active[i] == "contexthop-none" {
			active[i] = ""
		}
	}
	same, pristine := compare == active, input.pristine
	for i := range compare {
		if compare[i] != "" && compare[i] != active[i] {
			pristine = false
		}
	}
	if compare[2] != "" && input.namespace != "" && input.namespace != o.Namespace {
		same, pristine = false, false
	}
	result := pendingContextProjection{display: contextDisplay{label: "Pending"}, changed: values != [4]string{} && !same && !pristine}
	if result.changed {
		for i, key := range contextComponents {
			value := values[i]
			if value == "" || value == "contexthop-none" {
				continue
			}
			if key == contextKubernetes && input.namespace != "" {
				value += "/" + input.namespace
			}
			result.display.fields = append(result.display.fields, contextDisplayField{component: key, label: key.compactLabel(), value: singleLine(value)})
		}
	}
	return result
}

func (m listModel) pendingContextDisplay() pendingContextProjection {
	return projectPendingContext(pendingContextInput{selection: m.selection, docker: m.selectedDocker, path: m.selectionPath, kubernetesContext: m.selectedKubernetesContext, namespace: m.selectedNamespace, pristine: m.pendingPristine, observed: m.snapshot.Observed})
}
