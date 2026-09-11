package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/testenv"
	"github.com/infurio/contexthop/internal/ui"
)

func gatedDiscoveryFixture(t *testing.T) config.Config {
	t.Helper()
	fixture := testenv.New(t, testenv.Options{})
	dir := fixture.Bin
	t.Setenv("PATH", dir)
	t.Setenv("BROWSER", "true")
	script := `#!/bin/sh
[ "$CLOUDSDK_CORE_ACCOUNT" = "work@example.com" ] || exit 8
echo "$*" >> "$CLOUDSDK_CONFIG/calls"
case "$1 $2" in
 "auth print-access-token")
   [ -f "$CLOUDSDK_CONFIG/logged-in" ] && exit 0
   echo "invalid_grant: authentication required" >&2
   exit 1;;
 "auth login") : > "$CLOUDSDK_CONFIG/logged-in";;
 "projects list")
   [ -f "$CLOUDSDK_CONFIG/logged-in" ] || exit 9
   printf '%s' '[{"projectId":"new-project"}]';;
 "container clusters")
   [ -f "$CLOUDSDK_CONFIG/logged-in" ] || exit 9
   printf '%s' '[]';;
 *) exit 10;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gcloud"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com", CloudSDKConfig: dir}
	cfg.Projects["one"] = config.Project{Provider: "gcp", ProjectID: "project-one", Identities: []string{"work"}}
	return cfg
}

func TestDiscoveryAuthenticatesBeforeRequestsAndResumesScope(t *testing.T) {
	for _, scope := range []string{"projects", "clusters", "all"} {
		t.Run(scope, func(t *testing.T) {
			cfg := gatedDiscoveryFixture(t)
			draft := ui.Draft{discoverIdentity: "work", discoverScope: scope, discoverProject: "one"}
			gate := runDiscoveryDialog(cfg, cfg.Clone(), draft, nil)
			if gate.Picker.Flow == nil || gate.Picker.Screen != "authentication-required" {
				t.Fatalf("missing login gate: %#v", gate.Picker)
			}
			callsPath := filepath.Join(cfg.Identities["work"].CloudSDKConfig, "calls")
			calls, _ := os.ReadFile(callsPath)
			if strings.Contains(string(calls), "projects list") || strings.Contains(string(calls), "container clusters") {
				t.Fatal("discovery ran before login")
			}
			if !gate.Picker.Flow(ui.Choice{Option: ui.Option{Name: "cancel"}}, draft).Dismiss {
				t.Fatal("cancel did not dismiss")
			}
			login := gate.Picker.Flow(ui.Choice{Option: ui.Option{Name: "terminal"}}, draft)
			if login.Process == nil {
				t.Fatal("missing login process")
			}
			failed := login.Process.Continue(context.Background(), nil, errors.New("cancelled"))
			if failed.Picker.Screen != "authentication-required" {
				t.Fatal("failed login lost retry")
			}
			if err := login.Process.Command.Run(); err != nil {
				t.Fatal(err)
			}
			var progress []string
			result := login.Process.Continue(context.Background(), func(s string) { progress = append(progress, s) }, nil)
			if !result.Picker.ResourceBrowser || !result.PersistDiscovery {
				t.Fatalf("operation did not resume: %#v", result)
			}
			calls, _ = os.ReadFile(callsPath)
			projectCalls := strings.Count(string(calls), "projects list")
			clusterCalls := strings.Count(string(calls), "container clusters")
			if scope == "clusters" && (projectCalls != 0 || clusterCalls != 1 || !strings.Contains(string(calls), "project-one")) {
				t.Fatalf("lost single-project scope: %s", calls)
			}
			if scope == "projects" && (projectCalls != 1 || clusterCalls != 0) {
				t.Fatalf("recursive discovery ran: %s", calls)
			}
			if scope == "all" && (projectCalls != 1 || clusterCalls < 1) {
				t.Fatalf("lost recursive scope: %s", calls)
			}
			if len(progress) < 2 {
				t.Fatal("resumed operation has no progress")
			}
		})
	}
}

func TestLoginSuccessMustBeVerifiedBeforeContinuation(t *testing.T) {
	cfg := gatedDiscoveryFixture(t)
	ran := false
	gate := authenticatedOperation(context.Background(), cfg, "work", "Discovery", nil, func(context.Context, func(string)) ui.Transition { ran = true; return ui.Transition{Complete: true} })
	login := gate.Picker.Flow(ui.Choice{Option: ui.Option{Name: "terminal"}}, nil)
	result := login.Process.Continue(context.Background(), nil, nil)
	if ran || result.Picker.Screen != "authentication-required" {
		t.Fatal("unverified login started operation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	authenticatedOperation(ctx, cfg, "work", "Discovery", nil, func(context.Context, func(string)) ui.Transition { ran = true; return ui.Transition{} })
	if ran {
		t.Fatal("cancelled operation ran")
	}
}

func TestSessionLoginRetainsLaunchAction(t *testing.T) {
	cfg := gatedDiscoveryFixture(t)
	choice := ui.Choice{Action: "apply-shell"}
	gate := interactiveFlow(cfg, cfg, &catalogEditorState{})(choice, ui.Draft{ui.ScreenIdentity: "work"})
	if gate.Picker.Flow == nil {
		t.Fatal("missing session login")
	}
	login := gate.Picker.Flow(ui.Choice{Option: ui.Option{Name: "terminal"}}, nil)
	if err := login.Process.Command.Run(); err != nil {
		t.Fatal(err)
	}
	result := login.Process.Continue(context.Background(), nil, nil)
	if !result.Complete || result.CompletionChoice == nil || result.CompletionChoice.Action != choice.Action {
		t.Fatal("login lost pending shell action")
	}
}

func TestControllerPersistsResumedDiscovery(t *testing.T) {
	cfg := gatedDiscoveryFixture(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	c := newApplicationController(path, cfg, recency.History{}, nil)
	c.autoSaveDiscovery = true
	draft := ui.Draft{discoverIdentity: "work", discoverScope: "projects"}
	gate := c.Prepare(ui.Choice{Screen: screenDiscover, Option: ui.Option{Name: "start"}}, draft)
	if gate.Picker.Flow == nil {
		t.Fatalf("missing gate: %#v", gate.Picker)
	}
	login := gate.Picker.Flow(ui.Choice{Option: ui.Option{Name: "terminal"}}, draft)
	if err := login.Process.Command.Run(); err != nil {
		t.Fatal(err)
	}
	result := login.Process.Continue(context.Background(), nil, nil)
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved.Projects["new-project"]; !ok {
		t.Fatalf("resumed discovery was not persisted: %#v", result)
	}
	if _, ok := c.resources.Selection().Projects["new-project"]; ok {
		t.Fatal("worker mutated UI state before acceptance")
	}
	c.Accept(result.Result)
	if _, ok := c.resources.Selection().Projects["new-project"]; !ok {
		t.Fatal("resumed discovery missing from UI")
	}
}

func TestGuidedDiscoveryAlsoRequiresLogin(t *testing.T) {
	cfg := gatedDiscoveryFixture(t)
	for _, run := range []func() ui.Transition{
		func() ui.Transition { return projectSearchPicker(cfg, cfg, "work") },
		func() ui.Transition { return discoverBrowserClusters(cfg, cfg, "work", "one") },
		func() ui.Transition { return catalogProjectDiscoveryPicker(cfg, "work") },
		func() ui.Transition { return catalogClusterDiscoveryPicker(cfg, "one", "work") },
	} {
		if result := run(); result.Picker.Screen != "authentication-required" {
			t.Fatalf("guided discovery bypassed login: %#v", result)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(cfg.Identities["work"].CloudSDKConfig, "calls"))
	if strings.Contains(string(calls), "projects list") || strings.Contains(string(calls), "container clusters") {
		t.Fatal("guided discovery ran before login")
	}
}

func TestNoninteractiveDiscoveryStopsBeforeCloudRequests(t *testing.T) {
	cfg := gatedDiscoveryFixture(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTHOP_CONFIG", path)
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previous; input.Close() }()
	err = runCatalogDiscovery([]string{"work"})
	if err == nil || !strings.Contains(err.Error(), "chop auth work") {
		t.Fatalf("missing actionable login error: %v", err)
	}
	calls, _ := os.ReadFile(filepath.Join(cfg.Identities["work"].CloudSDKConfig, "calls"))
	if strings.Contains(string(calls), "projects list") || strings.Contains(string(calls), "auth login") {
		t.Fatal("noninteractive command attempted discovery or login")
	}
}
