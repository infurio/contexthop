package auth

import (
	"context"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"testing"
	"time"
)

func TestAuthenticationStatusTracksEvidenceAndExpires(t *testing.T) {
	for _, test := range []struct{ script, want string }{
		{"printf token", "Authenticated"},
		{"echo 'Reauthentication failed' >&2;exit 1", "Sign-in required"},
		{"echo 'connection timeout' >&2;exit 1", "Check failed"},
	} {
		bin, _ := fakeGcloud(t, test.script)
		t.Setenv("PATH", bin)
		identity := config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: t.TempDir()}
		if Status(identity) != "Not checked" {
			t.Fatal("unexpected initial status")
		}
		_ = Check(context.Background(), resolver.Resolved{IdentityName: "work", Identity: &identity})
		if Status(identity) != test.want {
			t.Fatalf("status = %s want %s", Status(identity), test.want)
		}
		old := time.Now().Add(-time.Minute)
		RecordStatus(identity, "Authenticated")
		recordStatusSince(identity, "Sign-in required", old)
		if Status(identity) != "Authenticated" {
			t.Fatal("stale check overwrote login")
		}
		observations.Lock()
		observations.values[identityStatusKey(identity)] = authObservation{label: "Authenticated", at: time.Now().Add(-3 * time.Minute)}
		observations.Unlock()
		if Status(identity) != "Check expired" {
			t.Fatal("old status presented as current")
		}
	}
}
