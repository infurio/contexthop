package ui

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/state"
	"strings"
	"testing"
)

func TestActivePendingHeaderUsesObservedComponentsAndExactlyTwoLines(t *testing.T) {
	for _, status := range []string{"MANAGED", "LOCAL MATCH", "CONTEXT-DRIFT", "UNMANAGED"} {
		for _, width := range []int{40, 60, 80, 118, 200} {
			m := listModel{resourceBrowser: true, width: width, snapshot: state.Snapshot{Managed: status != "UNMANAGED", LocalStatus: status, Destination: "workspace-alias", Observed: state.Component{Identity: "developer@example.com", Project: "payments-dev", Kubernetes: "dev-context", Namespace: "default", Docker: "desktop"}}, selection: [3]string{"other@example.com", "payments-prod", "prod-cluster"}, selectedDocker: "remote"}
			lines := strings.Split(ansi.Strip(m.resourceSelection()), "\n")
			activeLabel := "Active   "
			if m.snapshot.Managed {
				activeLabel = "Active · Pinned"
			}
			if len(lines) != 2 || !strings.HasPrefix(lines[0], activeLabel) || !strings.HasPrefix(lines[1], "Pending  ") {
				t.Fatal(lines)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.contentWidth() {
					t.Fatal("header overflow", width, line)
				}
			}
			if !strings.Contains(lines[0], status) || strings.Contains(lines[0], "workspace-alias") {
				t.Fatal("active status or effective state wrong", lines)
			}
			if width == 200 {
				for _, want := range []string{"id: developer@example.com", "project: payments-dev", "k8s: dev-context/default", "docker: desktop"} {
					if !strings.Contains(lines[0], want) {
						t.Fatal("missing active component", want, lines)
					}
				}
			}
		}
	}
}

func TestPendingNoChangesAndNamespaceDifference(t *testing.T) {
	m := listModel{width: 180, snapshot: state.Snapshot{Observed: state.Component{Identity: "developer@example.com", Project: "dev", Kubernetes: "gke-dev-context", Namespace: "default", Docker: "desktop"}}, selection: [3]string{"developer@example.com", "dev", "friendly-cluster"}, selectedKubernetesContext: "gke-dev-context", selectedNamespace: "default", selectedDocker: "desktop"}
	if !strings.Contains(m.resourceSelection(), "No changes") {
		t.Fatal("display alias caused false pending change")
	}
	m.selectedNamespace = "payments"
	if strings.Contains(m.resourceSelection(), "No changes") || !strings.Contains(ansi.Strip(m.resourceSelection()), "friendly-cluster/payments") {
		t.Fatal("namespace change hidden")
	}
	m.selection = [3]string{}
	m.selectedDocker = ""
	m.selectedNamespace = ""
	m.selectedKubernetesContext = ""
	if !strings.Contains(m.resourceSelection(), "No changes") {
		t.Fatal("empty draft is not a pending switch")
	}
	m.snapshot.Observed = state.Component{Kubernetes: "contexthop-none", Docker: "contexthop-none"}
	if strings.Contains(m.resourceSelection(), "contexthop-none") {
		t.Fatal("disabled sentinel exposed")
	}
}

func TestNextShellHeaderSeparatesHighlightedAndActiveContexts(t *testing.T) {
	m := NewAppModel(AppOptions{ResourceBrowser: true, ComposeSelection: true, StartScreen: ScreenKubernetes, Snapshot: state.Snapshot{Observed: state.Component{Kubernetes: "active-context", Namespace: "payments"}}, Pickers: map[Screen]Picker{ScreenKubernetes: {Screen: ScreenKubernetes, ResourceBrowser: true, Options: []Option{{Name: "alias", Kubernetes: true, KubernetesCluster: "next-cluster", KubernetesContext: "next-context", KubernetesNamespace: "default"}}}}})
	m.width = 180
	before := cloneDraft(m.draft)
	view := ansi.Strip(m.pickerModel(m.current()).resourceSelection())
	if !strings.Contains(view, "Active") || !strings.Contains(view, "active-context/payments") || !strings.Contains(view, "Selected") || !strings.Contains(view, "next-cluster") {
		t.Fatal(view)
	}
	if m.draft[ScreenKubernetes] != before[ScreenKubernetes] {
		t.Fatal("preview staged a resource")
	}
	if m.resourceContexts["alias"] != "next-context" {
		t.Fatal("canonical context lost")
	}
}

func TestActiveSelectedHeaderValuesAlign(t *testing.T) {
	for _, width := range []int{40, 60, 80, 118, 200} {
		m := listModel{width: width, snapshot: state.Snapshot{Observed: state.Component{Identity: "active@example.com"}},
			nextShellPreview: &LaunchPreview{Available: true, Fields: []PickerField{{Label: "id", Value: "selected@example.com"}, {Label: "ADC", Value: "off"}}}}
		lines := strings.Split(ansi.Strip(m.resourceSelection()), "\n")
		if len(lines) != 5 || !strings.HasPrefix(lines[1], "Identity") || ansi.StringWidth(strings.Split(lines[0], "Selected")[0]) != ansi.StringWidth(strings.Split(lines[1], "›")[0]) {
			t.Fatal("header values do not align", width, lines)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > m.contentWidth() {
				t.Fatal("header overflows", width, line)
			}
		}
	}
}

func TestSelectedHeaderShowsOrdinaryValuesAt80Columns(t *testing.T) {
	m := listModel{width: 80, height: 24, nextShellPreview: &LaunchPreview{Available: true,
		Fields:      []PickerField{{Label: "id", Value: "person@example.com"}, {Label: "project", Value: "example"}, {Label: "k8s", Value: "cluster"}, {Label: "docker", Value: "desktop-linux"}, {Label: "ADC", Value: "off"}},
		FieldStates: map[string]string{"id": "staged", "project": "highlighted", "k8s": "highlighted", "docker": "highlighted", "ADC": "empty"}}}
	view := ansi.Strip(m.resourceSelection())
	for _, want := range []string{"Identity", "✓ person@example.com", "Project", "› example", "Kubernetes", "› cluster", "Docker", "› desktop-linux"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q: %s", want, view)
		}
	}
	lines := strings.Split(view, "\n")
	if len(lines) != 5 || strings.Contains(view, "ADC") {
		t.Fatal("header must use five rows and omit disabled ADC", view)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > m.contentWidth() {
			t.Fatal("header overflows", line)
		}
	}
	m.height = 12
	if len(strings.Split(m.resourceSelection(), "\n")) != 1 {
		t.Fatal("short terminal lost row needed for filtering")
	}
}

func TestActiveContextScopeLabels(t *testing.T) {
	for _, tc := range []struct {
		scope    string
		subshell bool
		want     string
	}{
		{"shared", false, "Active · Shared"},
		{"local", false, "Active · Pinned"},
		{"local", true, "Active · Subshell"},
		{"shared", true, "Active · Shared"},
	} {
		m := listModel{snapshot: state.Snapshot{Managed: true, Scope: tc.scope, Subshell: tc.subshell}}
		if got := m.activeContextDisplay().label; got != tc.want {
			t.Fatalf("scope label = %q; want %q", got, tc.want)
		}
	}
}
