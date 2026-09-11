package state

import "testing"

func TestRelevantErrorsSuppressesUnmanagedProbeFailures(t *testing.T) {
	result := ProbeResult{Errors: []string{"gcloud probe failed", "kubectl probe failed", "docker probe failed"}}
	if relevant := result.RelevantErrors(Snapshot{}); len(relevant) != 0 {
		t.Fatalf("unmanaged RelevantErrors() = %v", relevant)
	}
}

func TestRelevantErrorsSuppressesDisabledComponents(t *testing.T) {
	result := ProbeResult{Errors: []string{"gcloud probe failed", "kubectl probe failed", "docker probe failed"}}
	snapshot := Snapshot{
		Managed: true,
		Expected: Component{
			Docker: "contexthop-none",
		},
	}
	if relevant := result.RelevantErrors(snapshot); len(relevant) != 0 {
		t.Fatalf("RelevantErrors() = %v", relevant)
	}
}
