package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/ui"
)

func TestCtrlDRequiresDeliberateConfirmationOnEveryTab(t *testing.T) {
	for _, screen := range []ui.Screen{ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker, ui.ScreenWorkspace} {
		t.Run(string(screen), func(t *testing.T) {
			t.Setenv("PATH", "")
			cfg := config.New()
			cfg.Identities["target"] = config.Identity{Provider: "gcp", Account: "test@example.com", CloudSDKConfig: t.TempDir()}
			cfg.Projects["target"] = config.Project{Provider: "gcp", ProjectID: "test-project"}
			cfg.Kubernetes["target"] = config.Kubernetes{Type: "kubeconfig", Context: "test-context", Kubeconfig: filepath.Join(t.TempDir(), "config")}
			cfg.Docker["target"] = config.Docker{Context: "test-docker"}
			cfg.Docker["workspace-support"] = config.Docker{Context: "workspace-docker"}
			cfg.Destinations["target"] = config.Destination{Docker: "workspace-support"}
			path := filepath.Join(t.TempDir(), "config.yaml")
			t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
			if err := config.Write(path, cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			c := newApplicationController(path, cfg, recency.History{}, nil)
			m := ui.NewAppModel(ui.AppOptions{StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept})
			resized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m = resized.(ui.AppModel)
			unchanged := func() {
				t.Helper()
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("configuration changed before explicit deletion", err)
				}
			}
			remove := tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
			for _, cancel := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: tea.KeyEnter}} {
				m = releaseKey(m, remove)
				if m.CurrentScreen() != ui.ScreenConfirm {
					t.Fatal("Ctrl+D bypassed confirmation", m.CurrentScreen())
				}
				unchanged()
				m = releaseKey(m, remove) // A repeated shortcut must not approve deletion.
				m = releaseKey(m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
				unchanged()
				m = releaseKey(m, cancel)
				unchanged()
				if m.CurrentScreen() != screen {
					t.Fatal("cancel did not restore originating tab", m.CurrentScreen())
				}
			}
			m = releaseKey(m, remove)
			m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyHome})
			m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
			after, err := os.ReadFile(path)
			if err != nil || bytes.Equal(before, after) {
				t.Fatal("explicit deletion was not saved", err)
			}
			if m.CurrentScreen() != screen {
				t.Fatal("delete did not return to originating tab")
			}
		})
	}
}

func TestCancelDeletionPreservesADCChoice(t *testing.T) {
	for _, override := range []string{"identity", "off", ""} {
		for _, cancel := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: tea.KeyEnter}} {
			t.Run(override+"/"+cancel.String(), func(t *testing.T) {
				t.Setenv("PATH", "")
				t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
				cfg := previewTestConfig()
				path := filepath.Join(t.TempDir(), "config.yaml")
				if err := config.Write(path, cfg); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				c := newApplicationController(path, cfg, recency.History{}, nil)
				initial := ui.Draft{ui.ScreenIdentity: "alice", ui.ScreenWorkspaceSource: "saved", ui.ScreenDocker: "local"}
				if override != "" {
					initial[ui.ScreenShellADCOverride] = override
				}
				var preview ui.LaunchPreview
				var observed ui.Draft
				m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenDocker, ResourceBrowser: true, ComposeSelection: true, InitialDraft: initial, Pickers: c.Pickers(), BrowserPicker: c.Browser, Flow: c.Prepare, AcceptResult: c.Accept,
					LaunchPreview: func(screen ui.Screen, option ui.Option, draft ui.Draft) ui.LaunchPreview {
						observed = cloneApplicationDraft(draft)
						preview = c.LaunchPreview(screen, option, draft)
						return preview
					}})
				m.View()
				expected := preview.Draft[ui.ScreenShellADCOverride]
				expectedFields := append([]ui.PickerField(nil), preview.Fields...)
				m = releaseKey(m, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
				if m.CurrentScreen() != ui.ScreenConfirm {
					t.Fatal("missing deletion confirmation")
				}
				m = releaseKey(m, cancel)
				m.View()
				if m.CurrentScreen() != ui.ScreenDocker || observed[ui.ScreenShellADCOverride] != override || preview.Draft[ui.ScreenShellADCOverride] != expected {
					t.Fatal("cancel changed ADC override", override, observed, preview)
				}
				if !reflect.DeepEqual(preview.Fields, expectedFields) {
					t.Fatal("cancel changed effective Selected preview", expectedFields, preview.Fields)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("cancel changed saved configuration", err)
				}
			})
		}
	}
}
