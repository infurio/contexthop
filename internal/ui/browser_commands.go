package ui

// Browser commands describe the direct controls available on each tab.
func (m listModel) browserCommands(selected Option, hasRow bool) []KeyAction {
	tab := tabFor(Screen(firstNonEmptyUI(string(m.tabScreen), m.dimension)))
	actions := append([]KeyAction{{Key: "o", Label: "Options", Action: "ui:options"}}, m.operationActions...)
	if m.composeSelection {
		if hasRow || m.hasSelectedContext() {
			for _, action := range contextLaunchCommands() {
				if action.Action != "stage" || hasRow {
					actions = append(actions, action)
				}
			}
		}
		adcLabel := "Enable ADC"
		if m.nextShellPreview == nil || !m.nextShellPreview.canConfigureADC() {
			adcLabel = "Enable ADC (select identity first)"
		} else if m.nextShellPreview.adcEnabled() {
			adcLabel = "Disable ADC"
		}
		actions = append(actions, KeyAction{Key: "A", Label: adcLabel, Action: "ui:toggle-adc"})
		if m.tabSelection[ScreenShellADCOverride] != "" {
			label := "Reset ADC to default"
			if m.nextShellPreview != nil && m.nextShellPreview.Draft.ContextSelection().SourceWorkspace() != "" {
				label = "Use workspace ADC setting"
			}
			actions = append(actions, KeyAction{Label: label, Action: "ui:reset-adc"})
		}
		actions = append(actions, KeyAction{Key: "ctrl+w", Label: "Save Selected as workspace", Action: "save-selection-workspace"})
	}
	if hasRow || selectionNotice(m.nextShellPreview).detailsAvailable || m.pickerDescription != "" {
		actions = append(actions, KeyAction{Key: "i", Label: "Resource info", Action: "ui:info"})
	}
	if hasRow {
		for _, action := range selected.Actions {

			actions = append(actions, action)
		}
	}
	if m.operationDetails == "" && m.pickerDescription != "" {
		actions = append(actions, KeyAction{Label: "Full status message", Action: "ui:status"})
	}
	if m.operationDetails != "" {
		actions = append(actions, KeyAction{Label: "Last operation results", Action: "ui:status"})
	}
	if tab.screen != ScreenDocker {
		actions = append(actions, KeyAction{Key: "d", Label: "Discover cloud resources", Footer: m.discoveryFooterLabel(), Action: "discover-resources"})
	}
	actions = append(actions, KeyAction{Key: "n", Label: "New " + tab.noun, Footer: "New", Action: "add-resource"})
	if tab.scope {
		label := "Show all resources (keep selection)"
		if m.browseAll {
			label = "Filter by selected resources"
		}
		actions = append(actions, KeyAction{Key: "b", Label: label, Action: "ui:scope"})
	}
	if tab.hidden {
		actions = append(actions, KeyAction{Key: "h", Label: m.hiddenToggleLabel(), Action: "ui:hidden"})
	}
	if m.composeSelection || m.scoped {
		actions = append(actions, KeyAction{Key: "c", Label: "Clear all selected resources", Action: "ui:clear"})
	}
	actions = append(actions, KeyAction{Key: "ctrl+g", Label: "Join shared", Action: "follow-shared"})
	actions = append(actions, KeyAction{Key: KeyReuse, Label: "Copy context from session…", Action: "ui:reuse"}, KeyAction{Key: "r", Label: "Refresh active shell status", Action: "ui:refresh"})
	if tab.localImport {
		actions = append(actions, KeyAction{Key: "l", Label: "Import local contexts", Footer: "Import local", Action: "import-local-contexts"})
	}
	actions = append(actions, KeyAction{Key: "q", Label: "Quit", Action: "ui:quit"})
	return actions
}

func (m listModel) hasSelectedContext() bool {
	return m.nextShellPreview != nil && m.nextShellPreview.Available
}
