package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func TestInteractiveADCRecovery(t *testing.T) {
	for _, action := range []string{"apply-shell", "launch-shell"} {
		for _, credentials := range []string{"missing", "expired", "wrong", "matching", "off"} {
			t.Run(action+"/"+credentials, func(t *testing.T) {
				cfg := gatedDiscoveryFixture(t)
				dir := cfg.Identities["work"].CloudSDKConfig
				identity := cfg.Identities["work"]
				identity.ADC = filepath.Join(dir, "custom", "adc.json")
				cfg.Identities["work"] = identity
				cfg.Kubernetes["cluster"] = config.Kubernetes{Type: "gke", Project: "one", Cluster: "dev", Location: "us-east1", Namespace: "payments"}
				script := `#!/bin/sh
case "$1 $2 $3" in
 "auth application-default login")
   [ "$4" = "work@example.com" ] || exit 90
   [ -z "$GOOGLE_APPLICATION_CREDENTIALS" ] || exit 91
   printf matching > "$CLOUDSDK_CONFIG/application_default_credentials.json";;
 "auth application-default print-access-token")
   value=$(/bin/cat "$GOOGLE_APPLICATION_CREDENTIALS") || exit 92
   [ "$value" != expired ] || exit 93
   printf '%s' "$value";;
 *)
   case "$1 $2" in
    "auth print-access-token") [ -f "$CLOUDSDK_CONFIG/logged-in" ];;
    "auth login") : > "$CLOUDSDK_CONFIG/logged-in";;
    *) exit 94;;
   esac;;
esac
`
				if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				if credentials != "missing" {
					if err := os.MkdirAll(filepath.Dir(identity.ADC), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(identity.ADC, []byte(credentials), 0600); err != nil {
						t.Fatal(err)
					}
				}
				previous := http.DefaultTransport
				checks := 0
				http.DefaultTransport = adcTestTransport(func(r *http.Request) (*http.Response, error) {
					checks++
					if _, ok := r.Context().Deadline(); !ok {
						t.Fatal("unbounded verification")
					}
					email := "work@example.com"
					if r.Header.Get("Authorization") == "Bearer wrong" {
						email = "other@example.com"
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"email":"` + email + `"}`)), Header: make(http.Header)}, nil
				})
				t.Cleanup(func() { http.DefaultTransport = previous })
				t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "parent-adc")
				t.Setenv("CONTEXTHOP_CONTEXT", "parent-context")
				draft := ui.Draft{ui.ScreenIdentity: "work", ui.ScreenProject: "one", ui.ScreenKubernetes: "cluster", ui.ScreenShellADCOverride: "identity"}
				if credentials == "off" {
					draft[ui.ScreenShellADCOverride] = "off"
				}
				before := cloneApplicationDraft(draft)
				choice := ui.Choice{Action: action}
				if err := os.WriteFile(filepath.Join(dir, "logged-in"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				transition := launchFlow(cfg, cfg, choice, draft)
				if credentials != "off" && credentials != "matching" {
					if transition.Complete || transition.Picker.Screen != "adc-authentication-required" {
						t.Fatalf("missing ADC recovery: %#v", transition)
					}
					if !transition.Picker.Flow(ui.Choice{Option: ui.Option{Name: "cancel"}}, draft).Dismiss {
						t.Fatal("cancel did not dismiss")
					}
					adc := transition.Picker.Flow(ui.Choice{Option: ui.Option{Name: "login"}}, draft)
					if adc.Process == nil {
						t.Fatal("missing ADC process")
					}
					failed := adc.Process.Continue(context.Background(), nil, errors.New("cancelled"))
					if failed.Complete || failed.Picker.Screen != "adc-authentication-required" {
						t.Fatal("failed login completed launch")
					}
					// Reporting successful login without valid credentials cannot launch.
					unverified := adc.Process.Continue(context.Background(), nil, nil)
					if unverified.Complete {
						t.Fatal("unverified ADC completed launch")
					}
					if err := adc.Process.Command.Run(); err != nil {
						t.Fatal(err)
					}
					if credentials == "wrong" {
						generated := filepath.Join(dir, "application_default_credentials.json")
						if err := os.WriteFile(generated, []byte("wrong"), 0600); err != nil {
							t.Fatal(err)
						}
						rejected := adc.Process.Continue(context.Background(), nil, nil)
						if rejected.Complete || rejected.Picker.Screen != "adc-authentication-required" {
							t.Fatal("wrong account accepted after login")
						}
						retry := rejected.Picker.Flow(ui.Choice{Option: ui.Option{Name: "login"}}, draft)
						if err := retry.Process.Command.Run(); err != nil {
							t.Fatal(err)
						}
						adc = retry
					}
					transition = adc.Process.Continue(context.Background(), nil, nil)
					data, err := os.ReadFile(identity.ADC)
					if err != nil || string(data) != "matching" {
						t.Fatalf("custom ADC path not updated: %s %v", data, err)
					}
				}
				if !transition.Complete || transition.CompletionChoice == nil || transition.CompletionChoice.Action != action {
					t.Fatalf("lost launch action: %#v", transition)
				}
				if !reflect.DeepEqual(draft, before) {
					t.Fatal("selection changed during authentication")
				}
				if credentials == "off" && checks != 0 {
					t.Fatal("ADC off contacted identity endpoint")
				}
				if credentials != "off" && checks == 0 {
					t.Fatal("ADC principal not verified")
				}
				if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "parent-adc" || os.Getenv("CONTEXTHOP_CONTEXT") != "parent-context" {
					t.Fatal("parent context changed")
				}
			})
		}
	}
}
