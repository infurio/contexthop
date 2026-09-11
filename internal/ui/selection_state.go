package ui

import "github.com/infurio/contexthop/internal/selection"

// ContextSelection is the typed resource state carried across dialogs. Other
// draft keys are navigation inputs and never enter selection resolution.
func (d Draft) ContextSelection() selection.Request {
	return selection.Request{
		Identity: d[ScreenIdentity], Project: d[ScreenProject],
		Kubernetes: d[ScreenKubernetes], Docker: d[ScreenDocker],
		Workspace: d[ScreenWorkspace], Source: d[ScreenWorkspaceSource],
		ADCOverride: selection.ADCChoice(d[ScreenShellADCOverride]),
	}
}

// setContextSelection replaces only resource state, preserving dialog inputs.
func (d Draft) setContextSelection(s selection.Request) {
	for key, value := range map[Screen]string{
		ScreenIdentity: s.Identity, ScreenProject: s.Project,
		ScreenKubernetes: s.Kubernetes, ScreenDocker: s.Docker,
		ScreenWorkspace: s.Workspace, ScreenWorkspaceSource: s.Source,
		ScreenShellADCOverride: string(s.ADCOverride),
	} {
		if value == "" {
			delete(d, key)
		} else {
			d[key] = value
		}
	}
}
