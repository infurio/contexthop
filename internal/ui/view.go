package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m AppModel) View() tea.View {
	view := m.renderView()
	if m.helpVisible {
		view.Content = m.helpPanel(view.Content)
	}
	view.BackgroundColor = backgroundColor
	view.ForegroundColor = bodyColor
	return view
}

func (m AppModel) renderView() tea.View {
	if m.working {
		width := (listModel{width: m.width}).contentWidth()
		workShortcut := shortcut("ctrl+c", "Close")
		if m.workCompact {
			workShortcut = ""
		}
		if m.workCancel != nil {
			workShortcut = shortcut("esc", "Stop operation")
		}
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		content := (pageLayout{Width: width, Height: m.height,
			Top:    []string{headingStyle.Render(frames[m.workFrame%len(frames)] + " Working…"), "", fitColumn(singleLine(m.workLabel), width), "", fmt.Sprintf("Waiting for the operation to finish · %ds elapsed", int(time.Since(m.workStarted).Seconds()))},
			Footer: [footerHeight]string{"", workShortcut},
		}).Render()
		if m.width >= minimumDialogTerminalWidth && m.height >= minimumDialogHeight {
			panelWidth := min(70, m.width-8)
			panel := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accentColor).Width(panelWidth).Padding(1, 2).Render(
				headingStyle.Render(frames[m.workFrame%len(frames)]+" Working…") + "\n\n" +
					wrapText(singleLine(m.workLabel), panelWidth-4) + "\n\n" +
					fmt.Sprintf("%ds elapsed · Waiting for the operation to finish", int(time.Since(m.workStarted).Seconds())) + "\n\n" + workShortcut)
			background := strings.Split(m.workBackground, "\n")
			for index := max(0, len(background)-footerHeight-1); index < len(background); index++ {
				background[index] = ""
			}
			content = overlayPanel(strings.Join(background, "\n"), panel, m.width, m.height)
		}
		view := tea.NewView(content)
		view.AltScreen = true
		return view
	}
	frame := m.current()

	if parent, ok := m.resourceBrowserParent(); ok {
		return m.dialogView(parent, frame)
	}
	if frame.picker.ReadOnlyText != "" {
		view := tea.NewView(m.readOnlyPage(frame, (listModel{width: m.width}).contentWidth(), m.height))
		view.AltScreen = true
		return view
	}
	if frame.picker.Input != nil {
		return m.inputView(frame)
	}
	return m.pickerModel(frame).View()
}

func (m AppModel) pickerModel(frame navigationFrame) listModel {
	var preview *LaunchPreview
	if frame.picker.ResourceBrowser && m.composeSelection {
		value := m.nextShell(frame)
		preview = &value
	}
	pristine := m.initialResourceDraft != nil
	for _, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes, ScreenDocker, ScreenWorkspace, ScreenShellADCOverride} {
		if m.draft[screen] != m.initialResourceDraft[screen] {
			pristine = false
		}
	}
	return listModel{version: m.version, selectionClearLabel: m.selectionClearLabel(), pendingPristine: pristine, nextShellPreview: preview, canApplyShell: m.canApplyShell,
		selectedKubernetesContext: m.resourceContexts[m.draft[ScreenKubernetes]],
		selectedNamespace:         m.resourceNamespaces[m.draft[ScreenKubernetes]],
		tabScreen:                 frame.screen, browseAll: m.browseAll, operationActions: frame.picker.OperationActions, statusKind: frame.picker.StatusKind,
		compactDialog: frame.picker.CompactDialog,
		actionRows:    frame.picker.ActionRows, contextFields: frame.picker.ContextFields,
		tabSelection:     m.draft,
		composeSelection: m.composeSelection, selectedName: m.draft[frame.screen], selectedDocker: firstNonEmptyUI(m.resourceLabels[ScreenDocker][m.draft[ScreenDocker]], m.draft[ScreenDocker]),
		snapshot: m.snapshot, showHidden: frame.picker.ShowHidden,
		options: frame.picker.Options, pickerTitle: frame.picker.Title,
		operationDetails:  frame.picker.OperationDetails,
		pickerDescription: frame.picker.Description, scopeLabel: frame.picker.ScopeLabel,
		dimension: frame.picker.Dimension, filter: frame.filter, cursor: frame.cursor, hideSearch: frame.picker.HideSearch,
		disableEnter: frame.picker.DisableEnter, showSelectedInfo: frame.picker.ShowSelectedInfo,
		modalActions: frame.picker.ModalActions, searching: frame.searching,
		resourceBrowser: frame.picker.ResourceBrowser, scoped: frame.picker.Scoped || m.composeSelection && m.hasStagedSelection(), enterLabel: frame.picker.EnterLabel,
		selectionPath: m.selectedResourcePath(),
		selection:     m.selectedResourceLabels(),
		width:         m.width, height: m.height,
	}
}

// resourceBrowserParent returns the browser page behind a child workflow. A
// browser-to-browser transition is navigation, not a dialog, so only frames
// below the current non-browser frame are considered.
func (m AppModel) resourceBrowserParent() (navigationFrame, bool) {
	if len(m.stack) < 2 || m.current().picker.ResourceBrowser {
		return navigationFrame{}, false
	}
	for index := len(m.stack) - 2; index >= 0; index-- {
		if m.stack[index].picker.ResourceBrowser {
			return m.stack[index], true
		}
	}
	return navigationFrame{}, false
}

func (m AppModel) selectedResourcePath() string {
	parts := make([]string, 0, 3)
	for _, label := range m.selectedResourceLabels() {
		if label != "" {
			parts = append(parts, label)
		}
	}

	return strings.Join(parts, " → ")
}

func (m AppModel) selectedResourceLabels() [3]string {
	labels := [3]string{}
	for index, screen := range []Screen{ScreenIdentity, ScreenProject, ScreenKubernetes} {
		name := m.draft[screen]
		if name == "" || strings.HasPrefix(name, "\x00__") {
			continue
		}
		label := firstNonEmptyUI(m.resourceLabels[screen][name], name)
		labels[index] = label
	}
	return labels
}

// Keep rendering independent of BrowserPicker, which can rebuild the catalog.
// Refresh labels when accepting picker data, never while drawing a frame.
func (m *AppModel) rememberResourceLabels(picker Picker) {
	if m.resourceLabels == nil {
		m.resourceLabels = make(map[Screen]map[string]string)
	}
	if m.resourceLabels[picker.Screen] == nil {
		m.resourceLabels[picker.Screen] = make(map[string]string)
	}
	for _, option := range picker.Options {
		var label string
		switch picker.Screen {
		case ScreenIdentity:
			label = firstNonEmptyUI(option.IdentityAccount, option.Label, option.Name)
		case ScreenProject:
			label = firstNonEmptyUI(option.ProjectID, option.Label, option.Name)
		case ScreenKubernetes:
			if m.resourceContexts == nil {
				m.resourceContexts = map[string]string{}
				m.resourceNamespaces = map[string]string{}
			}
			m.resourceContexts[option.Name] = firstNonEmptyUI(option.KubernetesEffectiveContext, option.KubernetesContext)
			m.resourceNamespaces[option.Name] = option.KubernetesNamespace
			label = firstNonEmptyUI(option.KubernetesCluster, option.KubernetesContext, option.Label, option.Name)
		case ScreenDocker:
			label = firstNonEmptyUI(option.DockerContext, option.Label, option.Name)
		default:
			continue
		}
		m.resourceLabels[picker.Screen][option.Name] = label
	}
}
