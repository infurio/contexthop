package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/infurio/contexthop/internal/testenv"
)

// Tests begin unmanaged even when go test was launched inside a chop session.
// Individual tests explicitly construct managed state and provider responses.
func TestMain(m *testing.M) { os.Exit(runIsolatedTests(m)) }

func runIsolatedTests(m *testing.M) int {
	// These subprocess entry points must retain the session constructed by their
	// parent test. Restrict the exception to the exact helper invocation.
	for _, arg := range os.Args[1:] {
		if (arg == "-test.run=^TestReviewCommandHelper$" && os.Getenv("CONTEXTHOP_TEST_COMMAND_HELPER") != "") || (arg == "-test.run=TestSharedShellHelper" && os.Getenv("CHOP_SHARED_TEST_HELPER") == "1") {
			return m.Run()
		}
	}

	root, err := os.MkdirTemp("", "contexthop-tests.")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(root)
	e, err := testenv.Create(root, testenv.Options{Path: os.Getenv("PATH")})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// Preserve only build controls and the explicitly requested capture directory.
	for _, key := range []string{"GOCACHE", "GOTOOLCHAIN", "GOPATH", "GOROOT", "CONTEXTHOP_UI_AUDIT_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			e.Vars[key] = value
		}
	}
	restore := e.Install()
	defer restore()
	return m.Run()
}
