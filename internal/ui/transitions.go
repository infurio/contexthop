package ui

import (
	tea "charm.land/bubbletea/v2"
	"context"
)

func (m AppModel) selectPickerOption(selected Option, action string) (tea.Model, tea.Cmd) {
	if m.current().screen == screenOptions && action == "" {
		return m.chooseOption(selected.Name)
	}
	if m.current().screen == "next-shell-identity" && action == "" {
		picker := m.current().picker
		original := cloneDraft(m.draft)
		m.draft = cloneDraft(picker.previewDraft)
		m.draft[ScreenIdentity] = selected.Name
		m.stack = m.stack[:len(m.stack)-1]
		if picker.previewAction == "stage" {
			m.refreshStagedRow(m.current().cursorKey)
			return m, nil
		}
		if picker.previewAction == "save-selection-workspace" {
			request := m.draft
			m.draft = original
			return m.selectPickerOption(Option{FlowDraft: request}, picker.previewAction)
		}
		return m.selectPickerOption(Option{}, picker.previewAction)
	}

	if selected.OpenScreen != "" {
		m.pushConfigured(selected.OpenScreen)
		return m, nil
	}
	frame := &m.stack[len(m.stack)-1]
	if frame.screen == screenResourceIdentity && action == "" {
		if selected.Name == recoverResourceIdentity {
			resource, target := frame.picker.identityResource, frame.picker.selectionTarget
			m.stack = m.stack[:len(m.stack)-1]
			m.identityRecovery = &identityRecovery{resource: resource, screen: m.current().screen, target: target, awaitingSetup: !m.hasRecoveryIdentity()}
			return m.selectPickerOption(resource, "discover-resources")
		}
		delete(m.draft, ScreenShellADCOverride)
		target := frame.picker.selectionTarget
		for screen, value := range selected.Selection {
			if value == "" {
				delete(m.draft, screen)
			} else {
				m.draft[screen] = value
			}
		}
		m.stack = m.stack[:len(m.stack)-1]
		if picker, ok := m.browserPicker(target); ok {
			m.replaceBrowserRoot(picker)
		}
		return m, nil
	}
	frame.cursorKey = selected.Name
	before := cloneDraft(m.draft)
	if action == "" || !frame.picker.ResourceBrowser {
		if frame.picker.ResourceBrowser {
			m.clearDownstreamResourceDraft(frame.screen)
		}
		m.draft[frame.screen] = selected.Name
	}
	choice := Choice{Screen: frame.screen, Option: selected, Action: action, CanApplyShell: m.canApplyShell}
	flow := m.flow
	if frame.picker.Flow != nil {
		flow = frame.picker.Flow
	}
	requestDraft := cloneDraft(m.draft)
	for key, value := range selected.FlowDraft {
		requestDraft[key] = value
	}
	if m.asyncFlow && flow != nil {
		draft := requestDraft
		label := firstNonEmptyUI(selected.WorkLabel, selected.Label, selected.ProjectID, selected.IdentityAccount, selected.Name, "Updating resources")
		if action == "discover-resources" {
			label = "Preparing discovery options"
		}
		return m.startWork(label, func(ctx context.Context, report func(string)) Transition {
			choice.Context = ctx
			choice.ReportProgress = report
			return flow(choice, draft)
		}, choice, before, false)
	}
	transition := Transition{Complete: true}
	if flow != nil {
		transition = flow(choice, requestDraft)
	}
	return m.applyChoiceTransition(transition, choice, before)
}

func (m *AppModel) acceptTransition(transition Transition) {
	if transition.Result != nil && m.acceptResult != nil {
		m.acceptResult(transition.Result)
	}
	for screen, picker := range transition.Pickers {
		m.pickers[screen] = clonePicker(picker)
		picker.Screen = screen
		m.rememberResourceLabels(picker)
	}
}

func (m AppModel) applyChoiceTransition(transition Transition, choice Choice, before Draft) (tea.Model, tea.Cmd) {
	updated, cmd := m.applyTransition(transition, choice, before, false)
	return updated.(AppModel).resumeIdentityRecovery(cmd)
}

func (m AppModel) applyProcessTransition(transition Transition) (tea.Model, tea.Cmd) {
	updated, cmd := m.applyTransition(transition, Choice{}, nil, true)
	return updated.(AppModel).resumeIdentityRecovery(cmd)
}

// All workflow results share one acceptance and navigation path. A process
// continuation replaces its dialog; a new choice normally pushes a child page.
func (m AppModel) applyTransition(transition Transition, choice Choice, before Draft, continuation bool) (tea.Model, tea.Cmd) {
	m.acceptTransition(transition)
	if transition.ResetNavigation && transition.PreservePosition {
		for index := len(m.stack) - 2; index >= 0; index-- {
			if m.stack[index].picker.ResourceBrowser {
				m.draft = cloneDraft(m.stack[index+1].draftBefore)
				m.stack = m.stack[:index+1]
				m.identityRecovery = nil
				return m, nil
			}
		}
	}
	if transition.Dismiss && len(m.stack) > 1 {
		m.draft = cloneDraft(m.current().draftBefore)
		m.stack = m.stack[:len(m.stack)-1]
		if m.current().picker.ResourceBrowser {
			m.identityRecovery = nil
		}
		return m, nil
	}
	for screen, value := range transition.DraftUpdates {
		if value == "" {
			delete(m.draft, screen)
		} else {
			m.draft[screen] = value
		}
	}
	if transition.Process != nil && transition.Process.Command != nil {
		process := transition.Process
		return m, tea.ExecProcess(process.Command, func(err error) tea.Msg { return processFinishedMessage{process: process, err: err} })
	}
	if transition.Complete {
		if transition.CompletionChoice != nil {
			m.outcome = Outcome{Complete: true, Choice: *transition.CompletionChoice, Draft: cloneDraft(m.draft)}
			return m, tea.Quit
		}
		if !continuation {
			m.outcome = Outcome{Complete: true, Choice: choice, Draft: cloneDraft(m.draft)}
		}
		return m, tea.Quit
	}
	if transition.ResetNavigation {
		m.stack = nil
		m.push(transition.Picker, m.draft)
		return m, nil
	}
	if transition.ReturnToScreen != "" {
		for index := len(m.stack) - 2; index >= 0; index-- {
			if m.stack[index].screen == transition.ReturnToScreen {
				m.stack = m.stack[:index+1]
				break
			}
		}
		m.replaceCurrent(transition.Picker)
		return m, nil
	}
	if transition.ReturnToPrevious && len(m.stack) > 1 {
		m.stack = m.stack[:len(m.stack)-1]
		previous := m.current()
		if transition.PreservePosition && transition.Picker.ResourceBrowser {
			for _, frame := range m.stack {
				if frame.screen == transition.Picker.Screen && frame.picker.ResourceBrowser {
					previous = frame
					break
				}
			}
		}
		m.replaceCurrent(transition.Picker)
		if transition.PreservePosition {
			frame := &m.stack[len(m.stack)-1]
			frame.filter = previous.filter
			frame.searching = previous.searching
			frame.focusOption(previous.cursorKey)
			if transition.Picker.ResourceBrowser {
				root := *frame
				root.draftBefore = Draft{}
				m.stack = []navigationFrame{root}
			}
		}
		return m, nil
	}
	if transition.ReplaceCurrent {
		previous := m.current()
		if transition.Picker.ResourceBrowser {
			m.replaceBrowserRoot(transition.Picker)
		} else {
			m.replaceCurrent(transition.Picker)
		}
		if transition.PreservePosition {
			frame := &m.stack[len(m.stack)-1]
			frame.filter, frame.searching = previous.filter, previous.searching
			frame.focusOption(previous.cursorKey)
		}
		return m, nil
	}
	if continuation {
		if transition.Picker.Screen != "" {
			if m.current().picker.ResourceBrowser && !transition.Picker.ResourceBrowser {
				m.push(transition.Picker, m.draft)
			} else {
				m.replaceCurrent(transition.Picker)
			}
		}
	} else {
		m.push(transition.Picker, before)
	}
	return m, nil
}
