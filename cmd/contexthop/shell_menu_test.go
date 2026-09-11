package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func TestShellMenuLaunchesBothModesWithoutConfirmation(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "desktop-linux"}
	for _, action := range []string{"launch-shell", "apply-shell"} {
		flow := interactiveFlow(cfg, cfg, &catalogEditorState{})
		tr := flow(ui.Choice{Screen: screenShellMenu, Option: ui.Option{Name: action}, CanApplyShell: true}, ui.Draft{ui.ScreenDocker: "local"})
		if !tr.Complete || tr.CompletionChoice == nil || tr.CompletionChoice.Action != action {
			t.Fatalf("%s: %#v", action, tr)
		}
	}
	tr, _ := shellMenuFlow(cfg, ui.Choice{Screen: screenShellMenu, Option: ui.Option{Name: "apply-shell"}}, ui.Draft{ui.ScreenDocker: "local"})
	if tr.Complete {
		t.Fatal("current shell offered without integration")
	}
	p := shellMenuPicker(cfg, ui.Draft{ui.ScreenDocker: "local"}, false)
	for _, o := range p.Options {
		if o.Name == "apply-shell" {
			t.Fatal("current-shell action enabled")
		}
	}
	if len(p.ContextFields) != 4 || p.ContextFields[3].Value != "desktop-linux" {
		t.Fatal(p.ContextFields)
	}
}

func TestShellADCOverrideIsLaunchOnlyAndSupportsWorkspace(t *testing.T) {
	cfg := config.New()
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "me@example.com"}
	cfg.Destinations["w"] = config.Destination{Identity: "work", ADC: "identity"}
	draft := ui.Draft{ui.ScreenWorkspace: "w"}
	r, err := resolveShellSelection(cfg, draft)
	if err != nil || r.ADCMode != "identity" {
		t.Fatal(r, err)
	}
	draft[ui.ScreenShellADCOverride] = "off"
	r, err = resolveShellSelection(cfg, draft)
	if err != nil || r.ADCMode != "" {
		t.Fatal(r, err)
	}
	if cfg.Destinations["w"].ADC != "identity" {
		t.Fatal("workspace was modified")
	}
	draft = ui.Draft{ui.ScreenIdentity: "work", ui.ScreenWorkspaceSource: "w", ui.ScreenShellADCOverride: "off"}
	r, err = resolveShellSelection(cfg, draft)
	if err != nil || r.ADCMode != "" {
		t.Fatal(r, err)
	}
	draft[ui.ScreenShellADCOverride] = "identity"
	r, err = resolveShellSelection(cfg, draft)
	if err != nil || r.ADCMode != "identity" {
		t.Fatal(r, err)
	}
	draft[ui.ScreenShellADCOverride] = "invalid"
	if _, err = resolveShellSelection(cfg, draft); err == nil {
		t.Fatal("invalid override accepted")
	}
}

func TestShellMenuADCNavigationAndCancel(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "desktop-linux"}
	picker := resourceBrowserPicker(cfg, ui.ScreenDocker, dockerOptions(cfg), "")
	model := ui.NewAppModel(ui.AppOptions{ComposeSelection: true, ResourceBrowser: true, CanApplyShell: true, InitialDialog: func() *ui.Picker { p := shellMenuPicker(cfg, ui.Draft{ui.ScreenDocker: "local"}, true); return &p }(), StartScreen: ui.ScreenDocker, InitialDraft: ui.Draft{ui.ScreenDocker: "local"}, Pickers: map[ui.Screen]ui.Picker{ui.ScreenDocker: picker}, Flow: interactiveFlow(cfg, cfg, &catalogEditorState{})})
	press := func(k tea.KeyPressMsg) { next, _ := model.Update(k); model = next.(ui.AppModel) }
	if model.CurrentScreen() != screenShellMenu {
		t.Fatal(model.CurrentScreen())
	}
	press(tea.KeyPressMsg{Code: tea.KeyHome})
	press(tea.KeyPressMsg{Code: tea.KeyDown})
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.CurrentScreen() != screenShellADC {
		t.Fatal(model.CurrentScreen())
	}
	press(tea.KeyPressMsg{Code: tea.KeyEnd})
	press(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.CurrentScreen() != screenShellMenu || model.StackDepth() != 2 {
		t.Fatal("ADC did not return to Shell", model.CurrentScreen(), model.StackDepth())
	}
	if !strings.Contains(model.View().Content, "this launch") {
		t.Fatal("ADC override not displayed")
	}
	press(tea.KeyPressMsg{Code: tea.KeyEscape})
	if model.CurrentScreen() != ui.ScreenDocker {
		t.Fatal("cancel did not return to resource browser")
	}
}

func TestIdentityLessSelectionDefaultsADCOff(t *testing.T) {
	cfg := config.New()
	cfg.Docker["local"] = config.Docker{Context: "desktop-linux"}
	cfg.Kubernetes["local"] = config.Kubernetes{Type: "kubeconfig", Kubeconfig: "/tmp/config", Context: "local"}
	cfg.Identities["work"] = config.Identity{Provider: "gcp", Account: "me@example.com"}
	cfg.Destinations["with-adc"] = config.Destination{Identity: "work", Docker: "local", ADC: "identity"}
	cfg.Destinations["local"] = config.Destination{Docker: "local"}
	for _, draft := range []ui.Draft{
		{ui.ScreenDocker: "local", ui.ScreenWorkspaceSource: "with-adc"},
		{ui.ScreenDocker: "local", ui.ScreenShellADCOverride: "identity"},
		{ui.ScreenKubernetes: "local", ui.ScreenShellADCOverride: "identity"},
		{ui.ScreenWorkspace: "local", ui.ScreenShellADCOverride: "identity"},
	} {
		resolved, err := resolveShellSelection(cfg, draft)
		if err != nil || resolved.Identity != nil || resolved.ADCMode != "" {
			t.Fatal("identity-less launch must have ADC off", draft, resolved, err)
		}
		preview := nextShellPreview(cfg, "", ui.Option{}, draft)
		if preview.Error != "" {
			t.Fatal("identity-less preview failed", draft, preview.Error)
		}
		for _, field := range preview.Fields {
			if field.Label == "ADC" && field.Value != "off" {
				t.Fatal("preview ADC is enabled without an identity", draft, field)
			}
		}
	}
	if cfg.Destinations["with-adc"].ADC != "identity" {
		t.Fatal("saved workspace ADC was modified")
	}
	if _, err := resolveShellSelection(cfg, ui.Draft{ui.ScreenWorkspace: "missing"}); err == nil {
		t.Fatal("unknown workspace accepted")
	}
	editor := &catalogEditorState{}
	tr, handled := selectionWorkspaceFlow(cfg, cfg, ui.Choice{Action: "save-selection-workspace"},
		ui.Draft{ui.ScreenDocker: "local", ui.ScreenWorkspaceSource: "with-adc"}, editor)
	if !handled || editor.workspace == nil || editor.workspace.Value.ADC != "" || editor.workspace.Value.Identity != "" {
		t.Fatal("saved selection retained identity ADC", tr, editor.workspace)
	}
}
