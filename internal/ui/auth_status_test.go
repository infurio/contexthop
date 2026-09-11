package ui

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

func TestBackgroundAuthRefreshPreservesIdentityNavigation(t *testing.T) {
	picker := Picker{Screen: ScreenIdentity, Dimension: "identity", ResourceBrowser: true, ModalActions: true, Options: []Option{
		{Name: "first", Identity: true, AuthStatus: "Not checked"},
		{Name: "second", Identity: true, AuthStatus: "Not checked"},
	}}
	checked := false
	model := NewAppModel(AppOptions{StartScreen: ScreenIdentity, ResourceBrowser: true, Pickers: map[Screen]Picker{ScreenIdentity: picker}, RefreshIdentityAuth: func() { checked = true }, BrowserPicker: func(Screen, Draft) Picker {
		next := clonePicker(picker)
		if checked {
			next.Options[1].AuthStatus = "Authenticated"
		}
		return next
	}})
	model, _ = updateApp(model, specialKey(tea.KeyDown))
	command := model.checkIdentityAuth()
	if checked || model.working {
		t.Fatal("auth check ran on UI thread")
	}
	message := command()
	updated, next := model.Update(message)
	model = updated.(AppModel)
	if next == nil || model.current().cursor != 1 || model.current().picker.Options[1].AuthStatus != "Authenticated" || model.StackDepth() != 1 {
		t.Fatal("refresh lost navigation or auth status")
	}
}
