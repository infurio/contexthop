package discovery

import (
	"errors"
	"os"
)

// RequireUnmanagedShell prevents isolated provider settings from being mistaken
// for the host's complete configuration. Treat stale session markers as managed
// too: a missing manifest does not restore the original provider environment.
func RequireUnmanagedShell() error {
	for _, key := range []string{"CONTEXTHOP_CONTEXT", "CONTEXTHOP_SESSION_ID", "CONTEXTHOP_SESSION_DIR", "CONTEXTHOP_SESSION_FILE"} {
		if os.Getenv(key) != "" {
			return errors.New("discovery is disabled in a ContextHop managed shell; run discovery from an unmanaged shell (a terminal without an active ContextHop session)")
		}
	}
	return nil
}
