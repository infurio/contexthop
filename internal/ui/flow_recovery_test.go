package ui

import (
	tea "charm.land/bubbletea/v2"
	"reflect"
	"testing"
)

func TestBackspaceDoesNotClearBrowserSelection(t *testing.T) {
	p := Picker{Screen: ScreenProject, ResourceBrowser: true, ModalActions: true}
	before := Draft{ScreenIdentity: "a", ScreenProject: "p", ScreenWorkspaceSource: "w"}
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject, InitialDraft: before, Pickers: map[Screen]Picker{ScreenProject: p}})
	m, _ = updateApp(m, specialKey(tea.KeyBackspace))
	if !reflect.DeepEqual(m.draft, before) {
		t.Fatal("Backspace changed selection", m.draft)
	}
}

func TestIdentityChooserCancelPreservesADCAndCommitClearsIt(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{textKey(" ")} {
		p := Picker{Screen: ScreenProject, ResourceBrowser: true, ModalActions: true, Options: []Option{{Name: "new", Project: true, IdentityChoices: []Option{{Name: "a"}, {Name: "b"}}}}}
		before := Draft{ScreenIdentity: "old", ScreenShellADCOverride: "off"}
		m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenProject, InitialDraft: before, Pickers: map[Screen]Picker{ScreenProject: p, ScreenKubernetes: {Screen: ScreenKubernetes, ResourceBrowser: true}}})
		m, _ = updateApp(m, key)
		m, _ = updateApp(m, specialKey(tea.KeyEscape))
		if !reflect.DeepEqual(m.draft, before) {
			t.Fatal("cancel changed draft", key, m.draft)
		}
		m, _ = updateApp(m, key)
		m, _ = updateApp(m, specialKey(tea.KeyEnter))
		if m.draft[ScreenIdentity] != "a" || m.draft[ScreenProject] != "new" || m.draft[ScreenShellADCOverride] != "" {
			t.Fatal("commit did not update selection", m.draft)
		}
	}
}

func TestMissingIdentityRecoveryResumesTarget(t *testing.T) {
	verified := false
	browser := func(screen Screen, draft Draft) Picker {
		option := Option{Name: "cluster", Kubernetes: true, RequiresIdentity: true}
		if verified {
			option.IdentityChoices = []Option{{Name: "account"}}
		}
		return Picker{Screen: screen, ResourceBrowser: true, ModalActions: true, Options: []Option{option}}
	}
	before := Draft{ScreenDocker: "local"}
	flow := func(choice Choice, draft Draft) Transition {
		if choice.Action == "discover-resources" {
			if choice.Screen != ScreenKubernetes || choice.Option.Name != "cluster" {
				t.Fatal("lost discovery target", choice)
			}
			return Transition{Picker: Picker{Screen: "discovery", HideSearch: true, Options: []Option{{Name: "scan"}}}}
		}
		verified = true
		return Transition{ResetNavigation: true, Picker: browser(ScreenKubernetes, draft)}
	}
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenKubernetes, InitialDraft: before, Pickers: map[Screen]Picker{ScreenKubernetes: browser(ScreenKubernetes, before)}, BrowserPicker: browser, Flow: flow})
	m, _ = updateApp(m, textKey(" "))
	if m.current().picker.DisableEnter || m.current().picker.Options[0].Name != recoverResourceIdentity {
		t.Fatal("recovery unavailable")
	}
	m, _ = updateApp(m, specialKey(tea.KeyEnter))
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if !reflect.DeepEqual(m.draft, before) {
		t.Fatal("cancel discovery changed selection")
	}
	m, _ = updateApp(m, textKey(" "))
	m, _ = updateApp(m, specialKey(tea.KeyEnter))
	m, _ = updateApp(m, specialKey(tea.KeyEnter))
	if m.CurrentScreen() != ScreenKubernetes {
		t.Fatal("sole recovered identity should resolve automatically", m.CurrentScreen())
	}
	if m.draft[ScreenKubernetes] != "cluster" || m.draft[ScreenIdentity] != "account" || m.draft[ScreenDocker] != "local" {
		t.Fatal("wrong resumed selection", m.draft)
	}
}

func TestIdentitySetupContinuesDiscoveryForOriginalTarget(t *testing.T) {
	hasIdentity := false
	browser := func(screen Screen, draft Draft) Picker {
		p := Picker{Screen: screen, ResourceBrowser: true, ModalActions: true}
		if screen == ScreenIdentity {
			if hasIdentity {
				p.Options = []Option{{Name: "account", Identity: true}}
			}
		} else {
			p.Options = []Option{{Name: "cluster", Kubernetes: true, RequiresIdentity: true}}
		}
		return p
	}
	calls := 0
	flow := func(choice Choice, draft Draft) Transition {
		if choice.Action == "discover-resources" {
			calls++
			if choice.Option.Name != "cluster" {
				t.Fatal("lost target")
			}
			return Transition{Picker: Picker{Screen: "setup-or-discover", HideSearch: true, Options: []Option{{Name: "add"}}}}
		}
		hasIdentity = true
		return Transition{ResetNavigation: true, Picker: browser(ScreenIdentity, draft)}
	}
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenKubernetes, Pickers: map[Screen]Picker{ScreenKubernetes: browser(ScreenKubernetes, nil)}, BrowserPicker: browser, Flow: flow})
	m, _ = updateApp(m, textKey(" "))
	for range 2 {
		m, _ = updateApp(m, specialKey(tea.KeyEnter))
	}
	if calls != 2 || m.CurrentScreen() != "setup-or-discover" {
		t.Fatal("setup did not continue to discovery", calls, m.CurrentScreen())
	}
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if m.identityRecovery != nil {
		t.Fatal("cancel left a pending recovery")
	}
}

func TestLaunchRecoveryDialogRetainsDraftAndBackNavigation(t *testing.T) {
	before := Draft{ScreenDocker: "local", ScreenShellADCOverride: "off"}
	dialog := Picker{Screen: "shell-review", HideSearch: true, Description: "Launch failed"}
	m := NewAppModel(AppOptions{ComposeSelection: true, ResourceBrowser: true, StartScreen: ScreenDocker, InitialDraft: before, InitialDialog: &dialog, Pickers: map[Screen]Picker{ScreenDocker: {Screen: ScreenDocker, ResourceBrowser: true}}})
	if m.CurrentScreen() != dialog.Screen {
		t.Fatal("missing recovery review")
	}
	m, _ = updateApp(m, specialKey(tea.KeyEscape))
	if m.CurrentScreen() != ScreenDocker || !reflect.DeepEqual(m.draft, before) {
		t.Fatal("failed launch lost browser selection")
	}
}
