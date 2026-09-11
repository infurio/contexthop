package session

import (
	"github.com/infurio/contexthop/internal/state"
	"strings"
	"testing"
)

func TestPromptItemColors(t *testing.T) {
	m := state.Manifest{WorkspaceName: "Work", KubernetesLabel: "Cluster", Production: true,
		PromptColors: map[string]string{"workspace": "#123456", "kubernetes": "#abcdef"},
		Expected:     state.Component{Kubernetes: "cluster", Namespace: "default"}}
	got := formatPromptPrefix(m, "default", true)
	for _, want := range []string{"%F{#123456}PROD%f", "%F{#123456}Work%f", "%F{#abcdef}Cluster%f", "%F{75}default%f"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s: %s", want, got)
		}
	}
	if strings.Contains(got, "%F{196}") {
		t.Fatal("production overrides tag colors")
	}
	if got := formatPromptPrefix(m, "default", false); got != "[PROD|Work|Cluster|default] " {
		t.Fatal(got)
	}
	m.PromptColors["kubernetes"] = "red}$(touch /tmp/nope)"
	if got := formatPromptPrefix(m, "", true); !strings.Contains(got, "%F{39}Cluster%f") || strings.Contains(got, "touch") {
		t.Fatal(got)
	}
	for _, test := range []struct {
		kind      string
		component state.Component
	}{
		{"docker", state.Component{Docker: "engine"}}, {"project", state.Component{Project: "project"}}, {"identity", state.Component{Identity: "account"}},
	} {
		got := formatPromptPrefix(state.Manifest{Expected: test.component, PromptColors: map[string]string{test.kind: "#aabbcc"}}, "", true)
		if !strings.Contains(got, "%F{#aabbcc}") {
			t.Fatal(got)
		}
	}
}
