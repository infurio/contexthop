package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/state"
	"github.com/infurio/contexthop/internal/ui"
)

// Exercise real controller pickers through the UI. Optional captures make the
// same scenarios available for visual review without using a user's catalog.
func TestUIWalkthrough(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))
	for _, scenario := range []string{"populated", "empty", "long-managed"} {
		empty := scenario == "empty"
		for _, height := range []int{12, 28} {
			for _, width := range []int{35, 80, 110, 180} {
				for _, screen := range []ui.Screen{ui.ScreenWorkspace, ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker} {
					name := fmt.Sprintf("%s-%dx%d-%s", screen, width, height, scenario)
					t.Run(name, func(t *testing.T) {
						cfg := visibilityConfig()
						if empty {
							cfg = config.New()
						}
						snapshot := state.Snapshot{Observed: state.Component{Kubernetes: "long-active-local-cluster-context"}}
						initialDraft := ui.Draft{}
						if scenario == "long-managed" {
							identity := cfg.Identities["identity"]
							identity.Account = strings.Repeat("long-account-", 8) + "@example.com"
							cfg.Identities["identity"] = identity
							docker := cfg.Docker["docker"]
							docker.Context = "desktop-local-context"
							initialDraft[ui.ScreenDocker] = "docker"
							cfg.Docker["docker"] = docker
							snapshot = state.Snapshot{Managed: true, Destination: strings.Repeat("long-workspace-", 10), LocalStatus: "MANAGED"}
						}
						dir := t.TempDir()
						path := filepath.Join(dir, "config.yaml")
						if err := config.Write(path, cfg); err != nil {
							t.Fatal(err)
						}
						t.Setenv("CONTEXTHOP_CACHE_DIR", dir)
						newModel := func() ui.AppModel {
							c := newApplicationController(path, cfg, recency.History{}, nil)
							m := ui.NewAppModel(ui.AppOptions{InitialDraft: initialDraft, Snapshot: snapshot, CanApplyShell: snapshot.Managed, StartScreen: screen, ResourceBrowser: true, ComposeSelection: true, Pickers: interactivePickers(cfg, cfg, nil), Flow: c.Prepare, AcceptResult: c.Accept, BrowserPicker: c.Browser, LaunchPreview: c.LaunchPreview})
							updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
							return updated.(ui.AppModel)
						}
						press := func(m ui.AppModel, key string) ui.AppModel {
							msg := tea.KeyPressMsg{Text: key, Code: []rune(key)[0]}
							if key == "esc" {
								msg = tea.KeyPressMsg{Code: tea.KeyEscape}
							}
							updated, _ := m.Update(msg)
							return updated.(ui.AppModel)
						}
						check := func(m ui.AppModel, step string) {
							view := ansi.Strip(m.View().Content)
							lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
							if len(lines) > height {
								t.Fatalf("%s exceeds height: %s", step, view)
							}
							for _, line := range lines {
								if ansi.StringWidth(line) > width {
									t.Fatalf("%s exceeds width: %s", step, line)
								}
							}
							if step == "browse" && !strings.Contains(lines[0], "Selected") {
								t.Fatalf("missing common header: %s", view)
							}
							if step == "browse" && width >= 91 && height >= 18 && !strings.Contains(lines[0], map[bool]string{true: "MANAGED", false: "UNMANAGED"}[snapshot.Managed]) {
								t.Fatalf("active shell status clipped: %s", view)
							}
							if step == "search" && !strings.Contains(view, "▏") {
								t.Fatalf("search cursor is hidden: %s", view)
							}
							if step == "browse" && width >= 80 {
								fullWidth := false
								for _, line := range lines {
									if strings.HasSuffix(line, "┐") && ansi.StringWidth(line) == width-1 {
										fullWidth = true
									}
								}
								if !fullWidth {
									t.Fatalf("panel does not use full width: %s", view)
								}
								if scenario == "long-managed" && strings.Contains(lines[1], "docker: docker") {
									t.Fatalf("Docker header displays alias: %s", view)
								}
							}
							if capture := os.Getenv("CONTEXTHOP_UI_AUDIT_DIR"); capture != "" {
								if err := os.MkdirAll(capture, 0700); err != nil {
									t.Fatal(err)
								}
								if err := os.WriteFile(filepath.Join(capture, name+"-"+step+".txt"), []byte(view), 0600); err != nil {
									t.Fatal(err)
								}
							}
						}
						m := newModel()
						check(m, "browse")
						m = press(m, "/")
						m = press(m, strings.Repeat("long-search-", 20))
						check(m, "search")
						m = press(m, "esc")
						check(m, "search-cancel")
						m = press(m, "?")
						check(m, "help")
						m = press(m, "esc")
						if m.CurrentScreen() != screen {
							t.Fatal("help changed tab")
						}

						keys := []string{"o", "n", "d", "i", "r"}
						if !empty {
							keys = append(keys, "m", "t")
							if screen == ui.ScreenWorkspace {
								keys = append(keys, "e")
							}
						}
						for _, key := range keys {
							m = press(newModel(), key)
							check(m, key)
							if m.CurrentScreen() == screen {
								continue
							}
							m = press(m, "esc")
							check(m, key+"-cancel")
							if m.CurrentScreen() != screen {
								t.Fatalf("%s cancel returned to %s", key, m.CurrentScreen())
							}
						}
					})
				}
			}
		}
	}
}
