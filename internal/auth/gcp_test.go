package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

func TestCheckSeparatesLoginFromProviderFailures(t *testing.T) {
	for _, tc := range []struct{ name, diagnostic, kind string }{
		{"expired", "invalid_grant: credentials have expired", "authentication_required"},
		{"network", "network is unreachable", "network_error"},
		{"storage", "permission denied", "provider_access_denied"},
		{"unknown", "provider failed", "provider_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, _ := fakeGcloud(t, "printf '%s\\n' '"+tc.diagnostic+"' >&2\nexit 1")
			t.Setenv("PATH", bin)
			identity := config.Identity{Provider: "gcp", Account: "alex@acme.example", CloudSDKConfig: t.TempDir()}
			err := Check(context.Background(), resolver.Resolved{IdentityName: "Acme Engineering", Identity: &identity})
			var check *CheckError
			if !errors.As(err, &check) || check.Kind != tc.kind {
				t.Fatalf("check = %v, want %s", err, tc.kind)
			}
		})
	}
}

func TestSavedBrowserBindingDoesNotReadChromeMetadata(t *testing.T) {
	t.Setenv("BROWSER", "another-browser")
	t.Setenv("HOME", t.TempDir())
	configDir := t.TempDir()
	identity := config.Identity{Provider: "gcp", Account: "alex@acme.example", CloudSDKConfig: configDir,
		Browser: config.BrowserProfile{App: "chrome", Profile: "Profile 7"}}
	for _, prepare := range []func(context.Context, string, config.Identity) (*exec.Cmd, string, error){LoginCommand, LoginADCCommand} {
		command, label, err := prepare(context.Background(), "Acme Engineering", identity)
		if err != nil {
			t.Fatal(err)
		}
		if label != "Google Chrome (Profile 7)" {
			t.Fatalf("browser label = %q", label)
		}
		var launcher string
		for _, entry := range command.Env {
			if strings.HasPrefix(entry, "BROWSER=") {
				launcher = strings.TrimPrefix(entry, "BROWSER=")
			}
		}
		if launcher == "" || launcher == "another-browser" {
			t.Fatalf("saved binding was not used: %q", launcher)
		}
		data, err := os.ReadFile(launcher)
		if err != nil || !strings.Contains(string(data), "--profile-directory='Profile 7'") {
			t.Fatalf("browser launcher = %q, %v", data, err)
		}
	}
}

func TestChromeAccountLauncherAvoidsProfileMetadata(t *testing.T) {
	chrome := filepath.Join(t.TempDir(), "Chrome")
	if err := os.WriteFile(chrome, []byte("#!/bin/sh\necho 'Google Chrome 154.0.0.0'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	env, label, err := configureChromeByAccount(nil, "alex@acme.example", configDir, chrome, errors.New("operation not permitted"))
	if err != nil || label != "Google Chrome for this account" || len(env) != 1 || !strings.HasPrefix(env[0], "BROWSER=") {
		t.Fatalf("Chrome fallback = %q, %q, %v", env, label, err)
	}
	launcher := strings.TrimPrefix(env[0], "BROWSER=")
	data, err := os.ReadFile(launcher)
	if err != nil || !strings.Contains(string(data), "--profile-email='alex@acme.example' --create-profile-email-if-not-exists") {
		t.Fatalf("account launcher = %q, %v", data, err)
	}
	for _, version := range []string{"Google Chrome 142.0.0.0", "unknown"} {
		if chromeSupportsEmailPicker(version) {
			t.Fatalf("unsupported Chrome version %q accepted", version)
		}
	}
	if !chromeSupportsEmailPicker("Google Chrome 143.0.0.0") {
		t.Fatal("Chrome 143 should support account picker")
	}
}

func TestSelectChromeProfilePrefersExactAccount(t *testing.T) {
	profiles := []chromeProfile{
		{Directory: "Default", Name: "Personal", Account: "other@example.com"},
		{Directory: "Profile 7", Name: "Work", Account: "person@example.com"},
	}
	selected, err := selectChromeProfile("person@example.com", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if selected == nil || selected.Directory != "Profile 7" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestSelectChromeProfileUsesUniqueDomain(t *testing.T) {
	profiles := []chromeProfile{{Directory: "Profile 7", Name: "Work", Account: "other@example.com"}}
	selected, err := selectChromeProfile("person@example.com", profiles)
	if err != nil {
		t.Fatal(err)
	}
	if selected == nil || selected.Directory != "Profile 7" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestSelectChromeProfileRejectsAmbiguousDomain(t *testing.T) {
	profiles := []chromeProfile{
		{Directory: "Profile 1", Name: "One", Account: "one@example.com"},
		{Directory: "Profile 2", Name: "Two", Account: "two@example.com"},
	}
	if _, err := selectChromeProfile("person@example.com", profiles); err == nil {
		t.Fatal("expected ambiguous domain error")
	}
}

func TestLoginCommandCanBeHandedToInteractiveTUI(t *testing.T) {
	t.Setenv("BROWSER", "true")
	configDir := filepath.Join(t.TempDir(), "gcloud")
	command, profile, err := LoginCommand(context.Background(), "work", config.Identity{
		Provider: "gcp", Account: "person@example.com", CloudSDKConfig: configDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile != "" || strings.Join(command.Args, " ") != "gcloud auth login person@example.com" {
		t.Fatalf("login command = %#v, profile %q", command.Args, profile)
	}
	if command.Stdin != nil || command.Stdout != nil || command.Stderr != nil {
		t.Fatal("login command claimed terminal streams before Bubble Tea handoff")
	}
	if info, err := os.Stat(configDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("isolated config directory = %#v, %v", info, err)
	}
}

func TestCheckCLIAuthDoesNotRequireADC(t *testing.T) {
	bin, log := fakeGcloud(t, `
printf '%s\n' "$*" >> "$FAKE_GCLOUD_LOG"
[ "$1 $2" = "auth print-access-token" ] || exit 42
`)
	t.Setenv("PATH", bin)
	resolved := resolver.Resolved{
		IdentityName: "work",
		Identity: &config.Identity{
			Provider: "gcp", Account: "person@example.com",
			CloudSDKConfig: filepath.Join(t.TempDir(), "gcloud"),
			ADC:            filepath.Join(t.TempDir(), "missing-adc.json"),
		},
	}
	if err := Check(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "application-default") {
		t.Fatalf("unexpected ADC call: %s", contents)
	}
}

func TestCheckADCOnlyWhenExplicitlyRequired(t *testing.T) {
	bin, log := fakeGcloud(t, `
printf '%s\n' "$*" >> "$FAKE_GCLOUD_LOG"
[ "$1 $2" = "auth application-default" ] || exit 42
printf 'test-token\n'
`)
	t.Setenv("PATH", bin)
	useADCIdentityResponse(t, "person@example.com")
	directory := t.TempDir()
	adc := filepath.Join(directory, "adc.json")
	if err := os.WriteFile(adc, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.Resolved{
		ADCMode: "identity", IdentityName: "work",
		Identity: &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: directory, ADC: adc},
	}
	if err := CheckADC(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "auth application-default print-access-token") {
		t.Fatalf("ADC call = %s", contents)
	}
}

func TestADCValidationRejectsWrongPrincipal(t *testing.T) {
	bin, _ := fakeGcloud(t, `printf 'test-token\n'`)
	t.Setenv("PATH", bin)
	useADCIdentityResponse(t, "someone-else@example.com")
	directory := t.TempDir()
	adc := filepath.Join(directory, "adc.json")
	if err := os.WriteFile(adc, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.Resolved{
		ADCMode: "identity", IdentityName: "work",
		Identity: &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: directory, ADC: adc},
	}
	err := CheckADC(context.Background(), resolved)
	if err == nil || !strings.Contains(err.Error(), "ADC identity mismatch") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoginDoesNotUpdateADC(t *testing.T) {
	bin, log := fakeGcloud(t, `
printf '%s\n' "$*" >> "$FAKE_GCLOUD_LOG"
case "$*" in *--update-adc*|*application-default*) exit 42 ;; esac
`)
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BROWSER", "true")
	configDir := t.TempDir()
	adc := filepath.Join(configDir, "application_default_credentials.json")
	if err := os.WriteFile(adc, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: configDir, ADC: adc}
	if err := Login(context.Background(), "work", identity); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "auth login person@example.com\n" {
		t.Fatalf("login call = %q", contents)
	}
	after, err := os.ReadFile(adc)
	if err != nil || string(after) != "unchanged" {
		t.Fatalf("ADC changed: %q, %v", after, err)
	}
}

func TestLoginADCUsesSeparateFlow(t *testing.T) {
	bin, log := fakeGcloud(t, `
printf '%s\n' "$*" >> "$FAKE_GCLOUD_LOG"
[ "$1 $2 $3" = "auth application-default login" ] || exit 42
printf 'generated' > "$CLOUDSDK_CONFIG/application_default_credentials.json"
`)
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BROWSER", "true")
	configDir := t.TempDir()
	desired := filepath.Join(t.TempDir(), "selected-adc.json")
	identity := config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: configDir, ADC: desired}
	if err := LoginADC(context.Background(), "work", identity); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "auth application-default login person@example.com --quiet --launch-browser\n" {
		t.Fatalf("ADC login call = %q", contents)
	}
	copied, err := os.ReadFile(desired)
	if err != nil || string(copied) != "generated" {
		t.Fatalf("selected ADC = %q, %v", copied, err)
	}
}

func TestAuthenticationNeutralizesInheritedGoogleOverrides(t *testing.T) {
	bin, _ := fakeGcloud(t, `
[ -z "${CLOUDSDK_ACTIVE_CONFIG_NAME+x}" ] || exit 31
[ -z "${CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE+x}" ] || exit 32
[ -z "${CLOUDSDK_AUTH_ACCESS_TOKEN+x}" ] || exit 97
[ -z "${CLOUDSDK_AUTH_ACCESS_TOKEN_FILE+x}" ] || exit 33
[ -z "${CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT+x}" ] || exit 34
[ "$CLOUDSDK_CORE_ACCOUNT" = "person@example.com" ] || exit 35
[ "$CLOUDSDK_CORE_PROJECT" = "right-project" ] || exit 36
`)
	t.Setenv("PATH", bin)
	t.Setenv("CLOUDSDK_ACTIVE_CONFIG_NAME", "hostile")
	t.Setenv("CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE", "/wrong/credential.json")
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "dummy-token")
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN_FILE", "/wrong/token")
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "wrong@example.com")
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "wrong@example.com")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "wrong-project")
	resolved := resolver.Resolved{
		IdentityName: "work",
		Identity:     &config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: t.TempDir()},
		Project:      &config.Project{Provider: "gcp", ProjectID: "right-project"},
	}
	if err := Check(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}
}

func fakeGcloud(t *testing.T, body string) (string, string) {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "gcloud.log")
	path := filepath.Join(bin, "gcloud")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GCLOUD_LOG", log)
	return bin, log
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func useADCIdentityResponse(t *testing.T, email string) {
	t.Helper()
	previousClient := adcIdentityHTTPClient
	adcIdentityHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization header was not set")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"email":"` + email + `"}`)),
			Request:    request,
		}, nil
	})}
	t.Cleanup(func() { adcIdentityHTTPClient = previousClient })
}

func TestLogoutUsesIsolatedAccountAndPreservesADC(t *testing.T) {
	bin, log := fakeGcloud(t, `
printf '%s|%s|%s\n' "$*" "$CLOUDSDK_CONFIG" "$CLOUDSDK_CORE_ACCOUNT" >> "$FAKE_GCLOUD_LOG"
[ "$*" = "auth revoke person@example.com --quiet" ] || exit 42
[ -z "${GOOGLE_APPLICATION_CREDENTIALS+x}" ] || exit 43
[ -z "${CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT+x}" ] || exit 44
`)
	t.Setenv("PATH", bin)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/unrelated/adc")
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "unrelated@example.com")
	dir := t.TempDir()
	adc := filepath.Join(dir, "application_default_credentials.json")
	if err := os.WriteFile(adc, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	identity := config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: dir, ADC: adc}
	other := identity
	other.CloudSDKConfig = t.TempDir()
	RecordStatus(identity, "Authenticated")
	RecordStatus(other, "Authenticated")
	if err := Logout(context.Background(), "work", identity); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(log)
	if err != nil || string(contents) != "auth revoke person@example.com --quiet|"+dir+"|person@example.com\n" {
		t.Fatalf("wrong logout invocation: %q %v", contents, err)
	}
	if Status(identity) != "Sign-in required" || Status(other) != "Authenticated" {
		t.Fatal("logout changed wrong identity status")
	}
	contents, err = os.ReadFile(adc)
	if err != nil || string(contents) != "unchanged" {
		t.Fatal("logout changed ADC")
	}
}

func TestFailedLogoutDoesNotReportSuccess(t *testing.T) {
	bin, _ := fakeGcloud(t, "echo 'network unavailable' >&2\nexit 1\n")
	t.Setenv("PATH", bin)
	identity := config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: t.TempDir()}
	RecordStatus(identity, "Authenticated")
	if err := Logout(context.Background(), "work", identity); err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("missing failure: %v", err)
	}
	if Status(identity) != "Authenticated" {
		t.Fatal("failed logout changed status")
	}
	identity.CloudSDKConfig = ""
	if err := Logout(context.Background(), "work", identity); err == nil {
		t.Fatal("logout accepted global credentials")
	}
}
