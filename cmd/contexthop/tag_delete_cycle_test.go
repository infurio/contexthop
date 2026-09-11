package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/ui"
	"path/filepath"
	"strings"
	"testing"
)

func TestTagDeletionConfirmCancelSaveAndReturn(t *testing.T) {
	t.Setenv("PATH", "")
	cfg := config.New()
	cfg.Tags["obsolete"] = config.Tag{Color: "#123456"}
	cfg.Docker["one"] = config.Docker{Context: "fictional-one", LabelSet: config.LabelSet{Tags: []string{"obsolete"}}}
	cfg.Docker["two"] = config.Docker{Context: "fictional-two", LabelSet: config.LabelSet{Tags: []string{"obsolete"}}}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	c := newApplicationController(path, cfg, recency.History{}, nil)
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenDocker, ResourceBrowser: true, ComposeSelection: true, BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept, Pickers: map[ui.Screen]ui.Picker{ui.ScreenDocker: c.Browser(ui.ScreenDocker, ui.Draft{})}})
	press := func(msg tea.Msg) { next, _ := m.Update(msg); m = next.(ui.AppModel) }
	press(tea.WindowSizeMsg{Width: 118, Height: 28})
	press(tea.KeyPressMsg{Code: 't', Text: "t"})
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	press(tea.KeyPressMsg{Code: tea.KeyEnd})
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.CurrentScreen() != ui.ScreenConfirm || !strings.Contains(m.View().Content, "obsolete") {
		t.Fatal("missing deletion review", m.View().Content)
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	saved, _ := config.Load(path)
	if len(saved.Tags) != 1 || m.CurrentScreen() != screenLabelAction {
		t.Fatal("cancel lost tag or origin")
	}
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	press(tea.KeyPressMsg{Code: tea.KeyHome}) // Explicitly choose deletion; Cancel is the default.
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.CurrentScreen() != ui.ScreenDocker || m.StackDepth() != 1 {
		t.Fatal("delete did not return to Docker", m.CurrentScreen(), m.View().Content)
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Tags) != 0 || len(saved.Docker["one"].Tags) != 0 || len(saved.Docker["two"].Tags) != 0 {
		t.Fatal("delete did not persist everywhere", saved)
	}
	press(tea.KeyPressMsg{Code: 't', Text: "t"})
	if strings.Contains(m.View().Content, "obsolete") {
		t.Fatal("deleted tag remains in picker", m.View().Content)
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.CurrentScreen() != ui.ScreenDocker {
		t.Fatal("empty tag picker did not cancel")
	}
}
