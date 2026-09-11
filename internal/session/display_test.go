package session

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/testenv"
)

func TestSelectionDisplayOnlyUsesSelectedComponentsAndTags(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	identity := cfg.Identities["Acme Engineering"]
	identity.Tags = []string{"Engineering"}
	cfg.Identities["Acme Engineering"] = identity
	cfg.Tags["Engineering"] = config.Tag{Color: "#123456"}
	r, err := resolver.Components(cfg, resolver.Selection{Identity: "Acme Engineering"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.DisplayTags) != 1 || r.DisplayTags["Engineering"] != "#123456" {
		t.Fatal(r.DisplayTags)
	}
	m := state.Manifest{Version: 1, SessionID: "acme-display", DisplayTags: r.DisplayTags, PromptColors: r.PromptColors,
		Expected: state.Component{Identity: identity.Account, Docker: disabledDockerContext}}
	want := "ContextHop\nIdentity: alex@acme.example\nTags: Engineering"
	if got := formatSummary(m, "inherited-namespace", false); got != want {
		t.Fatalf("summary = %q", got)
	}
	if got := formatPromptPrefix(m, "inherited-namespace", false); got != "[alex@acme.example|Engineering] " {
		t.Fatalf("prompt = %q", got)
	}
	colored := formatSummary(m, "", true)
	if !strings.Contains(colored, "\x1b[38;2;18;52;86mEngineering") || !strings.Contains(colored, "\x1b[38;5;245mIdentity: ") {
		t.Fatalf("colors = %q", colored)
	}
	data, _ := json.Marshal(m)
	path := filepath.Join(env.Root, "session.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.SessionFileEnv, path)
	t.Setenv("CLOUDSDK_CORE_PROJECT", "unselected-project")
	t.Setenv("DOCKER_CONTEXT", "unselected-engine")
	for _, setting := range []string{"NO_COLOR", "TERM"} {
		t.Run(setting, func(t *testing.T) {
			if setting == "NO_COLOR" {
				t.Setenv(setting, "1")
			} else {
				t.Setenv(setting, "dumb")
			}
			if got := CurrentSummary(); got != want {
				t.Fatalf("plain summary = %q", got)
			}
		})
	}
	m.Expected = state.Component{Docker: disabledDockerContext}
	if formatSummary(m, "", true) != "" || formatPromptPrefix(m, "", true) != "" {
		t.Fatal("empty selection displayed")
	}
	m.Expected = state.Component{Identity: "acme\x1b]0;title\a\naccount"}
	if strings.ContainsAny(formatSummary(m, "", false), "\x1b\a") {
		t.Fatal("terminal controls escaped sanitization")
	}
}

func TestSummaryHookIsInteractiveAndEmitsOncePerChange(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh unavailable")
	}
	env, err := testenv.Create(t.TempDir(), testenv.Options{Scenario: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(env.Bin, "chop")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$1\" = _summary ]; then cat \"$SUMMARY_FILE\"; fi\n"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(env.Root, "summary")
	env.Vars["SUMMARY_FILE"] = path
	if err := os.WriteFile(path, []byte("Identity: acme-one"), 0600); err != nil {
		t.Fatal(err)
	}
	init := filepath.Join(env.Root, "init.zsh")
	if err := os.WriteFile(init, []byte(CurrentShellInit(binary)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, interactive := range []bool{false, true} {
		args := []string{"-f"}
		if interactive {
			args = append(args, "-i")
		}
		args = append(args, "-c", `source "$1"; _chop_show_summary`, "_", init)
		cmd := exec.Command(zsh, args...)
		cmd.Env = env.Environ()
		if output, err := cmd.CombinedOutput(); err != nil || strings.Contains(string(output), "Identity:") {
			t.Fatalf("redirected summary: %v %s", err, output)
		}
	}
	// macOS script supplies a real TTY without using the developer's terminal.
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script unavailable")
	}
	cmd := exec.Command(script, "-q", "/dev/null", zsh, "-f", "-i", "-c", `
source "$1"
_chop_show_summary
_chop_show_summary
# A new interactive child has its own once-only reminder.
zsh -f -i -c 'source "$1"; _chop_show_summary; _chop_show_summary' _ "$1"
# Re-sourcing, an unchanged selection, and a canceled operation stay quiet.
source "$1"
false
_chop_show_summary
print -rn -- 'Identity: acme-two' > "$SUMMARY_FILE"
_chop_show_summary
_chop_show_summary
# Off emits nothing, then returning to summary emits the selection once.
: > "$SUMMARY_FILE"
_chop_show_summary
print -rn -- 'Identity: acme-two' > "$SUMMARY_FILE"
_chop_show_summary
`, "_", init)
	cmd.Env = env.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("interactive hook: %v %s", err, output)
	}
	if strings.Count(string(output), "Identity: acme-one") != 2 || strings.Count(string(output), "Identity: acme-two") != 2 {
		t.Fatalf("summary count: %q", output)
	}
}

func TestDisplayTagsSurvivePreparationAndSharedAdoption(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	docker := cfg.Docker["Local Docker"]
	docker.Tags = []string{"development"}
	cfg.Docker["Local Docker"] = docker
	resolved, err := resolver.Destination(cfg, "Local containers")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := PublishShared(prepared); err != nil {
		t.Fatal(err)
	}
	follower, _, err := FollowShared("")
	if err != nil {
		t.Fatal(err)
	}
	if follower == nil {
		t.Fatal("missing follower")
	}
	defer follower.Close()
	t.Setenv(state.SessionFileEnv, follower.Manifest)
	t.Setenv("NO_COLOR", "1")
	if got := CurrentSummary(); got != "ContextHop\nDocker: desktop-linux\nTags: development" {
		t.Fatalf("adopted summary = %q", got)
	}
	m, err := state.LoadManifest(follower.Manifest)
	if err != nil || m.DisplayTags["development"] != "#4ade80" {
		t.Fatalf("adopted tags lost colour: %v", err)
	}
}

func TestPromptUsesOneSelectionLabel(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	r, err := resolver.Destination(cfg, "Payments Dev")
	if err != nil {
		t.Fatal(err)
	}
	m := state.Manifest{WorkspaceName: r.WorkspaceName, KubernetesLabel: r.KubernetesName,
		DisplayTags: r.DisplayTags, PromptColors: r.PromptColors,
		Expected: state.Component{Identity: r.Identity.Account, Project: r.Project.ProjectID,
			Kubernetes: r.KubernetesName, Docker: "desktop-linux"}}
	cases := []struct {
		name, want string
		clear      func()
	}{
		{"workspace", "[Payments Dev|development] ", func() {}},
		{"kubernetes", "[payments-dev|development] ", func() { m.WorkspaceName = "" }},
		{"docker", "[desktop-linux|development] ", func() { m.Expected.Kubernetes = "" }},
		{"project", "[acme-development|development] ", func() { m.Expected.Docker = disabledDockerContext }},
		{"identity", "[alex@acme.example|development] ", func() { m.Expected.Project = "" }},
	}
	for _, tc := range cases {
		tc.clear()
		if got := formatPromptPrefix(m, "default", false); got != tc.want {
			t.Fatalf("%s: %q", tc.name, got)
		}
	}
}

func TestStatusSharesSummaryColoursAndRetainsFullDetail(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	r, err := resolver.Destination(cfg, "Payments Dev")
	if err != nil {
		t.Fatal(err)
	}
	m := state.Manifest{DisplayTags: r.DisplayTags, PromptColors: r.PromptColors,
		Expected: state.Component{Identity: r.Identity.Account, Project: r.Project.ProjectID, Kubernetes: r.KubernetesName, Namespace: "payments"}}
	snapshot := state.Snapshot{Managed: true, Destination: r.WorkspaceName, LocalStatus: "LOCAL MATCH", Observed: m.Expected}
	plain := FormatStatus(snapshot, m, false)
	want := "Context: Payments Dev (LOCAL MATCH)\nIdentity: alex@acme.example\nCloud project: acme-development\nKubernetes: payments-dev\nNamespace: payments\nDocker: none\nADC: none\nTags: development\n"
	if plain != want {
		t.Fatalf("status = %q", plain)
	}
	summary, status := formatSummary(m, "payments", true), FormatStatus(snapshot, m, true)
	for _, styled := range []string{
		"\x1b[38;5;245mIdentity: ", "\x1b[38;5;39malex@acme.example",
		"\x1b[38;2;74;222;128macme-development", "\x1b[38;2;74;222;128mpayments-dev",
		"\x1b[38;5;75mpayments", "\x1b[38;2;74;222;128mdevelopment",
	} {
		if !strings.Contains(summary, styled) || !strings.Contains(status, styled) {
			t.Fatalf("missing shared style %q", styled)
		}
	}
	if !strings.Contains(status, "\x1b[38;5;245mnone") {
		t.Fatal("unset values should be muted")
	}
	snapshot.LocalStatus = "CONTEXT-DRIFT"
	snapshot.Observed.Project = "acme-staging"
	if got := FormatStatus(snapshot, m, false); !strings.Contains(got, "CONTEXT-DRIFT") || !strings.Contains(got, "Cloud project: acme-staging") {
		t.Fatal("status hid observed drift")
	}
}
