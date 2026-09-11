package discovery

import (
	"context"
	"strings"
	"testing"
)

func TestManagedShellDiscoveryStopsBeforeProviders(t *testing.T) {
	keys := []string{"CONTEXTHOP_CONTEXT", "CONTEXTHOP_SESSION_ID", "CONTEXTHOP_SESSION_DIR", "CONTEXTHOP_SESSION_FILE"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	// Shell integration alone is not an active managed session.
	t.Setenv("CONTEXTHOP_BINARY", "/opt/homebrew/bin/chop")
	t.Setenv("CONTEXTHOP_IN_PLACE_SWITCH", "1")
	if err := RequireUnmanagedShell(); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "stale-or-active")
			t.Setenv("PATH", "")
			observations, err := ObserveLocal(context.Background())
			if err == nil || !strings.Contains(err.Error(), "unmanaged shell") {
				t.Fatalf("error = %v", err)
			}
			if len(observations.Warnings) != 0 {
				t.Fatal("providers ran before guard")
			}
		})
	}
}
