package main

import (
	"errors"
	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryOffersOneAuthenticationChoiceAndRetries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("BROWSER", "true")
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	script := filepath.Join(dir, "gcloud")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'Reauthentication failed' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: t.TempDir()}
	initial := projectSearchPickerAuthenticated(cfg, cfg, "work")
	if initial.Picker.Screen != screenProviderAuth || len(initial.Picker.Options) != 2 {
		t.Fatalf("discovery did not go directly to sign-in choice: %#v", initial.Picker)
	}
	if cloudauth.Status(cfg.Identities["work"]) != "Sign-in required" {
		t.Fatal("discovery failure did not update auth status")
	}
	for index, mode := range []string{"browser", "terminal"} {
		transition := providerAuthTransition(cfg, cfg, initial.Picker.Options[index].Name, &catalogEditorState{})
		if transition.Process == nil {
			t.Fatal("sign-in asked for another confirmation")
		}
		args := strings.Join(transition.Process.Command.Args, " ")
		if strings.Contains(args, "--force") != (mode == "browser") {
			t.Fatalf("wrong %s command: %s", mode, args)
		}
		failed := transition.Process.Done(errors.New("cancelled"))
		if !failed.ReplaceCurrent || failed.Picker.Screen != screenProviderAuth {
			t.Fatal("cancelled login lost retry screen")
		}
	}
	login := providerAuthTransition(cfg, cfg, initial.Picker.Options[0].Name, &catalogEditorState{})
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '[{\"projectId\":\"example-project\"}]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	retried := login.Process.Done(nil)
	if !retried.ReplaceCurrent || retried.Picker.Screen != ui.ScreenProject {
		t.Fatalf("successful login did not retry discovery: %#v", retried)
	}
	if cloudauth.Status(cfg.Identities["work"]) != "Authenticated" {
		t.Fatal("successful login did not update status")
	}
}

func TestCachedProjectsDoNotSuppressSignInRecovery(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: t.TempDir()}
	script := filepath.Join(dir, "gcloud")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '[{\"projectId\":\"example-project\"}]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	projectSearchPickerAuthenticated(cfg, cfg, "work")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'There was a problem refreshing your current auth tokens: invalid_grant: Token has been expired or revoked' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	got := projectSearchPickerAuthenticated(cfg, cfg, "work")
	if got.Picker.Screen != screenProviderAuth {
		t.Fatalf("cached results suppressed authentication recovery: %#v", got.Picker)
	}
}

func TestDiscoveryFailureNoticesExplainRecovery(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{"gcloud: exit status 1: invalid_grant", "sign-in expired"},
		{"gcloud: exit status 1: connection refused", "Could not connect"},
		{"context deadline exceeded", "timed out"},
		{"gcloud: exit status 1: PERMISSION_DENIED", "denied access"},
		{"gcloud: exit status 1: unexpected output", "Discovery failed"},
	} {
		got := discoveryFailureNotice(errors.New(test.raw))
		if !strings.Contains(got, test.want) || !strings.Contains(strings.ToLower(got), "press d") || strings.Contains(got, "exit status") {
			t.Fatalf("unhelpful notice: %s", got)
		}
	}
}

func TestCachedDiscoveryPreservesObservationAndDiagnostic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: t.TempDir()}
	script := filepath.Join(dir, "gcloud")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '[{\"projectId\":\"cached-project\"}]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	first := cfg.Clone()
	projectSearchPickerAuthenticated(first, cfg, "work")
	observed := first.Projects["cached-project"].ObservedAt
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'connection refused: diagnostic detail' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	second := cfg.Clone()
	result := projectSearchPickerAuthenticated(second, cfg, "work")
	if observed == "" || second.Projects["cached-project"].ObservedAt != observed {
		t.Fatal("cached record stamped as fresh")
	}
	if !strings.Contains(result.Picker.Description, "cached results") || !strings.Contains(result.Picker.OperationDetails, "diagnostic detail") {
		t.Fatal("cache status or diagnostic lost")
	}
}

func TestDiscoveryFailureShowsProviderReasonAndDetailsShortcut(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte("#!/bin/sh\necho 'ERROR: (gcloud.projects.list) The billing service is unavailable.' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: t.TempDir()}
	result := projectSearchPickerAuthenticated(cfg, cfg, "work")
	if !strings.Contains(result.Picker.Description, "The billing service is unavailable.") || !strings.Contains(result.Picker.Description, "work@example.com") {
		t.Fatalf("reason missing: %s", result.Picker.Description)
	}
	model := ui.NewAppModel(ui.AppOptions{StartScreen: result.Picker.Screen, Pickers: map[ui.Screen]ui.Picker{result.Picker.Screen: result.Picker}})
	if !strings.Contains(model.View().Content, "Full details") {
		t.Fatal("full diagnostic shortcut not visible")
	}
	if !strings.Contains(result.Picker.OperationDetails, "gcloud.projects.list") {
		t.Fatal("original diagnostic lost")
	}
}

func TestIdentityLoginLogoutReturnsWithoutDiscovery(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: t.TempDir()}
	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	draft := ui.Draft{ui.ScreenIdentity: "other", ui.ScreenProject: "staged-project", ui.ScreenDocker: "docker"}
	flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
	opened := flow(ui.Choice{Screen: ui.ScreenIdentity, Option: ui.Option{Name: "work"}, Action: "identity-auth"}, draft)
	if len(opened.Picker.Options) != 5 || !strings.Contains(opened.Picker.Title, "work@example.com [work]") {
		t.Fatal("missing identity authentication actions")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("BROWSER", "true")
	if err := os.WriteFile(filepath.Join(bin, "gcloud"), []byte("#!/bin/sh\n[ \"$1\" = auth ] || exit 42\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, option := range opened.Picker.Options {
		if strings.HasSuffix(option.Name, "\x00adc") {
			continue
		} // ADC is covered by TestStandaloneADCFromIdentityOptions.
		transition := flow(ui.Choice{Screen: screenProviderAuth, Option: option}, draft)
		if !strings.HasSuffix(option.Name, "logout") {
			if transition.Process == nil {
				t.Fatal("login did not hand off terminal")
			}
			cancelled := transition.Process.Done(errors.New("cancelled"))
			if cancelled.Picker.Screen != screenProviderAuth {
				t.Fatal("login cancellation lost recovery dialog")
			}
			if err := transition.Process.Command.Run(); err != nil {
				t.Fatal(err)
			}
			transition = transition.Process.Done(nil)
		}
		if transition.Picker.Screen != ui.ScreenIdentity || !transition.ReplaceCurrent || transition.PersistDiscovery || len(transition.DraftUpdates) != 0 {
			t.Fatalf("authentication changed selection or started discovery: %#v", transition)
		}
		if !strings.Contains(transition.Picker.Description, "work@example.com [work]") {
			t.Fatal("missing account in completion notice")
		}
	}
	if draft[ui.ScreenIdentity] != "other" || draft[ui.ScreenProject] != "staged-project" {
		t.Fatal("authentication changed staged selection")
	}
}
