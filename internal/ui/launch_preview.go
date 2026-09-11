package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/selection"
)

// LaunchPreview is a local, side-effect-free projection of the highlighted row
// combined with explicitly staged choices. Its draft is also the launch request.
type LaunchPreview struct {
	FieldStates     map[string]string // Header labels mapped to staged, highlighted, or empty.
	Draft           Draft
	Fields          []PickerField
	IdentityChoices []Option
	NeedsIdentity   bool
	Error           string
	Available       bool
}

func launchKeyHints(width int, canApply bool) []string {
	current := "Pin"
	if !canApply {
		current = "Set up shell"
	}
	commands := contextLaunchCommands()
	if width < 58 {
		return []string{commandHint(commands, "next-default", "", width), commandHint(commands, "next-launch", "", width), commandHint(commands, "next-apply", current, width)}
	}
	return []string{commandHint(commands, "next-default", "", width), commandHint(commands, "next-apply", current, width), commandHint(commands, "next-launch", "", width)}
}

func resourceTarget(screen Screen, option Option) (Screen, string) {
	return screen, option.Name
}

// A cursor can fill an unstaged component, but cannot replace staged values.
func (m AppModel) nextShell(frame navigationFrame) LaunchPreview {
	rows := filteredPickerOptions(frame.picker, frame.filter)
	screen, option := Screen(""), Option{}
	if len(rows) > 0 {
		candidate := rows[min(frame.cursor, len(rows)-1)]
		if !isBrowserAction(candidate) && candidate.OpenScreen == "" && m.draft[frame.screen] == "" && m.draft[ScreenWorkspace] == "" {
			screen, option = frame.screen, candidate
		}
	}
	preview := m.previewSelection(screen, option)
	// Browse-all and workspace rows can propose incompatible dependencies.
	// Keep the staged composition until Space explicitly chooses the other row.
	for _, key := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker} {
		if m.draft[key] != "" && preview.Draft[key] != m.draft[key] {
			preview = m.previewSelection("", Option{})
			break
		}
	}
	preview.FieldStates = map[string]string{}
	for _, field := range preview.Fields {
		key := Screen(field.Label)
		switch field.Label {
		case "id":
			key = ScreenIdentity
		case "k8s":
			key = ScreenKubernetes
		}
		state := "highlighted"
		if field.Value == "none" || field.Value == "" {
			state = "empty"
		} else if m.draft[key] != "" && m.draft[key] == preview.Draft[key] {
			state = "staged"
		}
		if field.Label == "ADC" {
			if preview.Draft[ScreenIdentity] == "" {
				state = "empty"
			} else if m.draft[ScreenWorkspace] != "" || m.draft[ScreenShellADCOverride] != "" {
				state = "staged"
			} else if field.Value == "off" {
				state = "empty"
			}
		}
		preview.FieldStates[field.Label] = state
	}
	return preview
}

// Projection shared by cursor previews and explicit Space replacements.
func (m AppModel) previewSelection(screen Screen, option Option) LaunchPreview {
	if screen == "" && !m.hasStagedSelection() {
		return LaunchPreview{Fields: []PickerField{{Label: "id", Value: "none"}, {Label: "project", Value: "none"}, {Label: "k8s", Value: "none"}, {Label: "docker", Value: "none"}, {Label: "ADC", Value: "off"}}}
	}
	if m.launchPreview != nil {
		return m.launchPreview(screen, option, cloneDraft(m.draft))
	}
	// Standalone UI models can use picker-provided dependency metadata.
	name := option.Name
	copy := m
	copy.draft = cloneDraft(m.draft)
	if screen != "" && copy.draft[screen] != name {
		copy.clearDownstreamResourceDraft(screen)
		delete(copy.draft, ScreenWorkspace)
	}
	if screen != "" {
		copy.draft[screen] = name
	}
	for key, value := range option.Selection {
		copy.draft[key] = value
	}
	identity := selectionIdentity(option, m.draft[ScreenIdentity])
	if identity != "" || option.RequiresIdentity || len(option.IdentityChoices) > 0 {
		copy.draft[ScreenIdentity] = identity
	}
	if screen == ScreenWorkspace {
		copy.draft[ScreenWorkspaceSource] = name
	}
	needs := (option.RequiresIdentity || len(option.IdentityChoices) > 1) && copy.draft[ScreenIdentity] == ""
	preview := LaunchPreview{Available: true, Draft: copy.draft, NeedsIdentity: needs, IdentityChoices: option.IdentityChoices}
	for _, key := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker} {
		if name := copy.draft[key]; name != "" {
			preview.Fields = append(preview.Fields, PickerField{Label: string(key), Value: firstNonEmptyUI(m.resourceLabels[key][name], name)})
		}
	}
	return preview
}

func (m AppModel) useNextShell(action string) (tea.Model, tea.Cmd) {
	frame := m.current()
	rows := filteredPickerOptions(frame.picker, frame.filter)
	if len(rows) == 0 && action == "stage" {
		return m, nil
	}
	if len(rows) == 0 && action == "save-selection-workspace" {
		return m.selectPickerOption(Option{}, action)
	}
	if len(rows) == 0 && !m.nextShell(frame).Available {
		return m, nil
	}
	option := Option{}
	if len(rows) > 0 {
		option = rows[min(frame.cursor, len(rows)-1)]
	}
	if isBrowserAction(option) || option.OpenScreen != "" {
		if action == "apply-shell" || action == "default-shell" {
			return m.selectPickerOption(option, "")
		}
		return m, nil
	}
	if action == "apply-shell" && !m.canApplyShell {
		m.push(Picker{Screen: "shell-integration", Title: "Current shell unavailable", Description: "Press Esc, then Option+Enter to start a subshell. To apply selections to this shell, enable Zsh integration with:\n\neval \"$(chop shell-init zsh --in-place)\"", HideSearch: true, DisableEnter: true}, m.draft)
		return m, nil
	}
	screen, name := resourceTarget(frame.screen, option)
	if action == "stage" && m.draft[screen] == name {
		staged := m.draft.ContextSelection()
		staged.Unstage(selection.Resource(screen))
		m.draft.setContextSelection(staged)
		m.refreshStagedRow(option.Name)
		return m, nil
	}
	preview := m.nextShell(frame)
	if action == "stage" {
		copy := m
		copy.draft = cloneDraft(m.draft)
		if copy.draft[screen] != option.Name {
			delete(copy.draft, ScreenShellADCOverride)
		}
		preview = copy.previewSelection(screen, option)
	}
	if !preview.Available {
		return m, nil
	}
	if preview.NeedsIdentity && len(preview.IdentityChoices) == 0 && (action == "stage" || m.launchPreview == nil) {
		return m.stageResource(option)
	}
	if preview.NeedsIdentity && len(preview.IdentityChoices) > 0 {
		m.push(Picker{Screen: "next-shell-identity", Title: "Choose identity", Description: "Choose an account to continue with this context. Esc cancels.", ContextFields: preview.Fields, Options: preview.IdentityChoices, previewDraft: cloneDraft(preview.Draft), previewAction: action}, m.draft)
		return m, nil
	}
	if preview.Error != "" {
		m.push(Picker{Screen: "next-shell-error", Title: "Context needs attention", Description: preview.Error, HideSearch: true, DisableEnter: true}, m.draft)
		return m, nil
	}
	if action == "save-selection-workspace" {
		return m.selectPickerOption(Option{FlowDraft: preview.Draft}, action)
	}
	m.draft = cloneDraft(preview.Draft)
	if action == "stage" {
		m.refreshStagedRow(option.Name)
		return m, nil
	}
	return m.selectPickerOption(Option{}, action)
}

func (m *AppModel) refreshStagedRow(name string) {
	m.recordWorkspaceStage()
	frame := m.current()
	if picker, ok := m.browserPicker(frame.screen); ok {
		m.replaceBrowserRoot(picker)
		current := &m.stack[len(m.stack)-1]
		current.filter, current.searching = frame.filter, frame.searching
		current.focusOption(name)
	}
}

func rowActionHints(actions []KeyAction) []string {
	hints := []string{}
	for _, action := range actions {
		hints = append(hints, shortcut(action.Key, action.footerLabel()))
	}
	return hints
}
