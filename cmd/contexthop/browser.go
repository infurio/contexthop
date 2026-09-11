package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"

	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/ui"
)

func recentResourceScreen(_ recency.History, cfg config.Config) ui.Screen {
	if len(cfg.Destinations) > 0 {
		return ui.ScreenWorkspace
	}
	return ui.ScreenIdentity
}

func resourceScreenForKind(kind catalog.Kind) ui.Screen {
	switch kind {
	case catalog.KindIdentity:
		return ui.ScreenIdentity
	case catalog.KindProject:
		return ui.ScreenProject
	case catalog.KindKubernetes:
		return ui.ScreenKubernetes
	case catalog.KindDocker:
		return ui.ScreenDocker
	case catalog.KindWorkspace:
		return ui.ScreenWorkspace
	default:
		return ui.ScreenKubernetes
	}
}

func resourceScreenForCategory(category string) ui.Screen {
	switch category {
	case "identities":
		return ui.ScreenIdentity
	case "projects":
		return ui.ScreenProject
	case "kubernetes":
		return ui.ScreenKubernetes
	case "docker":
		return ui.ScreenDocker
	case "workspaces":
		return ui.ScreenWorkspace
	default:
		return ui.ScreenCatalogEntity
	}
}

func interactivePickers(cfg, catalogCfg config.Config, activeSessions []session.Active) map[ui.Screen]ui.Picker {
	addPicker := catalogActionPicker(catalogCfg, catalogAddSelection)
	addPicker.Title = "New resource"
	return map[ui.Screen]ui.Picker{
		ui.ScreenIdentity:   resourceBrowserPicker(catalogCfg, ui.ScreenIdentity, identityOptions(cfg, nil, true), ""),
		ui.ScreenProject:    resourceBrowserPicker(catalogCfg, ui.ScreenProject, sessionProjectOptions(cfg, "", true), ""),
		ui.ScreenKubernetes: resourceBrowserPicker(catalogCfg, ui.ScreenKubernetes, kubernetesOptions(cfg, "", true), ""),
		ui.ScreenDocker:     resourceBrowserPicker(catalogCfg, ui.ScreenDocker, dockerOptions(cfg, true), ""),
		ui.ScreenWorkspace:  resourceBrowserPicker(catalogCfg, ui.ScreenWorkspace, workspacePickerOptions(catalogCfg, true), ""),
		ui.ScreenReuse: {
			Screen: ui.ScreenReuse, Title: "Copy context from session", Dimension: "reuse",
			Description: "Select an open ContextHop session. A new independent session is created without provider discovery or validation.",
			Options:     activeSessionOptions(activeSessions),
		},
		ui.ScreenCatalogAction: addPicker,
	}
}

func interactiveBrowserPicker(cfg, catalogCfg config.Config, screen ui.Screen, draft ui.Draft) ui.Picker {
	identityName := draft[ui.ScreenIdentity]
	projectName := draft[ui.ScreenProject]
	identityLabel, projectLabel := "", ""
	if identity, ok := cfg.Identities[identityName]; ok {
		identityLabel = identity.Account
	}
	if project, ok := cfg.Projects[projectName]; ok {
		projectLabel = project.ProjectID
	}

	switch screen {
	case ui.ScreenIdentity:
		options := identityOptions(cfg, nil, true)
		for index := range options {
			name := options[index].Name
			if projectName != "" && resolver.ProjectIdentityAvailable(cfg, projectName, name) {
				options[index].Selection = ui.Draft{ui.ScreenProject: projectName}
				if target := draft[ui.ScreenKubernetes]; target != "" && resolver.KubernetesIdentityAvailable(cfg, target, name) {
					options[index].Selection[ui.ScreenKubernetes] = target
				}
			}
		}
		return resourceBrowserPicker(catalogCfg, screen, options, "")
	case ui.ScreenProject:
		options := sessionProjectOptions(cfg, identityName, true)
		if identityName != "" {
			options = append(options, noProjectOption(cfg, identityName))
		}
		return resourceBrowserPicker(catalogCfg, screen, options, identityLabel)
	case ui.ScreenKubernetes:
		options := scopedKubernetesOptions(cfg, projectName, identityName, true)
		if projectName != "" {
			options = append(options, noKubernetesOption(cfg, projectName, identityName))
		}
		picker := resourceBrowserPicker(catalogCfg, screen, options, strings.Join(nonEmpty(identityLabel, projectLabel), " → "))
		if identityName != "" && projectName != "" {
			scope := cfg.DiscoveryFor(identityName).Clusters[cfg.Projects[projectName].ProjectID]
			switch {
			case scope.Issue == config.DiscoveryBillingDisabled:
				picker.Description = "Billing disabled: this project needs billing enabled to list Kubernetes clusters. Previous cluster results, if any, are retained."
			case scope.Issue == config.DiscoveryGKEDisabled:
				picker.Description = "GKE disabled: the Kubernetes Engine API is not enabled for this project. Previous cluster results, if any, are retained."
			case scope.Issue == config.DiscoveryAccessDenied:
				picker.Description = "Access denied: this account cannot list clusters in this project. Previous cluster results, if any, are retained."
			case scope.ObservedAt == "" && scope.Stale:
				picker.Description = "Cluster discovery failed for this account. Press d to retry this project."
			case scope.ObservedAt == "":
				picker.Description = "Clusters haven't been discovered for this account. Press d to scan this project."
			case scope.IsStale():
				picker.Description = "Cluster results for this account may be out of date. Press d to refresh this project."
			case len(scope.Resources) == 0:
				picker.Description = "No clusters were found for this account in this project. Press d to scan again."
			}
		}
		return picker
	case ui.ScreenDocker:
		return resourceBrowserPicker(catalogCfg, screen, dockerOptions(cfg, true), "")
	case ui.ScreenWorkspace:
		picker := resourceBrowserPicker(catalogCfg, screen, workspacePickerOptionsForDraft(catalogCfg, draft, true), "")
		selection, err := selectionFromDraft(cfg, draft)
		if err == nil {
			resolved, resolveErr := resolver.Components(cfg, selection)
			if resolveErr == nil {
				matches := matchingWorkspaceNames(catalogCfg, resolved)
				for index := range picker.Options {
					picker.Options[index].MatchesSelection = slices.Contains(matches, picker.Options[index].Name)
				}
			}
		}
		return picker
	default:
		return ui.Picker{}
	}
}

func componentPicker(screen ui.Screen, title string, options []ui.Option) ui.Picker {
	return ui.Picker{Screen: screen, Title: title, Dimension: string(screen), Options: options}
}

func resourceBrowserPicker(cfg config.Config, screen ui.Screen, options []ui.Option, scope string) ui.Picker {
	kind, title := catalog.Kind(""), "Resources"
	switch screen {
	case ui.ScreenIdentity:
		kind, title = catalog.KindIdentity, "Identities"
	case ui.ScreenProject:
		kind, title = catalog.KindProject, "Projects"
	case ui.ScreenKubernetes:
		kind, title = catalog.KindKubernetes, "Kubernetes"
	case ui.ScreenDocker:
		kind, title = catalog.KindDocker, "Docker"
	case ui.ScreenWorkspace:
		kind, title = catalog.KindWorkspace, "Workspaces"
	}
	recordCount := 0
	for index := range options {
		name := options[index].Name
		if strings.HasPrefix(name, "\x00__") {
			continue
		}
		if screen == ui.ScreenProject && options[index].ProjectID != "" {
			options[index].SaveStatus = "Saved"
			if _, saved := cfg.Projects[name]; !saved {
				recordCount++
				options[index].Source = "GCP"
				options[index].SaveStatus = "Unsaved"
				options[index].Summary = "Discovered project · available for this session only. Press Shift+S to save it and its identity mappings."
				options[index].Actions = []ui.KeyAction{{Key: "S", Label: "Save project", Action: "save-project"}}
				continue
			}
		}
		if !catalogRefExists(cfg, catalog.Ref{Kind: kind, Name: name}) {
			continue
		}
		ref := catalog.Ref{Kind: kind, Name: name}
		options[index].Hidden = kind != catalog.KindWorkspace && catalogRefHidden(cfg, ref)
		options[index].Source = resourceSource(cfg, ref)
		options[index].Tags = entityTags(cfg, ref)
		if kind == catalog.KindKubernetes {
			options[index].KubernetesNamespace = firstNonEmpty(cfg.Kubernetes[name].Namespace, "default")
			options[index].KubernetesEffectiveContext = firstNonEmpty(cfg.Kubernetes[name].Context, cfg.Kubernetes[name].Cluster, name)
			if options[index].Selection == nil {
				options[index].Selection = ui.Draft{}
			}
			options[index].Selection[ui.ScreenProject] = cfg.Kubernetes[name].Project
		}
		if kind == catalog.KindWorkspace {
			if resolved, err := resolver.Destination(cfg, name); err == nil {
				options[index].Selection = ui.Draft{ui.ScreenIdentity: resolved.IdentityName, ui.ScreenProject: resolved.ProjectName, ui.ScreenKubernetes: resolved.KubernetesName, ui.ScreenDocker: resolved.DockerName}
			}
		}
		if !options[index].Hidden {
			recordCount++
		}
		options[index].Summary = catalogNodeDetail(cfg, ref)
		options[index].Actions = resourceBrowserActions(catalogRowActions(cfg, ref))
		if kind == catalog.KindIdentity {
			options[index].Actions = append([]ui.KeyAction{{Label: "Browser profile", Action: "configure-browser"}}, options[index].Actions...)
		}

		if options[index].Hidden {
			options[index].Actions = []ui.KeyAction{{Key: "H", Label: "Unhide item", Action: "unhide-entity"}, {Key: "ctrl+d", Label: "Delete", Action: "remove-entity"}}
		}
		if kind == catalog.KindWorkspace {
			options[index].Actions = []ui.KeyAction{{Key: "e", Label: "Edit workspace", Action: "workspace-configure"}, {Key: "N", Label: "Duplicate workspace", Action: "workspace-copy"}, {Key: "ctrl+d", Label: "Delete workspace", Action: "remove-entity"}}
		}

		if options[index].Hidden || kind == catalog.KindWorkspace {
			options[index].Actions = append(options[index].Actions, ui.KeyAction{Key: "t", Label: "Tags", Action: "entity-labels"})
		}

		if launchHasWebConsole(cfg, launchTarget{Kind: string(kind), Name: ref.Name}) {
			options[index].Actions = append(options[index].Actions, ui.KeyAction{Key: ui.KeyConsole, Label: "Open web console", Action: "open-console"})
		}

		// Session-only discovery is still supported by the controller API; normal
		// interactive discovery saves these relationships automatically.
		if screen == ui.ScreenProject && scope != "" && !options[index].Hidden {
			for identityName, identity := range cfg.Identities {
				if identity.Account == scope && identity.Provider == cfg.Projects[name].Provider && mappedIdentityForProject(cfg, name, identityName) == "" {
					options[index].SaveStatus = "Unmapped"
					options[index].Actions = append([]ui.KeyAction{{Key: "S", Label: "Save discovery", Action: "save-project-mapping"}}, options[index].Actions...)
					break
				}
			}
		}
	}
	scopeLabel := "All " + strings.ToLower(title)
	if scope != "" {
		scopeLabel = title + " for " + scope
	}
	countLabel := strings.ToLower(title)
	if screen == ui.ScreenKubernetes {
		countLabel = "Kubernetes targets"
	}
	if screen == ui.ScreenDocker {
		countLabel = "Docker contexts"
	}
	if recordCount == 1 {
		switch screen {
		case ui.ScreenIdentity:
			countLabel = "identity"
		case ui.ScreenProject:
			countLabel = "project"
		case ui.ScreenKubernetes:
			countLabel = "Kubernetes target"
		case ui.ScreenDocker:
			countLabel = "Docker context"
		case ui.ScreenWorkspace:
			countLabel = "workspace"
		}
	}
	listTitle := fmt.Sprintf("%d %s", recordCount, countLabel)
	enterLabel := "Select"
	if screen == ui.ScreenIdentity || screen == ui.ScreenProject {
		enterLabel = "Continue"
	}
	return ui.Picker{
		Screen: screen, Title: listTitle, ScopeLabel: scopeLabel, Dimension: string(screen),
		Options: options, ModalActions: true, ShowSelectedInfo: true, ResourceBrowser: true,
		Scoped: scope != "", EnterLabel: enterLabel,
	}
}

func resourceBrowserActions(actions []ui.KeyAction) []ui.KeyAction {
	result := []ui.KeyAction{}
	kubernetesMap := false
	for _, action := range actions {
		switch action.Action {
		case "kubernetes-map-identity":
			if kubernetesMap {
				continue
			}
			kubernetesMap = true
			action = ui.KeyAction{Key: "m", Label: "Map", Action: "kubernetes-map"}
		case "workspace-configure":
			action.Label = "Edit workspace"
		case "hide-entity":
			action.Key = "H"
			action.Label = "Hide item"
		case "remove-entity":
			action.Key = "ctrl+d"
		}
		result = append(result, action)
	}
	return result
}

func workspacePickerOptions(cfg config.Config, _ ...bool) []ui.Option {
	// Older configurations may contain hidden workspaces; all presets are visible.
	return destinationOptions(cfg, "workspace", true)
}

func workspacePickerOptionsForDraft(cfg config.Config, _ ui.Draft, includeHidden ...bool) []ui.Option {
	return workspacePickerOptions(cfg, includeHidden...)
}

func activeSessionOptions(records []session.Active) []ui.Option {
	options := make([]ui.Option, 0, len(records))
	for _, record := range records {
		kubernetes := record.Expected.Kubernetes
		if record.Expected.Namespace != "" && record.Expected.Namespace != "default" {
			kubernetes += "/" + record.Expected.Namespace
		}
		options = append(options, ui.Option{
			Name: record.Key, Label: record.Destination, IdentityAccount: record.Expected.Identity,
			KubernetesContext: kubernetes, Detail: shortAge(time.Since(record.ActivatedAt)),
		})
	}
	return options
}

func shortAge(age time.Duration) string {
	if age < time.Minute {
		return "now"
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(age.Hours()/24))
}

func resourceSource(cfg config.Config, ref catalog.Ref) string {
	source := ""
	switch ref.Kind {
	case catalog.KindIdentity:
		source = cfg.Identities[ref.Name].Provenance
	case catalog.KindProject:
		source = cfg.Projects[ref.Name].Provenance
	case catalog.KindKubernetes:
		source = cfg.Kubernetes[ref.Name].Provenance
	case catalog.KindDocker:
		source = cfg.Docker[ref.Name].Provenance
	case catalog.KindWorkspace:
		source = cfg.Destinations[ref.Name].Provenance
	}
	return strings.ToUpper(firstNonEmpty(source, "unknown"))
}

func orderResourcePicker(picker *ui.Picker) {
	if !picker.ResourceBrowser {
		return
	}
	slices.SortStableFunc(picker.Options, func(a, b ui.Option) int {
		if strings.HasPrefix(a.Name, "\x00__") != strings.HasPrefix(b.Name, "\x00__") {
			if strings.HasPrefix(a.Name, "\x00__") {
				return 1
			}
			return -1
		}
		if order := strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label)); order != 0 {
			return order
		}
		return strings.Compare(a.Name, b.Name)
	})
}
