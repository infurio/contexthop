package ui

import (
	"testing"
)

func workspaceCursorModel() AppModel {
	pickers := map[Screen]Picker{}
	draft := Draft{}
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker} {
		draft[screen] = "alpha"
		pickers[screen] = Picker{Screen: screen, ResourceBrowser: true, Dimension: string(screen), ScopeLabel: "same scope",
			Options: []Option{{Name: "alpha", Identity: true, Project: true, Kubernetes: true, Docker: true}, {Name: "beta", Identity: true, Project: true, Kubernetes: true, Docker: true}}}
	}
	pickers[ScreenWorkspace] = Picker{Screen: ScreenWorkspace, ResourceBrowser: true, Dimension: "workspace",
		Options: []Option{{Name: "first"}, {Name: "second"}}}
	return NewAppModel(AppOptions{StartScreen: ScreenWorkspace, ComposeSelection: true, ResourceBrowser: true,
		InitialDraft: draft, Pickers: pickers,
		LaunchPreview: func(screen Screen, option Option, draft Draft) LaunchPreview {
			if screen == ScreenWorkspace {
				draft = Draft{ScreenWorkspace: option.Name, ScreenIdentity: "beta", ScreenProject: "beta", ScreenKubernetes: "beta", ScreenDocker: "beta"}
			}
			return LaunchPreview{Available: true, Draft: draft}
		}})
}

func focusedResource(m AppModel) string {
	f := m.current()
	rows := filteredPickerOptions(f.picker, f.filter)
	if len(rows) == 0 {
		return ""
	}
	return rows[f.cursor].Name
}

func TestWorkspaceStageOverridesCachedEntityCursorsOnce(t *testing.T) {
	for _, filter := range []string{"", "alpha", "a"} {
		t.Run("filter="+filter, func(t *testing.T) {
			m := workspaceCursorModel()
			screens := []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker}
			for _, screen := range screens {
				m.switchResourceTab(screen)
				f := &m.stack[len(m.stack)-1]
				f.filter = filter
				f.focusOption("alpha")
				f.picker.Description = "Saved operation feedback"
			}
			m.switchResourceTab(ScreenWorkspace)
			m, _ = updateApp(m, textKey(" "))
			for _, screen := range screens {
				m.switchResourceTab(screen)
				if m.draft[screen] != "beta" || focusedResource(m) != "beta" || m.current().cursorKey != "beta" {
					t.Fatalf("%s did not focus workspace selection: %s, draft=%v", screen, focusedResource(m), m.draft)
				}
				wantFilter := filter
				if filter == "alpha" {
					wantFilter = ""
				}
				if m.current().filter != wantFilter || m.current().picker.Description != "Saved operation feedback" {
					t.Fatal("lost compatible filter or operation feedback", m.current())
				}
				// Moving away from the staged row is still valid browsing state.
				m.stack[len(m.stack)-1].focusOption("alpha")
			}
			m.switchResourceTab(ScreenWorkspace)
			for _, screen := range screens {
				m.switchResourceTab(screen)
				if focusedResource(m) != "alpha" {
					t.Fatal("ordinary tab return forced staged focus again", screen)
				}
			}
			// A different workspace using the same entities must focus them again.
			m.switchResourceTab(ScreenWorkspace)
			m.stack[len(m.stack)-1].focusOption("second")
			m, _ = updateApp(m, textKey(" "))
			for _, screen := range screens {
				m.switchResourceTab(screen)
				if focusedResource(m) != "beta" {
					t.Fatal("restaging a workspace did not refresh focus", screen)
				}
			}
		})
	}
}

func TestWorkspaceIdentityChoiceRefreshesEntityFocus(t *testing.T) {
	m := workspaceCursorModel()
	m.switchResourceTab(ScreenIdentity)
	m.stack[len(m.stack)-1].focusOption("alpha")
	m.switchResourceTab(ScreenWorkspace)
	original := m.launchPreview
	m.launchPreview = func(screen Screen, option Option, draft Draft) LaunchPreview {
		p := original(screen, option, draft)
		p.NeedsIdentity = true
		p.IdentityChoices = []Option{{Name: "alpha"}, {Name: "beta"}}
		delete(p.Draft, ScreenIdentity)
		return p
	}
	m, _ = updateApp(m, textKey(" "))
	if m.CurrentScreen() != "next-shell-identity" {
		t.Fatal("identity choice not opened")
	}
	next, _ := m.selectPickerOption(Option{Name: "beta"}, "")
	m = next.(AppModel)
	m.switchResourceTab(ScreenIdentity)
	if focusedResource(m) != "beta" {
		t.Fatal("workspace identity choice did not replace cached focus", focusedResource(m))
	}
}
