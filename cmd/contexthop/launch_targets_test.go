package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/ui"
)

func launchFixture() config.Config {
	cfg := config.New()
	cfg.Identities["alice"] = config.Identity{Provider: "gcp", Account: "alice@example.com", CloudSDKConfig: "/profiles/alice"}
	cfg.Identities["bob"] = config.Identity{Provider: "gcp", Account: "bob@example.com", CloudSDKConfig: "/profiles/bob"}
	cfg.Projects["a"] = config.Project{Provider: "gcp", ProjectID: "alpha-project", Identities: []string{"alice"}}
	cfg.Projects["b"] = config.Project{Provider: "gcp", ProjectID: "beta-project", Identities: []string{"bob"}}
	cfg.Kubernetes["alpha"] = config.Kubernetes{Type: "gke", Project: "a", Cluster: "alpha-cluster", Location: "us-central1"}
	cfg.Kubernetes["beta"] = config.Kubernetes{Type: "gke", Project: "b", Cluster: "beta-cluster", Location: "europe-west1"}
	cfg.Destinations["Alpha workspace"] = config.Destination{Kubernetes: "alpha", Pinned: true}
	return cfg
}

func TestConsoleAutomaticProfileAndExplicitOverride(t *testing.T) {
	home := t.TempDir()
	writeProfiles := func(app string, profiles map[string]map[string]string) {
		root := browserProfilePath(home, config.BrowserProfile{App: app})
		for name := range profiles {
			if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
				t.Fatal(err)
			}
		}
		data, _ := json.Marshal(map[string]any{"profile": map[string]any{"info_cache": profiles}})
		if err := os.WriteFile(filepath.Join(root, "Local State"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	identity := config.Identity{Account: "alice@example.com"}
	writeProfiles("chrome", map[string]map[string]string{"Profile 1": {"user_name": "ALICE@example.com"}, "Profile 2": {"user_name": "bob@example.com"}})
	got, err := resolveConsoleBrowser(home, identity)
	if err != nil || got.App != "chrome" || got.Profile != "Profile 1" {
		t.Fatal(got, err)
	}
	writeProfiles("edge", map[string]map[string]string{"Default": {"user_name": "alice@example.com"}})
	if _, err := resolveConsoleBrowser(home, identity); err == nil {
		t.Fatal("ambiguous profile guessed")
	}
	identity.Browser = config.BrowserProfile{App: "edge", Profile: "Default"}
	if got, err := resolveConsoleBrowser(home, identity); err != nil || got.App != "edge" {
		t.Fatal(got, err)
	}
	identity = config.Identity{Account: "charlie@example.com"}
	if _, err := resolveConsoleBrowser(home, identity); err == nil {
		t.Fatal("domain match guessed")
	}
}

func TestConsoleOpensAutomaticallyMatchedProfile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS browser launcher")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := config.BrowserProfile{App: "chrome", Profile: "Profile 7"}
	if err := os.MkdirAll(browserProfilePath(home, profile), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"profile":{"info_cache":{"Profile 7":{"user_name":"alice@example.com"}}}}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(browserProfilePath(home, profile)), "Local State"), data, 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	output := filepath.Join(bin, "arguments")
	t.Setenv("TEST_CONSOLE_ARGS", output)
	if err := os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEST_CONSOLE_ARGS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r, err := resolveLaunchTarget(launchFixture(), launchTarget{Kind: "kubernetes", Name: "alpha"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := openConsole(r, "details"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output)
	if err != nil || !strings.Contains(string(got), "--profile-directory=Profile 7\n") || !strings.Contains(string(got), "/us-central1/alpha-cluster/details?project=alpha-project") {
		t.Fatalf("browser args: %s %v", got, err)
	}
}

func TestLaunchDoesNotInheritHostIdentity(t *testing.T) {
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "alice@example.com")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "alpha-project")
	cfg := launchFixture()
	r, err := resolveLaunchTarget(cfg, launchTarget{Kind: "kubernetes", Name: "beta"}, "")
	if err != nil || r.IdentityName != "bob" || r.ProjectName != "b" {
		t.Fatalf("resolution inherited host scope: %#v %v", r, err)
	}
}

func TestLaunchClusterAmbiguityAndPreference(t *testing.T) {
	cfg := launchFixture()
	p := cfg.Projects["a"]
	p.Identities = []string{"alice", "bob"}
	cfg.Projects["a"] = p
	if _, err := resolveLaunchTarget(cfg, launchTarget{Kind: "kubernetes", Name: "alpha"}, ""); err == nil {
		t.Fatal("silently selected ambiguous identity")
	}
	k := cfg.Kubernetes["alpha"]
	k.PreferredIdentity = "bob"
	cfg.Kubernetes["alpha"] = k
	r, err := resolveLaunchTarget(cfg, launchTarget{Kind: "kubernetes", Name: "alpha"}, "")
	if err != nil || r.IdentityName != "bob" {
		t.Fatalf("preference: %#v %v", r, err)
	}
}

func TestLaunchNameCollisionsAndCompletion(t *testing.T) {
	cfg := launchFixture()
	cfg.Destinations["alpha"] = config.Destination{Kubernetes: "alpha"}
	if _, err := parseLaunchTarget(cfg, "alpha"); err == nil {
		t.Fatal("collision not rejected")
	}
	for _, name := range []string{"workspace:alpha", "kubernetes:alpha"} {
		if _, err := parseLaunchTarget(cfg, name); err != nil {
			t.Fatal(err)
		}
	}
	k := cfg.Kubernetes["beta"]
	k.Hidden = true
	cfg.Kubernetes["beta"] = k
	if names := completionNames(cfg, "use"); !slices.Contains(names, "workspace:Alpha workspace") || slices.Contains(names, "kubernetes:beta") {
		t.Fatal(names)
	}
	if names := completionNames(cfg, "shell"); slices.Contains(names, "workspace:alpha") || !slices.Contains(names, "alpha") {
		t.Fatal(names)
	}
}

func TestCompletionKeepsLegacyHiddenWorkspacesVisible(t *testing.T) {
	cfg := launchFixture()
	w := cfg.Destinations["Alpha workspace"]
	w.Hidden = true
	cfg.Destinations["Alpha workspace"] = w
	if !slices.Contains(completionNames(cfg, "use"), "workspace:Alpha workspace") {
		t.Fatal("workspace missing from completion")
	}
}

func TestPreviousResolvesExactCombinationAndRejectsReassignment(t *testing.T) {
	cfg := launchFixture()
	p := state.Selection{Name: "Alpha workspace", Workspace: "Alpha workspace", Identity: "alice", Project: "a", Kubernetes: "alpha", Namespace: "payments", Account: "alice@example.com", ProjectID: "alpha-project"}
	r, err := resolvePrevious(cfg, p)
	if err != nil || r.Kubernetes.Namespace != "payments" || r.IdentityName != "alice" {
		t.Fatalf("previous %#v %v", r, err)
	}
	project := cfg.Projects["a"]
	project.ProjectID = "different-project"
	cfg.Projects["a"] = project
	if _, err := resolvePrevious(cfg, p); err == nil {
		t.Fatal("reassigned project accepted")
	}
	cfg = launchFixture()
	cfg.Destinations[p.Workspace] = config.Destination{Kubernetes: "beta"}
	if _, err := resolvePrevious(cfg, p); err == nil {
		t.Fatal("changed workspace accepted")
	}
}

func TestConsoleURLsAndProfileCommand(t *testing.T) {
	r, err := resolveLaunchTarget(launchFixture(), launchTarget{Kind: "kubernetes", Name: "alpha"}, "")
	if err != nil {
		t.Fatal(err)
	}
	value, err := consoleURL(r, "details")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(value)
	if u.Host != "console.cloud.google.com" || u.Path != "/kubernetes/clusters/details/us-central1/alpha-cluster/details" || u.Query().Get("project") != "alpha-project" {
		t.Fatal(value)
	}
	logs, _ := consoleURL(r, "logs")
	u, _ = url.Parse(logs)
	if !strings.Contains(u.Query().Get("query"), `resource.labels.cluster_name="alpha-cluster"`) {
		t.Fatal(logs)
	}
	if _, err := consoleURL(r, "unknown"); err == nil {
		t.Fatal("unknown page accepted")
	}
	command, args, err := consoleBrowserCommand("darwin", config.BrowserProfile{App: "chrome", Profile: "Profile 2"}, value)
	if err != nil || command != "open" || !slices.Equal(args, []string{"-na", "Google Chrome", "--args", "--profile-directory=Profile 2", value}) {
		t.Fatalf("command: %s %#v %v", command, args, err)
	}
	if _, _, err := consoleBrowserCommand("darwin", config.BrowserProfile{}, value); err == nil {
		t.Fatal("silently opened default browser")
	}
	if _, _, err := consoleBrowserCommand("darwin", config.BrowserProfile{App: "chrome", Profile: "../../Other"}, value); err == nil {
		t.Fatal("profile traversal accepted")
	}
}

func TestBrowserConfigurationPreservesLegacyFields(t *testing.T) {
	cfg := launchFixture()
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("CONTEXTHOP_CONFIG", path)
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := runBrowserConfig([]string{"alice", "edge", "Profile 3"}); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Identities["alice"].Browser.Profile != "Profile 3" || !saved.Destinations["Alpha workspace"].Pinned {
		t.Fatal("config changes lost")
	}
	saved, _ = config.Load(path)

}

func TestApplicationBrowserAndHighlightedActions(t *testing.T) {
	cfg := launchFixture()
	c := newApplicationController("", cfg, recency.History{}, nil)
	picker := c.Browser(ui.ScreenKubernetes, ui.Draft{})
	found := false
	for _, option := range picker.Options {
		if option.Name == "alpha" {
			for _, action := range option.Actions {
				found = found || action.Action == "open-console"
			}
		}
	}
	if !found {
		t.Fatal("missing highlighted Console action")
	}
	tr := c.Prepare(ui.Choice{Action: "configure-browser"}, ui.Draft{})
	if tr.Picker.Screen != browserIdentity || len(tr.Picker.Options) != 2 {
		t.Fatal(tr.Picker)
	}
}

// Keep fixture construction and tests isolated from real provider state.
func TestWorkspaceResolutionNeedsNoProviderExecutables(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CONTEXTHOP_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	if err := config.Write(os.Getenv("CONTEXTHOP_CONFIG"), launchFixture()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Destination(launchFixture(), "Alpha workspace"); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchActivationDefaultsAndExplicitModes(t *testing.T) {
	for _, mode := range []string{"use", "apply", "shell"} {
		for _, managed := range []bool{false, true} {
			t.Run(mode+strconv.FormatBool(managed), func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("CONTEXTHOP_CACHE_DIR", dir)
				t.Setenv("KUBECONFIG", filepath.Join(dir, "absent"))
				t.Setenv(state.SessionFileEnv, "")
				t.Setenv("CONTEXTHOP_CONTEXT", "")
				activation := filepath.Join(dir, "activation")
				t.Setenv(session.ActivationFileEnv, activation)
				marker := filepath.Join(dir, "child")
				t.Setenv("TEST_LAUNCH_CHILD", marker)
				shell := filepath.Join(dir, "test-shell")
				if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf '%s' \"$CONTEXTHOP_CONTEXT\" > \"$TEST_LAUNCH_CHILD\"\n"), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("SHELL", shell)
				if managed {
					previous, err := session.Prepare(context.Background(), resolver.Resolved{Name: "previous"})
					if err != nil {
						t.Fatal(err)
					}
					defer previous.Close()
					t.Setenv(state.SessionFileEnv, previous.Manifest)
					t.Setenv("CONTEXTHOP_CONTEXT", "previous")
				}
				r := resolver.Resolved{Name: "next", DockerName: "next", Docker: &config.Docker{Context: "next"}}
				if err := activateLaunch(r, mode); err != nil {
					t.Fatal(err)
				}
				child := mode == "shell" || mode == "use" && !managed
				_, childErr := os.Stat(marker)
				_, activationErr := os.Stat(activation)
				if (childErr == nil) != child || (activationErr == nil) == child {
					t.Fatalf("child=%t childErr=%v activationErr=%v", child, childErr, activationErr)
				}
				if os.Getenv(session.ActivationFileEnv) != activation {
					t.Fatal("activation binding not restored")
				}
				if managed && os.Getenv("CONTEXTHOP_CONTEXT") != "previous" {
					t.Fatal("binary mutated caller before shell sourced activation")
				}
			})
		}
	}
}

func TestWebConsoleAvailabilityForWorkspaceTypes(t *testing.T) {
	cfg := launchFixture()
	cfg.Destinations["local"] = config.Destination{}
	cfg.Projects["other"] = config.Project{Provider: "other", ProjectID: "other"}
	cfg.Destinations["other"] = config.Destination{Project: "other"}
	cfg.Destinations["gcp"] = config.Destination{Project: "a"}
	for name, want := range map[string]bool{"local": false, "other": false, "gcp": true} {
		if got := launchHasWebConsole(cfg, launchTarget{Kind: "workspace", Name: name}); got != want {
			t.Fatalf("%s web console = %t", name, got)
		}
	}
}
