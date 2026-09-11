package main

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixtureRunnerHelper(t *testing.T) {
	if os.Getenv("FIXTURE_RUNNER_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			break
		}
	}
	flag.CommandLine = flag.NewFlagSet("fixturecmd", flag.ExitOnError)
	os.Exit(run())
}

func TestRunnerIsolatesAndCleansUpOnExit(t *testing.T) {
	for _, tc := range []struct {
		name, finish string
		code         int
	}{{"success", "exit 0", 0}, {"failure", "exit 42", 42}, {"signal", "kill -TERM $$", 143}} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestFixtureRunnerHelper$", "--", "--binary", "/usr/bin/true", "--scenario", "acme", "--", "/bin/sh", "-c", `
set -eu
printf '%s\n' "$HOME"
test -z "${CONTEXTHOP_CONTEXT+x}"
test -z "${CONTEXTHOP_SCOPE+x}"
test -z "${GOOGLE_APPLICATION_CREDENTIALS+x}"
test -z "${KUBECONFIG+x}"
test "$ZDOTDIR" = "$HOME"
test "$CONTEXTHOP_BINARY" = /usr/bin/true
test "$(docker context show)" = default
gcloud projects list --format=json > /dev/null
`+tc.finish)
			cmd.Env = append(os.Environ(), "FIXTURE_RUNNER_HELPER=1", "CONTEXTHOP_CONTEXT=host", "CONTEXTHOP_SCOPE=shared", "GOOGLE_APPLICATION_CREDENTIALS=/host/adc", "KUBECONFIG=/host/kube")
			out, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			if code != tc.code {
				t.Fatalf("exit %d, want %d: %s", code, tc.code, out)
			}
			home := strings.TrimSpace(string(out))
			if !strings.HasPrefix(filepath.Base(filepath.Dir(home)), "contexthop-fixture.") {
				t.Fatalf("unexpected home: %q", home)
			}
			if _, err := os.Stat(filepath.Dir(home)); !os.IsNotExist(err) {
				t.Fatal("fixture survived child exit", err)
			}
		})
	}
}
