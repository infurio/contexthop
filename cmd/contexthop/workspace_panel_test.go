package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/ui"
)

func TestWorkspacePanelActionsAndDeletion(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "local"}
	cfg.Destinations["Workspace"] = config.Destination{Docker: "local", Hidden: true}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	c := newApplicationController(path, cfg, recency.History{}, nil)
	picker := c.Browser(ui.ScreenWorkspace, nil)
	if len(picker.Options) != 1 || picker.Options[0].Hidden {
		t.Fatalf("saved workspace not visible: %#v", picker)
	}
	actions := picker.Options[0].Actions
	if len(actions) != 4 || actions[0].Action != "workspace-configure" || actions[1].Action != "workspace-copy" || actions[2].Action != "remove-entity" || actions[3].Action != "entity-labels" {
		t.Fatalf("workspace actions: %#v", actions)
	}
	model := ui.NewAppModel(ui.AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ui.ScreenWorkspace, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept})
	press := func(key tea.KeyPressMsg) { next, _ := model.Update(key); model = next.(ui.AppModel) }
	next, _ := model.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	model = next.(ui.AppModel)
	for _, key := range []string{"H", "m", "u", "x"} {
		press(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		if model.CurrentScreen() != ui.ScreenWorkspace || model.StackDepth() != 1 {
			t.Fatalf("%s opened obsolete workflow", key)
		}
	}
	view := ansi.Strip(model.View().Content)
	for _, unwanted := range []string{"[p]", "[space] Select", "[shift+n] Copy"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("inappropriate workspace shortcut %q: %s", unwanted, view)
		}
	}
	press(tea.KeyPressMsg{Code: 'o', Text: "o"})
	menu := ansi.Strip(model.View().Content)
	if !strings.Contains(menu, "Duplicate workspace") || !strings.Contains(menu, "shift+n") || !strings.Contains(menu, "New workspace") {
		t.Fatal("creation and duplication missing in Options", menu)
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	for _, label := range []string{"Show hidden", "Hide workspace", "Edit selections"} {
		if strings.Contains(view, label) {
			t.Fatalf("obsolete footer %q: %s", label, view)
		}
	}

	press(tea.KeyPressMsg{Code: 'o', Text: "o"})
	press(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if model.CurrentScreen() != ui.ScreenConfirm {
		t.Fatal("Ctrl+D did not review deletion")
	}
	fresh, _ := config.Load(path)
	if len(fresh.Destinations) != 1 {
		t.Fatal("deleted before confirmation")
	}
	press(tea.KeyPressMsg{Code: tea.KeyHome}) // Choose Delete instead of the default Cancel.
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	fresh, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Destinations) != 0 || len(fresh.Docker) != 1 || model.CurrentScreen() != ui.ScreenWorkspace {
		t.Fatalf("deletion did not remove only preset: %#v", fresh)
	}
}

func TestCtrlDIsDeleteShortcutForEveryEntity(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		cfg := visibilityConfig()
		if hidden {
			identity := cfg.Identities["identity"]
			identity.Hidden = true
			cfg.Identities["identity"] = identity
			project := cfg.Projects["project"]
			project.Hidden = true
			cfg.Projects["project"] = project
			cluster := cfg.Kubernetes["cluster"]
			cluster.Hidden = true
			cfg.Kubernetes["cluster"] = cluster
			docker := cfg.Docker["docker"]
			docker.Hidden = true
			cfg.Docker["docker"] = docker
		}
		for _, screen := range []ui.Screen{ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker, ui.ScreenWorkspace} {
			picker := interactiveBrowserPicker(cfg, cfg, screen, ui.Draft{})
			var selected ui.Choice
			model := ui.NewAppModel(ui.AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: screen, Pickers: map[ui.Screen]ui.Picker{screen: picker}, Flow: func(choice ui.Choice, _ ui.Draft) ui.Transition {
				selected = choice
				return ui.Transition{Picker: ui.Picker{Screen: ui.ScreenConfirm}}
			}})
			if hidden && screen != ui.ScreenWorkspace {
				next, _ := model.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
				model = next.(ui.AppModel)
			}
			for _, key := range []tea.KeyPressMsg{{Code: 'x', Text: "x"}, {Code: tea.KeyDelete}} {
				next, _ := model.Update(key)
				model = next.(ui.AppModel)
				if selected.Action != "" {
					t.Fatalf("unmodified key deleted %s", screen)
				}
			}
			next, _ := model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
			model = next.(ui.AppModel)
			if selected.Action != "remove-entity" || model.CurrentScreen() != ui.ScreenConfirm {
				t.Fatalf("Ctrl+D on %s hidden=%t: %#v", screen, hidden, selected)
			}
		}
	}
}

func TestWorkspaceListIsUnfilteredAndMarksOnlyCompleteMatches(t *testing.T) {
	cfg := visibilityConfig()
	cfg.Docker["other"] = config.Docker{Context: "other"}
	cfg.Destinations["other"] = config.Destination{Docker: "other"}
	draft := ui.Draft{ui.ScreenIdentity: "identity", ui.ScreenProject: "project", ui.ScreenKubernetes: "cluster", ui.ScreenDocker: "docker"}
	for _, match := range []bool{true, false} {
		if !match {
			draft[ui.ScreenDocker] = "other"
		}
		picker := interactiveBrowserPicker(cfg, cfg, ui.ScreenWorkspace, draft)
		if len(picker.Options) != 2 || picker.Scoped {
			t.Fatalf("workspace list filtered: %#v", picker)
		}
		for _, option := range picker.Options {
			want := option.Name == "workspace" && match
			if option.MatchesSelection != want {
				t.Fatalf("partial match for %s: %t", option.Name, option.MatchesSelection)
			}
		}
	}
}
