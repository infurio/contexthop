package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func combinedADCFixture(t *testing.T) config.Config {
	t.Helper()
	cfg := gatedDiscoveryFixture(t)
	dir := cfg.Identities["work"].CloudSDKConfig
	identity := cfg.Identities["work"]
	identity.ADC = filepath.Join(dir, "custom", "adc.json")
	cfg.Identities["work"] = identity
	script := `#!/bin/sh
[ "$CLOUDSDK_CORE_ACCOUNT" = work@example.com ] || exit 89
printf '%s\n' "$*" >> "$CLOUDSDK_CONFIG/calls"
case "$1 $2" in
 "auth print-access-token") [ -f "$CLOUDSDK_CONFIG/logged-in" ];;
 "auth login")
  [ -z "$GOOGLE_APPLICATION_CREDENTIALS" ] || exit 90
  : > "$CLOUDSDK_CONFIG/logged-in"
  for arg in "$@"; do
   if [ "$arg" = --update-adc ]; then printf matching > "$CLOUDSDK_CONFIG/application_default_credentials.json"; fi
  done;;
 "auth application-default")
  case "$3" in
   login)
    [ "$4" = work@example.com ] || exit 91
    [ -z "$GOOGLE_APPLICATION_CREDENTIALS" ] || exit 92
    printf matching > "$CLOUDSDK_CONFIG/application_default_credentials.json";;
   print-access-token) /bin/cat "$GOOGLE_APPLICATION_CREDENTIALS";;
   *) exit 93;;
  esac;;
 *) exit 94;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	http.DefaultTransport = adcTestTransport(func(r *http.Request) (*http.Response, error) {
		email := "work@example.com"
		if r.Header.Get("Authorization") != "Bearer matching" {
			email = "other@example.com"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"email":"` + email + `"}`)), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	return cfg
}

func TestCombinedLoginForBothShellModes(t *testing.T) {
	for _, action := range []string{"apply-shell", "launch-shell"} {
		for _, mode := range []string{"browser", "terminal"} {
			for _, initial := range []string{"missing", "matching", "off"} {
				t.Run(action+"/"+mode+"/"+initial, func(t *testing.T) {
					cfg := combinedADCFixture(t)
					identity := cfg.Identities["work"]
					if initial == "matching" {
						if err := os.MkdirAll(filepath.Dir(identity.ADC), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(identity.ADC, []byte("matching"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "parent-adc")
					t.Setenv("CONTEXTHOP_CONTEXT", "parent")
					draft := ui.Draft{ui.ScreenIdentity: "work", ui.ScreenProject: "one", ui.ScreenShellADCOverride: "identity"}
					if initial == "off" {
						draft[ui.ScreenShellADCOverride] = "off"
					}
					before := cloneApplicationDraft(draft)
					gate := launchFlow(cfg, cfg, ui.Choice{Action: action}, draft)
					if gate.Picker.Screen != "authentication-required" {
						t.Fatal("missing login gate")
					}
					login := gate.Picker.Flow(ui.Choice{Option: ui.Option{Name: mode}}, draft)
					combined := strings.Contains(strings.Join(login.Process.Command.Args, " "), "--update-adc")
					if combined != (initial == "missing") {
						t.Fatalf("incorrect combined login: %v", login.Process.Command.Args)
					}
					if err := login.Process.Command.Run(); err != nil {
						t.Fatal(err)
					}
					result := login.Process.Continue(context.Background(), nil, nil)
					if !result.Complete || result.CompletionChoice.Action != action {
						t.Fatalf("launch did not resume: %#v", result)
					}
					if !reflect.DeepEqual(before, draft) {
						t.Fatal("selection changed")
					}
					calls, err := os.ReadFile(filepath.Join(identity.CloudSDKConfig, "calls"))
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(calls), "application-default login") {
						t.Fatalf("unnecessary second login: %s", calls)
					}
					if initial == "matching" {
						if _, err := os.Stat(filepath.Join(identity.CloudSDKConfig, "application_default_credentials.json")); !os.IsNotExist(err) {
							t.Fatal("valid ADC overwritten")
						}
					}
					if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "parent-adc" || os.Getenv("CONTEXTHOP_CONTEXT") != "parent" {
						t.Fatal("parent context changed")
					}
				})
			}
		}
	}
}

func TestStandaloneADCFromIdentityOptions(t *testing.T) {
	cfg := combinedADCFixture(t)
	actions := catalogRowActions(cfg, catalog.Ref{Kind: catalog.KindIdentity, Name: "work"})
	found := false
	for _, a := range actions {
		if a.Action == "identity-auth" && a.Key == "a" && a.Label == "Authentication" {
			found = true
		}
	}
	if !found {
		t.Fatal("Authentication action missing from Options")
	}
	draft := ui.Draft{ui.ScreenIdentity: "another-selection", ui.ScreenShellADCOverride: "off"}
	before := cloneApplicationDraft(draft)
	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	menu := flow(ui.Choice{Screen: ui.ScreenIdentity, Action: "identity-auth", Option: ui.Option{Name: "work"}}, draft)
	if !strings.HasPrefix(menu.Picker.Title, "Authentication ›") {
		t.Fatal("incorrect authentication title")
	}
	var adcOption ui.Option
	for _, option := range menu.Picker.Options {
		if strings.HasSuffix(option.Name, "\x00adc") {
			adcOption = option
		}
	}
	if adcOption.Name == "" {
		t.Fatal("ADC login missing from Authentication")
	}
	gate := flow(ui.Choice{Screen: screenProviderAuth, Option: adcOption}, draft)
	if gate.Picker.Screen != "adc-authentication-required" {
		t.Fatalf("ADC not accessible: %#v", gate)
	}
	login := gate.Picker.Flow(ui.Choice{Option: ui.Option{Name: "login"}}, draft)
	if err := login.Process.Command.Run(); err != nil {
		t.Fatal(err)
	}
	result := login.Process.Continue(context.Background(), nil, nil)
	if result.Complete || result.Picker.Screen != ui.ScreenIdentity || result.Picker.Focus != "work" || !strings.Contains(result.Picker.Description, "Application credentials verified") {
		t.Fatalf("did not return to identity: %#v", result)
	}
	if !reflect.DeepEqual(draft, before) || len(result.DraftUpdates) != 0 {
		t.Fatal("standalone ADC changed selection")
	}
	calls, err := os.ReadFile(filepath.Join(cfg.Identities["work"].CloudSDKConfig, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "auth login") || strings.Contains(string(calls), "auth print-access-token") {
		t.Fatalf("ADC required CLI login: %s", calls)
	}
}

func TestCombinedLoginVerifiesGeneratedADC(t *testing.T) {
	cfg := combinedADCFixture(t)
	draft := ui.Draft{ui.ScreenIdentity: "work", ui.ScreenShellADCOverride: "identity"}
	gate := launchFlow(cfg, cfg, ui.Choice{Action: "apply-shell"}, draft)
	if !gate.Picker.Flow(ui.Choice{Option: ui.Option{Name: "cancel"}}, draft).Dismiss {
		t.Fatal("cancel lost")
	}
	login := gate.Picker.Flow(ui.Choice{Option: ui.Option{Name: "terminal"}}, draft)
	if err := login.Process.Command.Run(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.Identities["work"].CloudSDKConfig, "application_default_credentials.json")
	if err := os.WriteFile(path, []byte("wrong"), 0600); err != nil {
		t.Fatal(err)
	}
	result := login.Process.Continue(context.Background(), nil, nil)
	if result.Complete || result.Picker.Screen != "adc-authentication-required" {
		t.Fatalf("unverified combined login accepted: %#v", result)
	}
}

func TestADCLogoutFromAuthentication(t *testing.T) {
	cfg := combinedADCFixture(t)
	identity := cfg.Identities["work"]
	if err := os.MkdirAll(filepath.Dir(identity.ADC), 0700); err != nil {
		t.Fatal(err)
	}
	payload := `{"type":"authorized_user","client_id":"client","refresh_token":"token"}`
	if err := os.WriteFile(identity.ADC, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n[ \"$*\" = 'auth application-default revoke --quiet' ] || exit 90\n/bin/rm \"$CLOUDSDK_CONFIG/application_default_credentials.json\"\n"
	if err := os.WriteFile(filepath.Join(identity.CloudSDKConfig, "gcloud"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	draft := ui.Draft{ui.ScreenIdentity: "other", ui.ScreenShellADCOverride: "off"}
	before := cloneApplicationDraft(draft)
	menu := providerAuthPicker(cfg, "identity", "work", "")
	var logout ui.Option
	for _, option := range menu.Options {
		if strings.HasSuffix(option.Name, "\x00adc-logout") {
			logout = option
		}
	}
	if logout.Name == "" {
		t.Fatal("ADC logout missing")
	}
	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	result := flow(ui.Choice{Screen: screenProviderAuth, Option: logout}, draft)
	if result.Complete || result.Picker.Screen != ui.ScreenIdentity || !strings.Contains(result.Picker.Description, "ADC logged out") {
		t.Fatalf("incorrect logout result: %#v", result)
	}
	if !reflect.DeepEqual(draft, before) || len(result.DraftUpdates) != 0 {
		t.Fatal("logout changed selection")
	}
	if _, err := os.Stat(identity.ADC); !os.IsNotExist(err) {
		t.Fatal("ADC not removed")
	}
	// A revocation error returns to Authentication and preserves the credentials.
	if err := os.WriteFile(identity.ADC, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identity.CloudSDKConfig, "gcloud"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	failed := flow(ui.Choice{Screen: screenProviderAuth, Option: logout}, draft)
	if failed.Complete || failed.Picker.Screen != screenProviderAuth {
		t.Fatal("failed revoke did not offer recovery")
	}
	if _, err := os.Stat(identity.ADC); err != nil {
		t.Fatal("failed revoke removed ADC")
	}
}
