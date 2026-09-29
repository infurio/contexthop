package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/testenv"
)

func TestAuthenticationHandoffUsesOptionTerminatorForLeadingDash(t *testing.T) {
	for _, adc := range []bool{false, true} {
		err := (&authenticationRequired{identity: "-Acme's Identity", adc: adc}).Error()
		if !strings.Contains(err, "-- '-Acme'\\''s Identity'") {
			t.Fatalf("unsafe authentication command: %s", err)
		}
		args := []string{"--", "-Acme's Identity"}
		if adc {
			args = append([]string{"--adc"}, args...)
		}
		name, gotADC, _, parseErr := parseAuthArgs(args)
		if parseErr != nil || name != "-Acme's Identity" || gotADC != adc {
			t.Fatalf("recovery command rejected: %q, %t, %v", name, gotADC, parseErr)
		}
	}
	name, adc, _, err := parseAuthArgs([]string{"--", "--adc"})
	if err != nil || adc || name != "--adc" {
		t.Fatalf("option after terminator was interpreted: %q, %t, %v", name, adc, err)
	}
}

func TestLoginDisabledIsScopedToLaunch(t *testing.T) {
	parent := context.Background()
	ctx := withLoginDisabled(parent)
	if loginDisabled(parent) || !loginDisabled(ctx) {
		t.Fatal("login option was not scoped to the launch context")
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	if !loginDisabled(child) {
		t.Fatal("launch authentication option lost in child context")
	}
}

func TestLoginDisabledReusesCredentialsAndNeverStartsLogin(t *testing.T) {
	fixture := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(fixture.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	identity := cfg.Identities["Acme Engineering"]
	if identity.Account == "" {
		t.Fatal("missing Acme identity fixture")
	}
	if err := os.MkdirAll(identity.CloudSDKConfig, 0700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
case "$1 $2" in
 "auth print-access-token")
   [ -f "$CLOUDSDK_CONFIG/ready" ] && exit 0
   echo 'invalid_grant: authentication required' >&2
   exit 1;;
 *) echo 'unexpected login' > "$CLOUDSDK_CONFIG/unexpected-login"; exit 2;;
esac
`
	if err := os.WriteFile(filepath.Join(fixture.Bin, "gcloud"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx := withLoginDisabled(context.Background())
	err = ensureIdentityLogin(ctx, "Acme Engineering", identity)
	var handoff *authenticationRequired
	if !errors.As(err, &handoff) || handoff.identity != "Acme Engineering" {
		t.Fatalf("missing identity recovery: %v", err)
	}
	if err := os.WriteFile(filepath.Join(identity.CloudSDKConfig, "ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureIdentityLogin(ctx, "Acme Engineering", identity); err != nil {
		t.Fatalf("valid credentials not reused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(identity.CloudSDKConfig, "unexpected-login")); !os.IsNotExist(err) {
		t.Fatalf("provider login was attempted: %v", err)
	}
}

func TestLaunchAuthErrorPreservesCauseAndDistinguishesPermission(t *testing.T) {
	denied := &cloudauth.CheckError{Kind: "provider_access_denied"}
	err := launchAuthError(denied)
	if !errors.Is(err, denied) || !strings.Contains(err.Error(), "command not started") || !strings.Contains(err.Error(), "host approval") {
		t.Fatalf("missing safe permission recovery: %v", err)
	}
	network := &cloudauth.CheckError{Kind: "network_error"}
	if launchAuthError(network) != network {
		t.Fatal("network failure gained automatic retry guidance")
	}
}
