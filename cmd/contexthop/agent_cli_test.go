package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/testenv"
)

func TestOrdinaryExecUsesTwoAcmeAccountsAndHandsOffLogin(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	t.Setenv("BROWSER", "true") // No real browser is used by this provider stub.
	cfg, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Destinations["Acme Development"] = config.Destination{Identity: "Acme Engineering", Project: "acme-development"}
	cfg.Destinations["Personal Lab"] = config.Destination{Identity: "Personal", Project: "personal-lab"}
	cfg.Destinations["Acme ADC"] = config.Destination{Identity: "Acme Engineering", Project: "acme-development", ADC: "identity"}
	// Names are unique within a kind, not across identities and workspaces.
	// Authenticating by this workspace name would select the unrelated account.
	cfg.Identities["Acme Development"] = cfg.Identities["Personal"]
	if err := config.Write(env.Catalog, cfg); err != nil {
		t.Fatal(err)
	}
	provider := `#!/bin/sh
case "$1 $2" in
  'auth print-access-token')
    if [ -f "$CLOUDSDK_CONFIG/signed-in" ]; then exit 0; fi
    printf 'invalid_grant: credentials have expired\n' >&2
    exit 1 ;;
  'auth login')
    mkdir -p "$CLOUDSDK_CONFIG"
    : > "$CLOUDSDK_CONFIG/signed-in"
    exit 0 ;;
esac
exit 88
`
	if err := os.WriteFile(filepath.Join(env.Bin, "gcloud"), []byte(provider), 0o700); err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(t.TempDir(), "account")
	t.Setenv("CHOP_RESULT_FILE", result)
	command := []string{"--", "/bin/sh", "-c", `printf '%s' "$CLOUDSDK_CORE_ACCOUNT" > "$CHOP_RESULT_FILE"`}
	run := func(workspace string) error { return runExec(append([]string{workspace}, command...)) }
	assertAccount := func(workspace, account string) {
		t.Helper()
		if err := run(workspace); err != nil {
			t.Fatal(err)
		}
		contents, err := os.ReadFile(result)
		if err != nil || string(contents) != account {
			t.Fatalf("%s account = %q, %v", workspace, contents, err)
		}
	}
	for _, workspace := range []string{"Acme Development", "Personal Lab"} {
		err := run(workspace)
		var handoff *authenticationRequired
		if !errors.As(err, &handoff) || handoff.adc || !strings.Contains(err.Error(), "chop auth '"+cfg.Destinations[workspace].Identity+"'") {
			t.Fatalf("%s login handoff = %v", workspace, err)
		}
		if _, err := os.Stat(result); !os.IsNotExist(err) {
			t.Fatalf("%s command started before login: %v", workspace, err)
		}
		if err := runAuth([]string{handoff.identity}); err != nil {
			t.Fatal(err)
		}
		if workspace == "Acme Development" {
			wrongLogin := filepath.Join(cfg.Identities["Personal"].CloudSDKConfig, "signed-in")
			if _, err := os.Stat(wrongLogin); !os.IsNotExist(err) {
				t.Fatalf("workspace collision authenticated unrelated identity: %v", err)
			}
			assertAccount(workspace, "alex@acme.example")
			if err := os.Remove(result); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertAccount("Acme Development", "alex@acme.example")
	assertAccount("Personal Lab", "alex@example.com")
	assertAccount("Acme Development", "alex@acme.example")
	if err := os.Remove(result); err != nil {
		t.Fatal(err)
	}
	err = run("Acme ADC")
	var handoff *authenticationRequired
	if !errors.As(err, &handoff) || !handoff.adc || !strings.Contains(err.Error(), "chop auth --adc 'Acme Engineering'") {
		t.Fatalf("ADC login handoff = %v", err)
	}
	if _, err := os.Stat(result); !os.IsNotExist(err) {
		t.Fatalf("ADC command started before login: %v", err)
	}
	if err := os.WriteFile(filepath.Join(env.Bin, "gcloud"), []byte("#!/bin/sh\nprintf 'network is unreachable\\n' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = run("Acme Development")
	if errors.As(err, &handoff) || err == nil || !strings.Contains(err.Error(), "network_error") || strings.Contains(err.Error(), "chop auth") {
		t.Fatalf("network failure was treated as login: %v", err)
	}
	if err := os.WriteFile(filepath.Join(env.Bin, "gcloud"), []byte("#!/bin/sh\nprintf 'operation not permitted\\n' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = run("Acme Development")
	if err == nil || !strings.Contains(err.Error(), "provider_access_denied; command not started") || !strings.Contains(err.Error(), "host approval") || strings.Contains(err.Error(), "chop auth") {
		t.Fatalf("local provider denial guidance = %v", err)
	}
	if _, err := os.Stat(result); !os.IsNotExist(err) {
		t.Fatalf("command started after local provider denial: %v", err)
	}
}

func TestAuthenticationInstructionQuotesIdentity(t *testing.T) {
	got := (&authenticationRequired{identity: "Acme's Lab"}).Error()
	if !strings.Contains(got, `chop auth 'Acme'\''s Lab'`) {
		t.Fatalf("unsafe authentication command: %s", got)
	}
}

func TestExecADCPermissionDenialStopsBeforeChild(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	identity := cfg.Identities["Acme Engineering"]
	identity.ADC = filepath.Join(env.Root, "acme-adc.json")
	if err := os.WriteFile(identity.ADC, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.Identities["Acme Engineering"] = identity
	cfg.Destinations["Acme ADC"] = config.Destination{Identity: "Acme Engineering", Project: "acme-development", ADC: "identity"}
	if err := config.Write(env.Catalog, cfg); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(env.Root, "provider-calls")
	t.Setenv("CHOP_TEST_PROVIDER_CALLS", calls)
	provider := `#!/bin/sh
printf '%s\n' "$*" >> "$CHOP_TEST_PROVIDER_CALLS"
case "$1 $2 $3" in
  'auth print-access-token --account') exit 0;;
  'auth application-default print-access-token')
    printf 'operation not permitted\n' >&2
    exit 1;;
esac
exit 88
`
	if err := os.WriteFile(filepath.Join(env.Bin, "gcloud"), []byte(provider), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(env.Root, "child-started")
	t.Setenv("CHOP_TEST_CHILD_MARKER", marker)
	err = runExec([]string{"--no-login", "Acme ADC", "--", "/bin/sh", "-c", `printf started > "$CHOP_TEST_CHILD_MARKER"`})
	var handoff *authenticationRequired
	if err == nil || errors.As(err, &handoff) || !strings.Contains(err.Error(), "provider_access_denied; command not started") || !strings.Contains(err.Error(), "host approval") || strings.Contains(err.Error(), "chop auth") {
		t.Fatalf("ADC permission recovery = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child started despite failed ADC check: %v", err)
	}
	data, err := os.ReadFile(calls)
	if err != nil || strings.Count(string(data), "auth print-access-token") != 1 || strings.Count(string(data), "auth application-default print-access-token") != 1 || strings.Contains(string(data), "login") {
		t.Fatalf("expected successful CLI check followed by one ADC check: %s, %v", data, err)
	}
}

func TestExecChildExit77IsNotAuthenticationHandoff(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Destinations["Acme CLI"] = config.Destination{Identity: "Acme Engineering", Project: "acme-development"}
	if err := config.Write(env.Catalog, cfg); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(env.Root, "child-runs")
	t.Setenv("CHOP_TEST_CHILD_MARKER", marker)
	err = runExec([]string{"--no-login", "Acme CLI", "--", "/bin/sh", "-c", `printf 'started\n' >> "$CHOP_TEST_CHILD_MARKER"; exit 77`})
	var completed *shellExitError
	var exited *exec.ExitError
	var handoff *authenticationRequired
	if !errors.As(err, &completed) || !errors.As(err, &exited) || exited.ExitCode() != 77 || errors.As(err, &handoff) || strings.Contains(err.Error(), "command not started") {
		t.Fatalf("child exit was mistaken for authentication: %v", err)
	}
	data, readErr := os.ReadFile(marker)
	if readErr != nil || string(data) != "started\n" {
		t.Fatalf("child did not run exactly once: %q, %v", data, readErr)
	}
}
