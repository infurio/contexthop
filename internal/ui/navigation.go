package ui

import (
	"github.com/infurio/contexthop/internal/selection"
	"slices"
	"strings"
)

// StackDepth exposes navigation depth for deterministic model tests.
func (m AppModel) StackDepth() int { return len(m.stack) }

// CurrentScreen reports the visible page.
func (m AppModel) CurrentScreen() Screen { return m.current().screen }

func (m AppModel) current() navigationFrame { return m.stack[len(m.stack)-1] }

func (m *AppModel) pushConfigured(screen Screen) {
	if picker, ok := m.browserPicker(screen); ok && picker.ResourceBrowser {
		m.push(picker, m.draft)
		return
	}
	if picker, ok := m.pickers[screen]; ok {
		m.push(picker, m.draft)
	}
}

func (m *AppModel) push(picker Picker, draftBefore Draft) {
	picker = preparePicker(m.resourceRows(picker))
	if picker.ResourceBrowser {
		picker.ShowHidden = m.showHidden[picker.Screen]
	}
	if picker.Screen == "" {
		picker.Screen = screenForDimension(picker.Dimension)
	}
	m.rememberResourceLabels(picker)
	frame := navigationFrame{screen: picker.Screen, picker: clonePicker(picker), draftBefore: cloneDraft(draftBefore)}
	frame.focusOption(picker.Focus)
	if picker.Input != nil {
		frame.input = picker.Input.Initial
	}
	m.stack = append(m.stack, frame)
}

func (m *AppModel) replaceCurrent(picker Picker) {
	picker = preparePicker(m.resourceRows(picker))
	if picker.ResourceBrowser {
		picker.ShowHidden = m.showHidden[picker.Screen]
	}
	if picker.Screen == "" {
		picker.Screen = screenForDimension(picker.Dimension)
	}
	m.rememberResourceLabels(picker)
	frame := &m.stack[len(m.stack)-1]
	frame.screen = picker.Screen
	frame.picker = clonePicker(picker)
	frame.filter = ""
	frame.cursor = 0
	frame.cursorKey = ""
	frame.input = ""
	frame.inputOffset = 0
	if picker.Input != nil {
		frame.input = picker.Input.Initial
	}
	frame.inputErr = ""
	frame.searching = false
	frame.focusOption(picker.Focus)
}

func (m *AppModel) replaceBrowserRoot(picker Picker) {
	m.replaceCurrent(picker)
	current := m.stack[len(m.stack)-1]
	current.draftBefore = Draft{}
	m.stack = []navigationFrame{current}
}

func (m *AppModel) clearDownstreamResourceDraft(screen Screen) {
	state := m.draft.ContextSelection()
	state.ClearDependents(selection.Resource(screen))
	m.draft.setContextSelection(state)
}

func (m *AppModel) popResourceSelection() bool {
	state := m.draft.ContextSelection()
	if !state.UnstageNext(m.composeSelection) {
		return false
	}
	m.draft.setContextSelection(state)
	return true
}

func (m AppModel) selectionClearLabel() string {
	for _, item := range []struct {
		screen Screen
		label  string
	}{{ScreenKubernetes, "Kubernetes"}, {ScreenProject, "project"}, {ScreenIdentity, "identity"}, {ScreenDocker, "Docker"}, {ScreenWorkspace, "workspace"}} {
		if m.draft[item.screen] != "" {
			return item.label
		}
	}
	return ""
}

func (m AppModel) browserPicker(screen Screen) (Picker, bool) {
	if m.browser != nil {
		draft := cloneDraft(m.draft)
		if m.browseAll && (screen == ScreenProject || screen == ScreenKubernetes) {
			delete(draft, ScreenIdentity)
			delete(draft, ScreenProject)
		}
		picker := m.browser(screen, draft)
		if picker.Screen != "" {
			picker.ShowHidden = m.showHidden[screen]
			return picker, true
		}
	}
	picker, ok := m.pickers[screen]
	picker.ShowHidden = m.showHidden[screen]
	return picker, ok
}

func (frame *navigationFrame) focusOption(name string) {
	if name == "" {
		return
	}
	for index, option := range filteredPickerOptions(frame.picker, frame.filter) {
		if option.Name == name {
			frame.cursor = index
			frame.cursorKey = name
			return
		}
	}
}

func filteredPickerOptions(picker Picker, filter string) []Option {
	model := listModel{options: picker.Options, dimension: picker.Dimension, filter: filter, resourceBrowser: picker.ResourceBrowser, showHidden: picker.ShowHidden}
	return model.filteredOptions()
}

func screenForDimension(dimension string) Screen {
	switch dimension {
	case "identity":
		return ScreenIdentity
	case "project":
		return ScreenProject
	case "kubernetes":
		return ScreenKubernetes
	case "docker":
		return ScreenDocker
	case "catalog", "catalog-entity":
		return ScreenCatalogEntity
	case "action", "catalog-action":
		return ScreenCatalogAction
	case "dependency", "dependency-target":
		return ScreenDependencyTarget
	case "preview":
		return ScreenPreview
	case "confirm":
		return ScreenConfirm
	case "input":
		return ScreenInput
	default:
		return ScreenWorkspace
	}
}

func cloneDraft(source Draft) Draft {
	result := make(Draft, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func clonePicker(source Picker) Picker {
	source.optionsCommands = slices.Clone(source.optionsCommands)
	source.previewDraft = cloneDraft(source.previewDraft)
	source.OperationDraft = cloneDraft(source.OperationDraft)
	source.OperationActions = cloneKeyActions(source.OperationActions)
	source.ContextFields = append([]PickerField(nil), source.ContextFields...)
	source.Options = append([]Option(nil), source.Options...)
	for index := range source.Options {
		if len(source.Options[index].IdentityChoices) > 0 {
			source.Options[index].IdentityChoices = clonePicker(Picker{Options: source.Options[index].IdentityChoices}).Options
		}
		source.Options[index].Tags = append([]Tag(nil), source.Options[index].Tags...)
		source.Options[index].FlowDraft = cloneDraft(source.Options[index].FlowDraft)
		source.Options[index].Selection = cloneDraft(source.Options[index].Selection)
		source.Options[index].Actions = cloneKeyActions(source.Options[index].Actions)
	}
	if source.Input != nil {
		input := *source.Input
		source.Input = &input
	}
	return source
}

func clonePickers(source map[Screen]Picker) map[Screen]Picker {
	result := make(map[Screen]Picker, len(source))
	for screen, picker := range source {
		if picker.Screen == "" {
			picker.Screen = screen
		}
		result[screen] = clonePicker(picker)
	}
	return result
}

func pickerOptions(picker Picker) []Option { return picker.Options }

// Tab positions remember browsing state. Staging a workspace gives its resource
// selections priority on the next visit to each entity tab.
// A different scope starts with a fresh position instead of restoring a filter
// that belonged to another identity or project.
type tabPosition struct {
	workspaceStage      uint64
	operationDraft      Draft
	scope, filter, name string
	notice, details     string
	statusKind          string
	operationActions    []KeyAction
	cursor              int
}

func (m *AppModel) switchResourceTab(target Screen) {
	m.identityRecovery = nil
	if target == "" || target == m.current().screen {
		return
	}
	frame := m.current()
	if m.tabPositions == nil {
		m.tabPositions = map[Screen]tabPosition{}
	}
	filtered := filteredPickerOptions(frame.picker, frame.filter)
	name := ""
	if len(filtered) > 0 {
		name = filtered[min(frame.cursor, len(filtered)-1)].Name
	}
	m.tabPositions[frame.screen] = tabPosition{workspaceStage: m.workspaceStage, scope: frame.picker.ScopeLabel, filter: frame.filter, name: name, cursor: frame.cursor, notice: frame.picker.Description, details: frame.picker.OperationDetails, statusKind: frame.picker.StatusKind, operationActions: frame.picker.OperationActions, operationDraft: frame.picker.OperationDraft}
	picker, ok := m.browserPicker(target)
	if !ok {
		return
	}
	picker.Focus = m.draft[target]
	m.replaceBrowserRoot(picker)
	position, exists := m.tabPositions[target]
	if !exists || position.scope != picker.ScopeLabel {
		return
	}
	current := &m.stack[len(m.stack)-1]
	current.filter = position.filter
	current.picker.Description, current.picker.OperationDetails = position.notice, position.details
	current.picker.StatusKind, current.picker.OperationActions = position.statusKind, position.operationActions
	current.picker.OperationDraft = cloneDraft(position.operationDraft)
	current.picker.ShowHidden = m.showHidden[target]
	filtered = filteredPickerOptions(current.picker, position.filter)
	current.cursor = max(0, min(position.cursor, len(filtered)-1))
	for index, option := range filtered {
		if option.Name == position.name {
			current.cursor = index
			break
		}
	}
	if len(filtered) > 0 {
		current.cursorKey = filtered[current.cursor].Name
	} else {
		current.cursorKey = ""
	}

	// A newly staged workspace replaces the entity selections even when their
	// scope is unchanged. Focus those rows once, then resume normal tab memory.
	if target != ScreenWorkspace && position.workspaceStage != m.workspaceStage && m.draft[target] != "" {
		name := m.draft[target]
		for _, option := range filteredPickerOptions(current.picker, "") {
			if option.Name != name {
				continue
			}
			current.focusOption(name)
			if current.cursorKey != name {
				current.filter, current.searching = "", false
				current.focusOption(name)
			}
			break
		}
	}
}

func (m *AppModel) recordWorkspaceStage() {
	if m.current().screen == ScreenWorkspace && m.draft[ScreenWorkspace] != "" {
		m.workspaceStage++
	}
}

func (m AppModel) resourceRows(picker Picker) Picker {
	if m.composeSelection && picker.ResourceBrowser {
		picker.Options = slices.DeleteFunc(slices.Clone(picker.Options), func(option Option) bool { return strings.HasPrefix(option.Name, "\x00__") })
	}
	return picker
}

// Lists with row actions explicitly enter search before accepting text.
func preparePicker(picker Picker) Picker {
	if picker.ResourceBrowser {
		picker.ModalActions = true
		return picker
	}
	if !picker.HideSearch && picker.Input == nil {
		for _, option := range picker.Options {
			if len(option.Actions) > 0 {
				picker.ModalActions = true
				break
			}
		}
	}
	return picker
}

func (m AppModel) acceptsText() bool {
	frame := m.current()
	return !m.working && (frame.picker.Input != nil ||
		frame.picker.ReadOnlyText == "" && !frame.picker.HideSearch && (!frame.picker.ModalActions || frame.searching))
}

func cloneKeyActions(actions []KeyAction) []KeyAction {
	result := slices.Clone(actions)
	for i := range result {
		result[i].Aliases = slices.Clone(result[i].Aliases)
	}
	return result
}
