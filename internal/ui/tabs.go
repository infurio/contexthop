package ui

// tabDefinition owns navigation and behavioural differences between resource tabs.
// The application owns window state; table modules own resource presentation.
type tabDefinition struct {
	module                     resourceModule
	screen                     Screen
	label, key, noun           string
	scope, localImport, hidden bool
}

var applicationTabs = []tabDefinition{
	{module: workspaceModule{}, screen: ScreenWorkspace, label: "Workspaces", key: "W", noun: "workspace"},
	{module: identityModule{}, screen: ScreenIdentity, label: "Identities", key: "I", noun: "identity", localImport: true, hidden: true},
	{module: projectModule{}, screen: ScreenProject, label: "Projects", key: "P", noun: "project", scope: true, hidden: true},
	{module: kubernetesModule{}, screen: ScreenKubernetes, label: "Kubernetes", key: "K", noun: "Kubernetes target", scope: true, localImport: true, hidden: true},
	{module: dockerModule{}, screen: ScreenDocker, label: "Docker", key: "D", noun: "Docker context", localImport: true, hidden: true},
}

func tabFor(screen Screen) tabDefinition {
	for _, tab := range applicationTabs {
		if tab.screen == screen {
			return tab
		}
	}
	return tabDefinition{screen: screen, noun: "resource"}
}

func browserTarget(current Screen, key string) Screen {
	for index, tab := range applicationTabs {
		if key == tab.key {
			return tab.screen
		}
		if tab.screen != current {
			continue
		}
		switch key {
		case "left", "right", "tab", "shift+tab":
			offset := 1
			if key == "left" || key == "shift+tab" {
				offset = -1
			}
			next := index + offset
			if (key == "left" || key == "right") && (next < 0 || next >= len(applicationTabs)) {
				return ""
			}
			return applicationTabs[(next+len(applicationTabs))%len(applicationTabs)].screen
		}
	}
	return ""
}
