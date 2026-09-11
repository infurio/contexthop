package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func TestLogoutADCTargetsSelectedCredentials(t *testing.T) {
	const selectedData = `{"type":"authorized_user","client_id":"client","refresh_token":"selected-token"}`
	const differentData = `{"type":"authorized_user","client_id":"client","refresh_token":"other-token"}`
	for _, scenario := range []string{"default", "custom-copy", "custom-different-default", "missing", "failure", "unsupported", "changed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			identity := config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: filepath.Join(dir, "identity")}
			if err := os.MkdirAll(identity.CloudSDKConfig, 0700); err != nil {
				t.Fatal(err)
			}
			generated := filepath.Join(identity.CloudSDKConfig, "application_default_credentials.json")
			selected := generated
			if strings.HasPrefix(scenario, "custom") || scenario == "failure" {
				selected = filepath.Join(dir, "custom-adc.json")
				identity.ADC = selected
				data := selectedData
				if scenario == "custom-different-default" {
					data = differentData
				}
				if err := os.WriteFile(generated, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			payload := selectedData
			if scenario == "unsupported" {
				payload = `{"type":"service_account","private_key":"secret"}`
			}
			if scenario != "missing" {
				if err := os.WriteFile(selected, []byte(payload), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ambient := filepath.Join(dir, "ambient-adc.json")
			if err := os.WriteFile(ambient, []byte(differentData), 0600); err != nil {
				t.Fatal(err)
			}
			cli := filepath.Join(identity.CloudSDKConfig, "credentials.db")
			if err := os.WriteFile(cli, []byte("cli database"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", ambient)
			t.Setenv("CLOUDSDK_CONFIG", filepath.Join(dir, "ambient"))
			t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "ambient-token")
			t.Setenv("CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE", ambient)
			t.Setenv("TEST_LOG", filepath.Join(dir, "calls"))
			t.Setenv("TEST_SELECTED", selected)
			t.Setenv("TEST_SCENARIO", scenario)
			script := `#!/bin/sh
[ "$*" = "auth application-default revoke --quiet" ] || exit 90
[ "$CLOUDSDK_CORE_ACCOUNT" = work@example.com ] || exit 91
[ -z "$GOOGLE_APPLICATION_CREDENTIALS$CLOUDSDK_AUTH_ACCESS_TOKEN$CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE" ] || exit 92
[ "$CLOUDSDK_CONFIG/application_default_credentials.json" != "$TEST_SELECTED" ] || exit 93
/usr/bin/cmp "$CLOUDSDK_CONFIG/application_default_credentials.json" "$TEST_SELECTED" || exit 94
printf '%s' "$CLOUDSDK_CONFIG" > "$TEST_LOG"
if [ "$TEST_SCENARIO" = failure ]; then echo 'sensitive-provider-diagnostic' >&2; exit 95; fi
if [ "$TEST_SCENARIO" = changed ]; then printf new-credentials > "$TEST_SELECTED"; fi
/bin/rm "$CLOUDSDK_CONFIG/application_default_credentials.json"
`
			if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			RecordStatus(identity, "Authenticated")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			err := LogoutADC(ctx, "work", identity)
			failure := scenario == "failure" || scenario == "unsupported" || scenario == "changed" || scenario == "cancelled"
			if (err != nil) != failure {
				t.Fatalf("unexpected logout result: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive-provider-diagnostic") {
				t.Fatal("provider diagnostic leaked")
			}
			current, readErr := os.ReadFile(selected)
			switch {
			case scenario == "changed":
				if readErr != nil || string(current) != "new-credentials" {
					t.Fatal("new credentials removed")
				}
			case failure:
				if readErr != nil || string(current) != payload {
					t.Fatal("failed revoke removed credentials")
				}
			default:
				if !os.IsNotExist(readErr) {
					t.Fatal("selected ADC still present")
				}
			}
			if selected != generated {
				data, err := os.ReadFile(generated)
				if scenario == "custom-copy" && !os.IsNotExist(err) {
					t.Fatal("revoked default copy retained")
				}
				if scenario == "custom-different-default" && (err != nil || string(data) != differentData) {
					t.Fatal("unrelated default ADC changed")
				}
				if scenario == "failure" && (err != nil || string(data) != selectedData) {
					t.Fatal("failure removed default copy")
				}
			}
			for path, want := range map[string]string{ambient: differentData, cli: "cli database"} {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != want {
					t.Fatal("unrelated credential file changed")
				}
			}
			log, logErr := os.ReadFile(os.Getenv("TEST_LOG"))
			if scenario == "missing" || scenario == "unsupported" || scenario == "cancelled" {
				if !os.IsNotExist(logErr) {
					t.Fatal("provider called unnecessarily")
				}
			} else {
				if logErr != nil {
					t.Fatal(logErr)
				}
				if _, err := os.Stat(string(log)); !os.IsNotExist(err) {
					t.Fatal("staged credentials leaked")
				}
			}
			if !failure && scenario != "missing" && Status(identity) != "Not checked" {
				t.Fatal("CLI status still claims valid shared credentials")
			}
		})
	}
}
