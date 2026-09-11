package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/ui"
)

func TestIdentityBrowserProfileStartsWithHighlightedAccount(t *testing.T) {
	cfg := visibilityConfig()
	cfg.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	c := newApplicationController(path, cfg, recency.History{}, nil)
	pickers := c.Pickers()
	if _, exists := pickers[ui.ScreenCatalogEntity]; exists {
		t.Fatal("duplicate catalog browser remains registered")
	}
	p := pickers[ui.ScreenIdentity]
	p.Focus = "identity"
	pickers[ui.ScreenIdentity] = p
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenIdentity, ResourceBrowser: true, ComposeSelection: true, InitialDraft: ui.Draft{ui.ScreenIdentity: "other"}, Pickers: pickers, BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept})
	press := func(code rune, text string) {
		next, _ := m.Update(tea.KeyPressMsg{Code: code, Text: text})
		m = next.(ui.AppModel)
	}
	press('o', "o")
	m = chooseVisibleOption(t, m, "Browser profile")
	if m.CurrentScreen() != browserApp || m.StackDepth() != 2 || !strings.Contains(m.View().Content, "person@example.com") {
		t.Fatal("browser settings reselected the account or targeted the staged account", m.View().Content)
	}
	press(tea.KeyEscape, "")
	if m.CurrentScreen() != ui.ScreenIdentity || m.StackDepth() != 1 {
		t.Fatal("cancel did not return directly to Identities")
	}
	press('o', "o")
	m = chooseVisibleOption(t, m, "Browser profile")
	press(tea.KeyEnter, "")
	if m.CurrentScreen() != browserDirectory {
		t.Fatal("browser choice did not proceed directly to profile field")
	}
}

func TestLocalImportIsDirectOnOwningTabs(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))
	for _, screen := range []ui.Screen{ui.ScreenIdentity, ui.ScreenKubernetes, ui.ScreenDocker} {
		cfg := visibilityConfig()
		c := newApplicationController("", cfg, recency.History{}, nil)
		m := ui.NewAppModel(ui.AppOptions{StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare})
		key := 'l'
		next, _ := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		m = next.(ui.AppModel)
		if m.CurrentScreen() != "local-import-results" || m.StackDepth() != 2 {
			t.Fatal("import requires an intermediate menu", screen, m.CurrentScreen())
		}
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = next.(ui.AppModel)
		if m.CurrentScreen() != screen {
			t.Fatal("import cancel lost tab")
		}
	}
}
