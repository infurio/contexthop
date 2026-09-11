package main

import (
	"fmt"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
	"strings"
)

const (
	screenDiscover         ui.Screen = "discover"
	screenDiscoverIdentity ui.Screen = "discover-identity"
	screenDiscoverProject  ui.Screen = "discover-project"
	screenDiscoverScope    ui.Screen = "discover-scope"
	discoverIdentity       ui.Screen = "discover-selected-identity"
	discoverProject        ui.Screen = "discover-selected-project"
	discoverScope          ui.Screen = "discover-selected-scope"
	discoverOrigin         ui.Screen = "discover-origin"
	discoverRetryProjects  ui.Screen = "discover-retry-projects"
)

func discoveryDialogFlow(cfg, saved config.Config, choice ui.Choice, draft ui.Draft) (ui.Transition, bool) {
	if choice.Action == "discover-resources" || choice.Action == "retry-discovery" {
		if blocked, ok := discoveryBlocked(); ok {
			return blocked, true
		}
	}
	if choice.Action == "retry-discovery" {
		updates := ui.Draft{discoverIdentity: draft[discoverIdentity], discoverScope: draft[discoverScope], discoverProject: draft[discoverProject], discoverOrigin: draft[discoverOrigin], discoverRetryProjects: draft[discoverRetryProjects]}
		if draft[discoverRetryProjects] != "" {
			draft[discoverScope] = "retry"
			updates[discoverScope] = "retry"
		}
		return ui.Transition{Picker: discoveryDialog(cfg, draft, "Retry the failed or interrupted scopes from the last discovery."), DraftUpdates: updates}, true
	}
	if choice.Action == "discover-resources" {
		if len(cfg.Identities) == 0 {
			return ui.Transition{Picker: ui.Picker{Screen: "discovery-setup", Title: "Add an identity to discover cloud resources", HideSearch: true, CompactDialog: true, Options: []ui.Option{{Name: "add-identity", Label: "Add identity"}, {Name: "import-local-contexts", Label: "Import local contexts"}}}}, true
		}
		scope, identity, project := "projects", draft[ui.ScreenIdentity], ""
		switch choice.Screen {
		case ui.ScreenIdentity:
			if _, ok := cfg.Identities[choice.Option.Name]; ok {
				identity = choice.Option.Name
			}
		case ui.ScreenProject:
			if _, ok := cfg.Projects[choice.Option.Name]; ok {
				project = choice.Option.Name
			}
		case ui.ScreenKubernetes:
			target, exists := cfg.Kubernetes[choice.Option.Name]
			project = target.Project
			if !exists {
				project = draft[ui.ScreenProject]
			}
		}
		if project != "" {
			scope = "clusters"
		}
		updates := ui.Draft{discoverScope: scope, discoverIdentity: identity, discoverProject: project, discoverOrigin: string(choice.Screen)}
		normalizeDiscoveryIdentity(cfg, updates)
		for key, value := range updates {
			draft[key] = value
		}
		return ui.Transition{Picker: discoveryDialog(cfg, draft, ""), DraftUpdates: updates}, true
	}
	switch choice.Screen {
	case screenDiscoverIdentity, screenDiscoverProject, screenDiscoverScope:
		if choice.Option.Name == "discover-projects" {
			draft[discoverScope] = "projects"
			return ui.Transition{ReturnToPrevious: true, Picker: discoveryDialog(cfg, draft, "Discover projects first, then return to cluster discovery."), DraftUpdates: ui.Draft{discoverScope: "projects"}}, true
		}
		field := discoverIdentity
		if choice.Screen == screenDiscoverProject {
			field = discoverProject
		}
		if choice.Screen == screenDiscoverScope {
			field = discoverScope
		}
		draft[field] = choice.Option.Name
		normalizeDiscoveryIdentity(cfg, draft)
		return ui.Transition{ReturnToPrevious: true, Picker: discoveryDialog(cfg, draft, ""), DraftUpdates: ui.Draft{field: draft[field], discoverIdentity: draft[discoverIdentity]}}, true
	case screenDiscover:
		switch choice.Option.Name {
		case "cancel":
			return ui.Transition{Dismiss: true}, true
		case "identity":
			var allowed []string
			if draft[discoverScope] == "clusters" && draft[discoverProject] != "" {
				allowed = resolver.EligibleIdentities(cfg, draft[discoverProject], "")
			}
			options := formatCatalogComponentOptions(catalog.KindIdentity, identityOptions(cfg, allowed, true))
			description := "Choose the single identity used for this run."
			if len(options) == 0 {
				description = "Discover the project relationships for an identity first."
				options = []ui.Option{{Name: "discover-projects", Label: "Discover projects first"}}
			}
			return ui.Transition{Picker: ui.Picker{Screen: screenDiscoverIdentity, Title: "Discovery identity", Description: description, Options: options, Focus: draft[discoverIdentity], DisableEnter: len(options) == 0, CompactDialog: true}}, true
		case "project":
			options := formatCatalogComponentOptions(catalog.KindProject, projectOptions(cfg, "", true))
			description := "Choose a project, then an identity with access."
			if len(options) == 0 {
				description = "No projects are available yet."
				options = []ui.Option{{Name: "discover-projects", Label: "Discover projects"}}
			}
			return ui.Transition{Picker: ui.Picker{Screen: screenDiscoverProject, Title: "Discovery project", Description: description, Options: options, Focus: draft[discoverProject], DisableEnter: len(options) == 0, CompactDialog: true}}, true
		case "scope":
			return ui.Transition{Picker: ui.Picker{Screen: screenDiscoverScope, CompactDialog: true, Title: "Discovery scope", Focus: draft[discoverScope], Options: []ui.Option{
				{Name: "projects", Label: "Projects", Detail: "List projects available to one identity"},
				{Name: "clusters", Label: "Clusters in one project", Detail: "Query Kubernetes clusters in one selected project"},
				{Name: "all", Label: "Projects and all their clusters", Detail: "Queries every discovered project; can take considerably longer"},
			}}}, true
		case "start":
			if notice := discoveryValidation(cfg, draft); notice != "" {
				return ui.Transition{ReplaceCurrent: true, Picker: discoveryDialog(cfg, draft, notice)}, true
			}
			return runDiscoveryDialog(cfg, saved, draft, choice.ReportProgress, choice.Context), true
		}
	}
	return ui.Transition{}, false
}

func normalizeDiscoveryIdentity(cfg config.Config, draft ui.Draft) {
	if draft[discoverScope] != "clusters" || draft[discoverProject] == "" {
		return
	}
	candidates := resolver.EligibleIdentities(cfg, draft[discoverProject], "")
	for _, candidate := range candidates {
		if candidate == draft[discoverIdentity] {
			return
		}
	}
	draft[discoverIdentity] = ""
	if len(candidates) == 1 {
		draft[discoverIdentity] = candidates[0]
	}
}

func discoveryValidation(cfg config.Config, draft ui.Draft) string {
	if draft[discoverScope] == "clusters" {
		if _, ok := cfg.Projects[draft[discoverProject]]; !ok {
			return "Choose a project first, then an identity with access."
		}
	}
	if _, ok := cfg.Identities[draft[discoverIdentity]]; !ok {
		return "Choose an identity before starting discovery."
	}
	switch draft[discoverScope] {
	case "projects", "all":
	case "retry":
		if draft[discoverRetryProjects] == "" {
			return "No failed project scans to retry."
		}
		for _, name := range strings.Split(draft[discoverRetryProjects], "\x00") {
			if !resolver.ProjectIdentityAvailable(cfg, name, draft[discoverIdentity]) {
				return "A retry project is no longer available to this identity. Choose another scope."
			}
		}
	case "clusters":
		if _, ok := cfg.Projects[draft[discoverProject]]; !ok {
			return "Choose a project before starting discovery."
		}
		if !resolver.ProjectIdentityAvailable(cfg, draft[discoverProject], draft[discoverIdentity]) {
			return "Choose an identity associated with this project."
		}
	default:
		return "Choose a discovery scope."
	}
	return ""
}

func discoveryDialog(cfg config.Config, draft ui.Draft, notice string) ui.Picker {
	identity := "Choose identity…"
	if item, ok := cfg.Identities[draft[discoverIdentity]]; ok {
		identity = fmt.Sprintf("%s [%s]", item.Account, draft[discoverIdentity])
	}
	labels := map[string]string{"projects": "Projects", "clusters": "Clusters in one project", "all": "Projects and all their clusters", "retry": "Failed project scans"}
	options := []ui.Option{{Name: "scope", Label: fmt.Sprintf("%-10s %s", "Scope", labels[draft[discoverScope]])}}
	if draft[discoverScope] == "clusters" {
		project := "Choose project…"
		if item, ok := cfg.Projects[draft[discoverProject]]; ok {
			project = item.ProjectID
		}
		options = append(options, ui.Option{Name: "project", Label: fmt.Sprintf("%-10s %s", "Project", project)})
	}
	options = append(options, ui.Option{Name: "identity", Label: fmt.Sprintf("%-10s %s", "Identity", identity)})
	description := "Choose the scope and identity, then start discovery."
	if draft[discoverScope] == "all" {
		description += "\nFull scan: queries every project and can take considerably longer."
	}
	if notice != "" {
		description += "\n\n" + notice
	}
	workLabel := "Discovering " + labels[draft[discoverScope]] + " as " + identity
	if draft[discoverScope] == "clusters" {
		workLabel = "Discovering clusters in " + cfg.Projects[draft[discoverProject]].ProjectID + " as " + identity
	}
	options = append(options, ui.Option{Name: "start", Label: "Start discovery", WorkLabel: workLabel, Cancellable: true, EnterLabel: "Start"}, ui.Option{Name: "cancel", Label: "Cancel"})
	focus := "start"
	if draft[discoverScope] == "clusters" && draft[discoverProject] == "" {
		focus = "project"
	} else if draft[discoverIdentity] == "" {
		focus = "identity"
	}
	return ui.Picker{CompactDialog: true, Screen: screenDiscover, Title: "Discover", Description: description, Options: options, HideSearch: true, Focus: focus}
}
