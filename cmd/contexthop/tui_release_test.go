package main

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
	"strings"
	"testing"
)

func releaseDialog(p ui.Picker, width, height int) ui.AppModel {
	m := ui.NewAppModel(ui.AppOptions{StartScreen: ui.ScreenWorkspace, ResourceBrowser: true, ComposeSelection: true, Pickers: map[ui.Screen]ui.Picker{
		ui.ScreenWorkspace: {ResourceBrowser: true, Options: []ui.Option{{Name: "open", Label: "Open", OpenScreen: p.Screen}}}, p.Screen: p}})
	resized, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return releaseKey(resized.(ui.AppModel), tea.KeyPressMsg{Code: tea.KeyEnter})
}
func releaseKey(m ui.AppModel, key tea.KeyPressMsg) ui.AppModel {
	next, _ := m.Update(key)
	return next.(ui.AppModel)
}

func TestShortIdentityInputKeepsValidationVisible(t *testing.T) {
	p := catalogAddTransition(config.New(), "add-identity", nil, &catalogEditorState{}).Picker
	m := releaseDialog(p, 35, 12)
	m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Name", "▏", "A value is required.", "[enter]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q: %s", want, view)
		}
	}
	m = releaseKey(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if strings.Contains(ansi.Strip(m.View().Content), "A value is required.") {
		t.Fatal("correcting input retained validation error")
	}
}

func TestCatalogReviewAllDetailsAccessibleBeforeApply(t *testing.T) {
	cfg := config.New()
	cfg.Tags["shared"] = config.Tag{Color: "#abcdef"}
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("docker-%02d", i)
		cfg.Docker[name] = config.Docker{Context: name, LabelSet: config.LabelSet{Tags: []string{"shared"}}}
	}
	plan, err := catalog.PlanDeleteTag(cfg, "shared")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{35, 12}, {80, 24}, {180, 28}} {
		for _, blocked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/blocked=%v", size[0], size[1], blocked), func(t *testing.T) {
				review := plan
				if blocked {
					review.Problems = []string{"FINAL DEPENDENCY PROBLEM"}
				}
				p := catalogConfirmPicker(&catalogEditorState{plan: &review})
				m := releaseDialog(p, size[0], size[1])
				depth := m.StackDepth()
				if !strings.Contains(ansi.Strip(m.View().Content), "[i] Full details") {
					t.Fatal("full review shortcut hidden", m.View().Content)
				}
				m = releaseKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
				if m.StackDepth() != depth+1 {
					t.Fatal("full review did not open")
				}
				m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyEnd})
				last := "docker-19"
				if blocked {
					last = "FINAL DEPENDENCY PROBLEM"
				}
				if !strings.Contains(ansi.Strip(m.View().Content), last) {
					t.Fatal("cannot reach final review detail", m.View().Content)
				}
				m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
				if m.StackDepth() != depth+1 {
					t.Fatal("Enter applied from read-only review")
				}
				m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
				if m.CurrentScreen() != ui.ScreenConfirm || m.StackDepth() != depth {
					t.Fatal("closing details lost confirmation")
				}
			})
		}
	}
}

func TestDialogFilterKeepsQueryTailAndCursorVisible(t *testing.T) {
	p := ui.Picker{Screen: "test-chooser", Title: "Choose context", Options: []ui.Option{{Name: strings.Repeat("a", 90), Label: "Long context"}}}
	m := releaseDialog(p, 80, 24)
	for _, r := range strings.Repeat("a", 75) + "tail" {
		m = releaseKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "tail▏") {
		t.Fatal("query tail/caret hidden", m.View().Content)
	}
	m = releaseKey(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if !strings.Contains(ansi.Strip(m.View().Content), "tai▏") {
		t.Fatal("backspace not reflected at visible cursor")
	}
}
