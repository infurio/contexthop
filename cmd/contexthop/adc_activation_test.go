package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
)

type adcTestTransport func(*http.Request) (*http.Response, error)

func (f adcTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestADCActivationGate(t *testing.T) {
	for _, mode := range []string{"exec", "subshell", "in-place", "equivalent", "reuse", "shared"} {
		for _, credential := range []string{"matching", "wrong", "missing", "expired", "unavailable", "disabled"} {
			t.Run(mode+"/"+credential, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("HOME", t.TempDir())
				t.Setenv("CONTEXTHOP_CACHE_DIR", dir)
				t.Setenv("PATH", dir)
				t.Setenv("CONTEXTHOP_ACTIVATION_FILE", "")
				adc := filepath.Join(dir, "selected-adc.json")
				t.Setenv("TEST_SELECTED_ADC", adc)
				if credential != "missing" {
					if err := os.WriteFile(adc, []byte(credential), 0600); err != nil {
						t.Fatal(err)
					}
				}
				// Refuse any ambient ADC path. CLI login succeeds independently of ADC.
				gcloud := `#!/bin/sh
if [ "$1 $2" = "auth print-access-token" ]; then exit 0; fi
[ "$1 $2 $3" = "auth application-default print-access-token" ] || exit 91
[ "$GOOGLE_APPLICATION_CREDENTIALS" = "$TEST_SELECTED_ADC" ] || exit 92
value=$(/bin/cat "$GOOGLE_APPLICATION_CREDENTIALS") || exit 93
[ "$value" != expired ] || exit 94
printf '%s' "$value"
`
				if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte(gcloud), 0700); err != nil {
					t.Fatal(err)
				}
				marker := filepath.Join(dir, "launched")
				t.Setenv("TEST_LAUNCH_MARKER", marker)
				launcher := filepath.Join(dir, "shell")
				if err := os.WriteFile(launcher, []byte("#!/bin/sh\nprintf launched > \"$TEST_LAUNCH_MARKER\"\n"), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("SHELL", launcher)
				calls := 0
				previousTransport := http.DefaultTransport
				http.DefaultTransport = adcTestTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.URL.String() != "https://openidconnect.googleapis.com/v1/userinfo" {
						t.Fatalf("unexpected request: %s", r.URL)
					}
					if _, ok := r.Context().Deadline(); !ok {
						t.Fatal("identity request has no deadline")
					}
					token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
					if token == "unavailable" {
						return nil, fmt.Errorf("identity service unavailable")
					}
					email := "selected@example.com"
					if token == "wrong" {
						email = "other@example.com"
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"email":%q}`, email))), Header: make(http.Header)}, nil
				})
				t.Cleanup(func() { http.DefaultTransport = previousTransport })
				resolved := resolver.Resolved{Name: "workspace", IdentityName: "selected", ADCMode: "identity", Identity: &config.Identity{Provider: "gcp", Account: "selected@example.com", CloudSDKConfig: filepath.Join(dir, "gcloud-state"), ADC: adc}}
				if credential == "disabled" {
					resolved.ADCMode = ""
				}
				// Establish an existing session; failures must preserve it and its environment.
				previous, err := session.Prepare(context.Background(), resolved)
				if err != nil {
					t.Fatal(err)
				}
				defer previous.Close()
				if err = previous.Commit(); err != nil {
					t.Fatal(err)
				}
				for _, item := range previous.Env {
					key, value, _ := strings.Cut(item, "=")
					if strings.HasPrefix(key, "CONTEXTHOP_") || strings.HasPrefix(key, "CLOUDSDK_") || strings.HasPrefix(key, "GOOGLE_") || strings.HasPrefix(key, "DOCKER_") || strings.HasPrefix(key, "AWS_") || key == "KUBECONFIG" {
						t.Setenv(key, value)
					}
				}
				activation := filepath.Join(dir, "activate.sh")
				if err = os.WriteFile(activation, []byte("previous activation"), 0600); err != nil {
					t.Fatal(err)
				}
				var sharedRevision string
				if mode == "shared" {
					if err := session.PublishShared(previous); err != nil {
						t.Fatal(err)
					}
					copy, revision, err := session.FollowShared("")
					if err != nil {
						t.Fatal(err)
					}
					copy.Close()
					sharedRevision = revision
				}
				switch mode {
				case "shared":
					t.Setenv(session.ActivationFileEnv, activation)
					err = runResolvedMode(resolved, nil, false, true)
				case "exec":
					err = runResolvedMode(resolved, []string{launcher}, false)
				case "subshell":
					err = runResolvedMode(resolved, nil, true)
				case "in-place":
					t.Setenv(session.ActivationFileEnv, activation)
					next := resolved
					next.Name = "new-workspace" // Force a switch instead of the equivalent fast path.
					next.Docker = &config.Docker{Context: "new-docker"}
					err = runResolvedMode(next, nil, false)
				case "equivalent":
					same, e := currentContextEquivalent(resolved)
					if e != nil || !same {
						t.Fatalf("fixture is not equivalent: %v %v", same, e)
					}
					err = runResolvedMode(resolved, nil, false)
				case "reuse":
					err = reuseActive(session.Active{ShellPID: os.Getpid(), Manifest: previous.Manifest, Destination: resolved.Name})
				}
				success := credential == "matching" || credential == "disabled"
				if success && err != nil {
					t.Fatal(err)
				}
				if !success && (err == nil || !strings.Contains(err.Error(), "ADC verification failed") || !strings.Contains(err.Error(), "chop auth --adc")) {
					t.Fatalf("missing actionable failure: %v", err)
				}
				if mode == "shared" {
					copy, revision, followErr := session.FollowShared(sharedRevision)
					if followErr != nil {
						t.Fatal(followErr)
					}
					if copy != nil {
						copy.Close()
					}
					if success == (revision == sharedRevision) {
						t.Fatal("publication did not respect authentication result")
					}
				}
				if credential == "wrong" && !strings.Contains(err.Error(), "ADC identity mismatch") {
					t.Fatal(err)
				}
				if credential == "disabled" && calls != 0 {
					t.Fatal("disabled ADC contacted identity endpoint")
				}
				if credential == "matching" && calls != 1 {
					t.Fatalf("identity checked %d times", calls)
				}
				if _, err := os.Stat(previous.Manifest); err != nil {
					t.Fatal("previous session removed", err)
				}
				if os.Getenv("CONTEXTHOP_CONTEXT") != resolved.Name {
					t.Fatal("parent environment changed")
				}
				if !success {
					if _, err := os.Stat(marker); !os.IsNotExist(err) {
						t.Fatal("launched before verification")
					}
					data, _ := os.ReadFile(activation)
					if string(data) != "previous activation" {
						t.Fatal("activation file changed on failure")
					}
					entries, err := os.ReadDir(filepath.Join(dir, "contexthop", "sessions"))
					if err != nil || len(entries) != 1 {
						t.Fatalf("staging leaked: %v %v", entries, err)
					}
				} else if mode != "equivalent" && mode != "in-place" && mode != "shared" {
					if _, err := os.Stat(marker); err != nil {
						t.Fatal("verified launch did not execute", err)
					}
				} else if mode == "in-place" {
					data, _ := os.ReadFile(activation)
					if !strings.Contains(string(data), "new-workspace") {
						t.Fatal("verified activation was not written")
					}
				}
			})
		}
	}
}
