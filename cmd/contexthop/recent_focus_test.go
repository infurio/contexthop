package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/testenv"
	"github.com/infurio/contexthop/internal/ui"
	"reflect"
	"testing"
	"time"
)

func TestStartupPickerFocusRestoresLastSuccessfulSelection(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	recent := map[string]string{"workspace": "Payments Dev", "identity": "Personal", "project": "acme-staging", "kubernetes": "payments-staging", "docker": "OrbStack"}
	if err := recency.Record(recent); err != nil {
		t.Fatal(err)
	}
	history := recency.Load()
	for kind := range recent {
		history.LastUsed[kind]["deleted-resource"] = time.Now().Add(time.Hour)
	}
	c := newApplicationController(env.Catalog, cfg, history, nil)
	baseline := newApplicationController(env.Catalog, cfg, recency.History{}, nil).Pickers()
	for screen, picker := range c.Pickers() {
		want, ok := recent[picker.Dimension]
		if !ok {
			continue
		}
		if picker.Focus != want {
			t.Fatalf("%s focus = %q, want %q", screen, picker.Focus, want)
		}
		if !reflect.DeepEqual(picker.Options, baseline[screen].Options) {
			t.Fatal("history reordered or changed options")
		}
		var highlighted string
		model := ui.NewAppModel(ui.AppOptions{StartScreen: screen, ResourceBrowser: true, ComposeSelection: true,
			Pickers: c.Pickers(), BrowserPicker: c.Browser,
			LaunchPreview: func(screen ui.Screen, option ui.Option, draft ui.Draft) ui.LaunchPreview {
				highlighted = option.Name
				return ui.LaunchPreview{Available: true, Draft: ui.Draft{screen: option.Name}}
			},
		})
		model.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
		if highlighted != want {
			t.Fatalf("%s opened on %q, want %q", screen, highlighted, want)
		}
		if got := c.Browser(screen, ui.Draft{}).Focus; got != "" {
			t.Fatal("refresh forced the startup highlight again")
		}
	}
	hidden := cfg.Docker["OrbStack"]
	hidden.Hidden = true
	cfg.Docker["OrbStack"] = hidden
	c = newApplicationController(env.Catalog, cfg, history, nil)
	if got := c.Pickers()[ui.ScreenDocker].Focus; got != "" {
		t.Fatalf("hidden or deleted resource focused: %q", got)
	}
}
