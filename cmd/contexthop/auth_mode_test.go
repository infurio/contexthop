package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/testenv"
)

func TestAuthBrowserDefaultAndTerminalOptIn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		options  []string
		want     string
		terminal bool
	}{
		{"browser", nil, "auth login alex@acme.example --force --launch-browser", false},
		{"terminal", []string{"--terminal"}, "auth login alex@acme.example --no-launch-browser", true},
		{"adc browser", []string{"--adc"}, "auth application-default login alex@acme.example --quiet --launch-browser", false},
		{"adc terminal", []string{"--adc", "--terminal"}, "auth application-default login alex@acme.example --quiet --no-launch-browser", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testenv.New(t, testenv.Options{Scenario: "acme"})
			t.Setenv("BROWSER", "true")
			cfg, err := config.Load(env.Catalog)
			if err != nil {
				t.Fatal(err)
			}
			if tc.terminal {
				// A terminal login must not need a usable browser binding.
				identity := cfg.Identities["Acme Engineering"]
				identity.Browser = config.BrowserProfile{App: "chrome", Profile: "Profile 7"}
				cfg.Identities["Acme Engineering"] = identity
				if err := config.Write(env.Catalog, cfg); err != nil {
					t.Fatal(err)
				}
			}
			log := filepath.Join(env.Root, "auth-args")
			t.Setenv("CHOP_TEST_AUTH_ARGS", log)
			stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$CHOP_TEST_AUTH_ARGS\"\n"
			if err := os.WriteFile(filepath.Join(env.Bin, "gcloud"), []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"Acme Engineering"}, tc.options...)
			if err := runAuth(args); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil || strings.TrimSpace(string(data)) != tc.want {
				t.Fatalf("auth arguments = %q, %v; want %q", data, err, tc.want)
			}
			if tc.terminal {
				launcher := filepath.Join(cfg.Identities["Acme Engineering"].CloudSDKConfig, "contexthop-browser")
				if _, err := os.Stat(launcher); !os.IsNotExist(err) {
					t.Fatalf("terminal mode configured browser: %v", err)
				}
			}
		})
	}
}

func TestAuthTerminalFlagParsing(t *testing.T) {
	for _, args := range [][]string{{"--terminal", "Acme Engineering"}, {"Acme Engineering", "--terminal"}} {
		name, adc, terminal, err := parseAuthArgs(args)
		if err != nil || name != "Acme Engineering" || adc || !terminal {
			t.Fatalf("parse = %q, %t, %t, %v", name, adc, terminal, err)
		}
	}
	if _, _, _, err := parseAuthArgs([]string{"--terminal", "--terminal", "Acme Engineering"}); err == nil {
		t.Fatal("accepted duplicate terminal flag")
	}
	name, _, terminal, err := parseAuthArgs([]string{"--", "--terminal"})
	if err != nil || name != "--terminal" || terminal {
		t.Fatal("interpreted a literal identity as an option")
	}
}
