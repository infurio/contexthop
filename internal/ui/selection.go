package ui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
)

func isBrowserAction(option Option) bool {
	return strings.HasPrefix(option.Name, "\x00__") && option.Name != "\x00__contexthop_no_project__" && option.Name != "\x00__contexthop_no_kubernetes__"
}

func (m AppModel) stageResource(option Option) (tea.Model, tea.Cmd) {
	m.identityRecovery = nil
	if isBrowserAction(option) {
		return m.selectPickerOption(option, "")
	}
	screen := m.current().screen
	if identity := selectionIdentity(option, m.draft[ScreenIdentity]); identity != "" {
		option.Selection = cloneDraft(option.Selection)
		option.Selection[ScreenIdentity] = identity
	}
	if screen == ScreenProject && m.draft[screen] != option.Name && len(option.IdentityChoices) > 1 && selectionIdentity(option, m.draft[ScreenIdentity]) == "" {
		m.chooseResourceIdentity(option, ScreenProject)
		return m, nil
	}
	if screen == ScreenKubernetes && m.draft[screen] != option.Name && option.RequiresIdentity && selectionIdentity(option, m.draft[ScreenIdentity]) == "" {
		m.chooseResourceIdentity(option, ScreenKubernetes)
		return m, nil
	}
	delete(m.draft, ScreenShellADCOverride)
	if screen != ScreenWorkspace {
		delete(m.draft, ScreenWorkspace)
	}
	if m.draft[screen] == option.Name || screen == ScreenWorkspace && option.MatchesSelection || strings.HasPrefix(option.Name, "\x00__contexthop_no_") {
		delete(m.draft, screen)
		m.clearDownstreamResourceDraft(screen)
	} else {
		m.clearDownstreamResourceDraft(screen)
		if screen != ScreenWorkspace {
			delete(m.draft, ScreenWorkspace)
		}
		m.draft[screen] = option.Name
		if screen == ScreenWorkspace {
			m.draft[ScreenWorkspaceSource] = option.Name
		}
		for key, value := range option.Selection {
			if value == "" {
				delete(m.draft, key)
			} else {
				m.draft[key] = value
			}
		}
	}
	m.recordWorkspaceStage()
	if picker, ok := m.browserPicker(screen); ok {
		frame := &m.stack[len(m.stack)-1]
		filter, searching := frame.filter, frame.searching
		m.replaceBrowserRoot(picker)
		frame = &m.stack[len(m.stack)-1]
		frame.filter, frame.searching = filter, searching
		frame.focusOption(option.Name)
	}
	return m, nil
}

func (m AppModel) toggleHelp() (tea.Model, tea.Cmd) {
	m.helpVisible = !m.helpVisible
	m.helpOffset = 0
	return m, nil
}
func (m AppModel) hasStagedSelection() bool {
	return m.draft.ContextSelection().HasResources()
}

func (m AppModel) enterResource(option Option) (tea.Model, tea.Cmd) {
	return m.useNextShell("launch-shell")
}

const screenResourceIdentity Screen = "select-resource-identity"
const recoverResourceIdentity = "recover-resource-identity"

type identityRecovery struct {
	awaitingSetup  bool
	resource       Option
	screen, target Screen
}

func (m *AppModel) chooseResourceIdentity(resource Option, target Screen) {
	options := clonePicker(Picker{Options: resource.IdentityChoices}).Options
	for index := range options {
		selection := cloneDraft(resource.Selection)
		selection[ScreenIdentity] = options[index].Name
		selection[ScreenWorkspace] = ""
		selection[m.current().screen] = resource.Name
		if m.current().screen == ScreenProject {
			selection[ScreenKubernetes] = ""
		}
		options[index].Selection = selection
	}
	label := firstNonEmptyUI(resource.KubernetesCluster, resource.KubernetesContext, resource.ProjectID, resource.Label, resource.Name)
	description := "Select the identity to use with " + label + "."
	if len(options) == 0 {
		description = "No eligible identities are available for " + label + ". Set up discovery to verify access, then resume this selection."
		options = []Option{{Name: recoverResourceIdentity, Label: "Set up identity and discover", Selection: Draft{}, WorkLabel: "Preparing identity recovery"}}

	}
	m.push(Picker{
		identityResource: resource, Screen: screenResourceIdentity, Title: "Choose an identity",
		Description: description, DisableEnter: len(options) == 0,
		Options: options, Focus: m.draft[ScreenIdentity], selectionTarget: target,
	}, m.draft)
}

func eligibleSelectionIdentity(options []Option, name string) bool {
	if name == "" {
		return false
	}
	for _, option := range options {
		if option.Name == name {
			return true
		}
	}
	return false
}

// Resume only after a workflow returns to a browser with newly eligible identities.
func (m AppModel) resumeIdentityRecovery(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	pending := m.identityRecovery
	if cmd != nil || pending == nil || len(m.stack) == 0 || !m.current().picker.ResourceBrowser {
		return m, cmd
	}
	picker, ok := m.browserPicker(pending.screen)
	if !ok {
		return m, cmd
	}
	for _, option := range picker.Options {
		if option.Name == pending.resource.Name && len(option.IdentityChoices) > 0 {
			picker.Focus = option.Name
			m.replaceBrowserRoot(picker)
			m.identityRecovery = nil
			if selectionIdentity(option, m.draft[ScreenIdentity]) != "" {
				updated, nextCmd := m.stageResource(option)
				m = updated.(AppModel)
				if pending.target != pending.screen {
					m.switchResourceTab(pending.target)
				}
				return m, nextCmd
			}
			m.chooseResourceIdentity(option, pending.target)
			return m, cmd
		}
	}
	if pending.awaitingSetup && m.hasRecoveryIdentity() {
		next := *pending
		next.awaitingSetup = false
		m.identityRecovery = &next
		m.replaceBrowserRoot(picker)
		return m.selectPickerOption(pending.resource, "discover-resources")
	}
	return m, cmd
}

func (m AppModel) hasRecoveryIdentity() bool {
	picker, ok := m.browserPicker(ScreenIdentity)
	if ok {
		for _, option := range picker.Options {
			if option.Identity && !isBrowserAction(option) {
				return true
			}
		}
	}
	return false
}

// Row builders supply dependency defaults; the UI only arbitrates an explicit
// current selection against those defaults and a sole eligible account.
func selectionIdentity(option Option, current string) string {
	if eligibleSelectionIdentity(option.IdentityChoices, current) {
		return current
	}
	if chosen := option.Selection[ScreenIdentity]; chosen != "" && (len(option.IdentityChoices) == 0 || eligibleSelectionIdentity(option.IdentityChoices, chosen)) {
		return chosen
	}
	if len(option.IdentityChoices) == 1 {
		return option.IdentityChoices[0].Name
	}
	return ""
}
