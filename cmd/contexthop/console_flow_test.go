package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/destination"

	"github.com/infurio/contexthop/internal/ui"
	"os"
	"strings"
	"testing"
)

func consoleFixture(t *testing.T) config.Config {
	t.Helper()
	cfg := consoleCatalogFixture()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for name, identity := range cfg.Identities {
		identity.Browser = config.BrowserProfile{App: "chrome", Profile: "Profile-" + name}
		cfg.Identities[name] = identity
		if err := os.MkdirAll(browserProfilePath(home, identity.Browser), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func TestWebConsoleTabAvailabilityAndProjectBrowser(t *testing.T) {
	cfg := consoleFixture(t)
	for _, test := range []struct {
		screen ui.Screen
		name   string
		want   bool
	}{{ui.ScreenIdentity, "alice", true}, {ui.ScreenProject, "a", true}, {ui.ScreenKubernetes, "alpha", true}, {ui.ScreenKubernetes, "local", false}, {ui.ScreenDocker, "desktop", false}, {ui.ScreenWorkspace, "Alpha workspace", true}} {
		picker := interactiveBrowserPicker(cfg, cfg, test.screen, ui.Draft{})
		found := false
		for _, row := range picker.Options {
			if row.Name == test.name {
				for _, a := range row.Actions {
					if a.Action == "open-console" {
						found = true
						if a.Key != ui.KeyConsole || a.Label != "Open web console" {
							t.Fatal(a)
						}
					}
				}
			}
		}
		if found != test.want {
			t.Fatal("wrong web availability", test, found)
		}
	}
	tr := webConsoleFlow(cfg, launchTarget{Kind: "project", Name: "a"}, "alice")
	if tr.Process == nil {
		t.Fatal("project did not resolve directly", tr.Picker.Description)
	}
	args := strings.Join(tr.Process.Command.Args, " ")
	if !strings.Contains(args, "--profile-directory=Profile-alice") || !strings.Contains(args, "/home/dashboard?project=") {
		t.Fatal("wrong project URL/profile", args)
	}
	if tr.Process.Done(nil).Picker.Screen != "console-result" {
		t.Fatal("web process lost return dialog")
	}
	for _, kind := range []string{"identity", "project"} {
		meta, _ := destination.Describe(cfg, launchTarget{Kind: kind, Name: map[string]string{"identity": "alice", "project": "a"}[kind]})
		if !meta.CanWebConsole {
			t.Fatal("Console capability missing", kind)
		}
	}
}

func TestWebConsoleAmbiguityStaysInApplication(t *testing.T) {
	cfg := consoleFixture(t)
	p := cfg.Projects["a"]
	p.Identities = []string{"alice", "bob"}
	cfg.Projects["a"] = p
	tr := webConsoleFlow(cfg, launchTarget{Kind: "project", Name: "a"}, "")
	if tr.Picker.Screen != "web-console-identity" || tr.Picker.Flow == nil {
		t.Fatal("missing in-app account choice", tr)
	}
	next := tr.Picker.Flow(ui.Choice{Option: ui.Option{Name: "bob"}}, ui.Draft{})
	if next.Process == nil || !strings.Contains(strings.Join(next.Process.Command.Args, " "), "Profile-bob") {
		t.Fatal("chosen browser profile not used", next)
	}
	if !next.Process.Done(nil).ReplaceCurrent {
		t.Fatal("result would retain redundant account chooser")
	}
}

func TestIdentityHomeAndProjectLinkedLocalConsole(t *testing.T) {
	cfg := consoleFixture(t)
	tr := webConsoleFlow(cfg, launchTarget{Kind: "identity", Name: "alice"}, "")
	if tr.Process == nil || tr.Process.Command.Args[len(tr.Process.Command.Args)-1] != "https://console.cloud.google.com/" {
		t.Fatal("identity home missing", tr)
	}
	k := cfg.Kubernetes["local"]
	k.Project = "a"
	cfg.Kubernetes["local"] = k
	tr = webConsoleFlow(cfg, launchTarget{Kind: "kubernetes", Name: "local"}, "alice")
	if tr.Process == nil || !strings.Contains(strings.Join(tr.Process.Command.Args, " "), "/home/dashboard?project=") {
		t.Fatal("local target did not use associated project dashboard", tr)
	}
	cfg.Identities["alice"] = config.Identity{Provider: "gcp", Account: "alice@example.invalid"}
	tr = webConsoleFlow(cfg, launchTarget{Kind: "identity", Name: "alice"}, "")
	if tr.Process != nil || !strings.Contains(tr.Picker.Description, "no browser profile matches") {
		t.Fatal("missing profile should not open default browser")
	}
}

func TestWebChooserCancellationPreservesOriginAndSearch(t *testing.T) {
	cfg := consoleFixture(t)
	p := cfg.Projects["a"]
	p.Identities = []string{"alice", "bob"}
	cfg.Projects["a"] = p
	picker := interactiveBrowserPicker(cfg, cfg, ui.ScreenProject, ui.Draft{})
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenProject, ResourceBrowser: true, ComposeSelection: true,
		Pickers: map[ui.Screen]ui.Picker{ui.ScreenProject: picker},
		Flow: func(choice ui.Choice, draft ui.Draft) ui.Transition {
			return webConsoleFlow(cfg, launchTarget{Kind: "project", Name: choice.Option.Name}, draft[ui.ScreenIdentity])
		},
	})
	press := func(msg tea.Msg) { next, _ := m.Update(msg); m = next.(ui.AppModel) }
	press(tea.WindowSizeMsg{Width: 118, Height: 28})
	press(tea.KeyPressMsg{Code: '/', Text: "/"})
	press(tea.KeyPressMsg{Code: 'a', Text: "a"})
	before := m.View().Content
	press(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.CurrentScreen() != "web-console-identity" {
		t.Fatal("web action unavailable during search", m.View().Content)
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.CurrentScreen() != ui.ScreenProject || m.StackDepth() != 1 || m.View().Content != before {
		t.Fatal("cancel changed search, selection or origin", m.View().Content)
	}
}

func TestFilteredKubernetesWebIdentityChoiceAndCancel(t *testing.T) {
	cfg := consoleFixture(t)
	p := cfg.Projects["a"]
	p.Identities = []string{"alice", "bob"}
	cfg.Projects["a"] = p
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenKubernetes, ResourceBrowser: true, ComposeSelection: true,
		Pickers: map[ui.Screen]ui.Picker{ui.ScreenKubernetes: interactiveBrowserPicker(cfg, cfg, ui.ScreenKubernetes, ui.Draft{})},
		Flow: func(choice ui.Choice, draft ui.Draft) ui.Transition {
			tr, _ := browserConfigFlow(cfg, choice, draft, nil)
			return tr
		},
	})
	press := func(msg tea.Msg) { next, _ := m.Update(msg); m = next.(ui.AppModel) }
	press(tea.WindowSizeMsg{Width: 118, Height: 28})
	press(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "alpha" {
		press(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	before := m.View().Content
	press(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.CurrentScreen() != "web-console-identity" || !strings.Contains(m.View().Content, "bob@example.com") {
		t.Fatal("missing account choice", m.View().Content)
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.CurrentScreen() != ui.ScreenKubernetes || m.View().Content != before {
		t.Fatal("cancel changed filtered Kubernetes")
	}
}

func TestWebUnavailableWithoutEligibleIdentity(t *testing.T) {
	cfg := consoleFixture(t)
	cfg.Projects["unmapped"] = config.Project{Provider: "gcp", ProjectID: "fictional-unmapped"}
	for _, target := range []launchTarget{{Kind: "project", Name: "unmapped"}, {Kind: "docker", Name: "desktop"}, {Kind: "kubernetes", Name: "local"}} {
		if destination.HasWebConsole(cfg, target) {
			t.Fatal("Web offered without eligible identity", target)
		}
		tr := webConsoleFlow(cfg, target, "")
		if tr.Process != nil || tr.Picker.Screen == "web-console-identity" {
			t.Fatal("unsupported target tried to open browser")
		}
	}
}

func consoleCatalogFixture() config.Config {
	cfg := launchFixture()
	cfg.Destinations["Alpha workspace"] = config.Destination{Kubernetes: "alpha"}
	cfg.Docker["desktop"] = config.Docker{Context: "desktop-linux"}
	cfg.Kubernetes["local"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/fictional/kubeconfig", Context: "local-cluster"}
	cfg.Tags["fictional"] = config.Tag{Color: "#123456"}
	v := cfg.Identities["alice"]
	v.Tags = []string{"fictional"}
	cfg.Identities["alice"] = v
	return cfg
}
