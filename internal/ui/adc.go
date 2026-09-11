package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/infurio/contexthop/internal/selection"
)

func (p LaunchPreview) adcEnabled() bool {
	for _, field := range p.Fields {
		if field.Label == "ADC" {
			return field.Value == "identity"
		}
	}
	return false
}

func (p LaunchPreview) canConfigureADC() bool {
	return p.Available && !p.NeedsIdentity && p.Draft[ScreenIdentity] != ""
}

func (m AppModel) toggleADC() (tea.Model, tea.Cmd) {
	preview := m.nextShell(m.current())
	if !preview.canConfigureADC() {
		m.push(Picker{Screen: "adc-needs-identity", Title: "Select an identity to enable ADC",
			Description: "ADC is off because Selected has no identity. Highlight or stage an identity, or choose a workspace containing one, then press Shift+A.",
			HideSearch:  true, DisableEnter: true, CompactDialog: true}, m.draft)
		return m, nil
	}
	override := selection.ADCIdentity
	if preview.adcEnabled() {
		override = selection.ADCOff
	}
	// Changing a launch setting must not stage cursor-derived resources.
	m.draft[ScreenShellADCOverride] = string(override)
	return m, nil
}
