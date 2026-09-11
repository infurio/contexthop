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

func TestTagRenameValidationCancelSaveAndReload(t *testing.T) {
	t.Setenv("PATH", "")
	cfg := config.New()
	cfg.Tags["old"] = config.Tag{Color: "#123456"}
	cfg.Tags["taken"] = config.Tag{Color: "#abcdef"}
	cfg.Docker["one"] = config.Docker{Context: "fictional-one", LabelSet: config.LabelSet{Tags: []string{"old"}}}
	cfg.Docker["two"] = config.Docker{Context: "fictional-two", LabelSet: config.LabelSet{Tags: []string{"old"}}}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	c := newApplicationController(path, cfg, recency.History{}, nil)
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenDocker, ResourceBrowser: true, ComposeSelection: true, BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept, Pickers: map[ui.Screen]ui.Picker{ui.ScreenDocker: c.Browser(ui.ScreenDocker, ui.Draft{})}})
	press := func(msg tea.Msg) { next, _ := m.Update(msg); m = next.(ui.AppModel) }
	key := func(code rune) { press(tea.KeyPressMsg{Code: code, Text: string(code)}) }
	press(tea.WindowSizeMsg{Width: 118, Height: 28})
	key('t')
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.View().Content, "Edit tag name") {
		t.Fatal("rename action missing")
	}
	press(tea.KeyPressMsg{Code: tea.KeyDown})
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.CurrentScreen() != screenTagRename {
		t.Fatal("rename input missing")
	}
	press(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	for _, r := range "taken" {
		key(r)
	}
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.CurrentScreen() != screenTagRename || !strings.Contains(m.View().Content, "already exists") {
		t.Fatal("collision not rejected")
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	saved, _ := config.Load(path)
	if saved.Tags["old"].Color != "#123456" {
		t.Fatal("cancel changed saved tag")
	}
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	press(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	for _, r := range "renamed" {
		key(r)
	}
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.CurrentScreen() != ui.ScreenDocker || m.StackDepth() != 1 {
		t.Fatal("rename lost originating tab", m.CurrentScreen(), m.View().Content)
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tags["renamed"].Color != "#123456" || saved.Docker["one"].Tags[0] != "renamed" || saved.Docker["two"].Tags[0] != "renamed" {
		t.Fatal("rename not persisted globally")
	}
	key('t')
	if strings.Contains(m.View().Content, " old") || !strings.Contains(m.View().Content, "renamed") {
		t.Fatal("tag picker stale", m.View().Content)
	}
}
