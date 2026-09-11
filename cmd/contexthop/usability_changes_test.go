package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/ui"
	"reflect"
	"strings"
	"testing"
)

// Select by the visible label so menu ordering is free to improve.
func chooseVisibleOption(t *testing.T, m ui.AppModel, label string) ui.AppModel {
	t.Helper()
	m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyHome})
	for range 60 {
		if strings.Contains(ansi.Strip(m.View().Content), "> "+label) {
			return releaseKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	t.Fatalf("option %q unavailable: %s", label, m.View().Content)
	return m
}

func TestWorkspaceEditorShowsChangedValues(t *testing.T) {
	cfg := previewTestConfig()
	original := cfg.Destinations["saved"]
	changed := original
	changed.Docker = "remote"
	changed.ADC = ""
	picker := workspaceBuilderPicker(cfg, &workspaceEditorDraft{Name: "saved", Value: changed, Existing: true})
	for _, tc := range []struct{ name, old, new string }{{"workspace-set:docker", "None", "remote"}, {"workspace-adc", "Enabled", "Disabled"}} {
		found := false
		for _, o := range picker.Options {
			if o.Name == tc.name {
				found = true
				if !strings.HasSuffix(o.Label, " *") || !strings.Contains(o.Detail, " → ") || !strings.Contains(o.Summary, tc.old) || !strings.Contains(o.Summary, tc.new) {
					t.Fatal("change comparison missing", o)
				}
			}
		}
		if !found {
			t.Fatal(tc.name)
		}
	}
	m := releaseDialog(picker, 80, 24)
	m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyHome})
	for range 3 {
		m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	m = releaseKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	if !strings.Contains(ansi.Strip(m.View().Content), "Previously: None") || !strings.Contains(ansi.Strip(m.View().Content), "Now: remote") {
		t.Fatal("full changed-field details unavailable", m.View().Content)
	}
	if !reflect.DeepEqual(cfg.Destinations["saved"], original) {
		t.Fatal("review modified saved workspace")
	}
	unchanged := workspaceBuilderPicker(cfg, &workspaceEditorDraft{Name: "saved", Value: original, Existing: true})
	for _, o := range unchanged.Options {
		if strings.HasSuffix(o.Label, " *") {
			t.Fatal("unchanged field marked", o)
		}
	}
}

func TestCancelReviewRestoresFilteredBrowser(t *testing.T) {
	for _, cancel := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: tea.KeyEnter}} {
		c := controllerFixture(t)
		m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenProject, ResourceBrowser: true, ComposeSelection: true, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept, InitialDraft: ui.Draft{ui.ScreenIdentity: "work", ui.ScreenShellADCOverride: "off"}, LaunchPreview: c.LaunchPreview})
		n, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = n.(ui.AppModel)
		m = releaseKey(m, tea.KeyPressMsg{Code: 'b', Text: "b"}) // Include projects without an identity mapping.
		m = releaseKey(m, tea.KeyPressMsg{Code: '/', Text: "/"})
		for _, r := range "existing" {
			m = releaseKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
		m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		before := m.View().Content
		if !strings.Contains(ansi.Strip(before), "existing") {
			t.Fatal("filter not retained")
		}
		m = releaseKey(m, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
		if m.CurrentScreen() != ui.ScreenConfirm {
			t.Fatal("review not opened", m.View().Content)
		}
		m = releaseKey(m, cancel)
		if m.View().Content != before {
			t.Fatalf("cancel changed browser state:\nBEFORE\n%s\nAFTER\n%s", before, m.View().Content)
		}
	}
}
