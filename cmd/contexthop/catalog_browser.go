package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

func catalogEntityOptions(cfg config.Config) []ui.Option {
	needsIdentity := 0
	visibleIdentities := 0
	visibleProjects := 0
	visibleKubernetes := 0
	hidden := 0
	for _, identity := range cfg.Identities {
		if identity.Hidden {
			hidden++
		} else {
			visibleIdentities++
		}
	}
	for name, project := range cfg.Projects {
		if project.Hidden {
			hidden++
		} else {
			visibleProjects++
			if len(resolver.EligibleIdentities(cfg, name, "")) == 0 {
				needsIdentity++
			}
		}
	}
	standaloneKubernetes := 0
	for _, target := range cfg.Kubernetes {
		if target.Hidden {
			hidden++
		} else {
			visibleKubernetes++
			if target.Project == "" {
				standaloneKubernetes++
			}
		}
	}
	visibleDocker := 0
	for _, target := range cfg.Docker {
		if target.Hidden {
			hidden++
		} else {
			visibleDocker++
		}
	}
	visibleWorkspaces := len(cfg.Destinations)
	return []ui.Option{
		{Name: catalogAddSelection, Label: "+ New resource", Detail: "guided setup"},
		{Name: catalogCategoryPrefix + "identities", Label: "Identities", Detail: catalogItemCount(visibleIdentities, "account")},
		{Name: catalogCategoryPrefix + "projects", Label: "Projects", Detail: catalogItemCount(visibleProjects, "project") + " · " + catalogNeedsIdentityCount(needsIdentity)},
		{Name: catalogCategoryPrefix + "kubernetes", Label: "Kubernetes", Detail: catalogItemCount(visibleKubernetes, "target") + fmt.Sprintf(" · %d standalone", standaloneKubernetes)},
		{Name: catalogCategoryPrefix + "docker", Label: "Docker", Detail: catalogItemCount(visibleDocker, "target") + " · no cloud dependency"},
		{Name: catalogCategoryPrefix + "workspaces", Label: "Workspaces", Detail: catalogItemCount(visibleWorkspaces, "workspace")},
		{Name: catalogCategoryPrefix + "hidden", Label: "Hidden", Detail: catalogItemCount(hidden, "resource")},
	}
}

func catalogCategoryPicker(cfg config.Config, category string) ui.Picker {
	title := "Catalog"
	options := []ui.Option{}
	for _, node := range catalog.Build(cfg).Nodes {
		include := false
		hidden := catalogRefHidden(cfg, node.Ref)
		switch category {
		case "identities":
			title, include = "Identities", node.Ref.Kind == catalog.KindIdentity && !hidden
		case "projects":
			title = "Projects"
			include = node.Ref.Kind == catalog.KindProject && !hidden
		case "kubernetes":
			title = "Kubernetes"
			include = node.Ref.Kind == catalog.KindKubernetes && !hidden
		case "docker":
			title, include = "Docker", node.Ref.Kind == catalog.KindDocker && !hidden
		case "workspaces":
			title, include = "Workspaces", node.Ref.Kind == catalog.KindWorkspace && !hidden
		case "hidden":
			title, include = "Hidden", hidden
		}
		if !include {
			continue
		}
		option := ui.Option{
			Name: encodeCatalogRef(node.Ref), Label: node.Label,
			Detail: catalogNodeSummary(cfg, node.Ref), Risk: node.Risk,
		}
		option.Tags = entityTags(cfg, node.Ref)
		if category != "hidden" {
			option.Summary = catalogNodeDetail(cfg, node.Ref)
			option.Actions = catalogRowActions(cfg, node.Ref)
		}
		if category == "hidden" {
			option.Label = catalogKindLabel(node.Ref.Kind) + " · " + node.Label
			option.Summary = catalogNodeDetail(cfg, node.Ref)
			option.Actions = []ui.KeyAction{
				{Key: "u", Label: "Unhide", Action: "unhide-entity"},
				{Key: "t", Label: "Tags", Action: "entity-labels"},
				{Key: "ctrl+d", Label: "Delete", Action: "remove-entity"},
			}
		}
		options = append(options, option)
	}
	picker := ui.Picker{Screen: screenCatalogList, Title: "Catalog › " + title, Dimension: "catalog", Options: options}
	if category != "" {
		picker.DisableEnter = true
		picker.ShowSelectedInfo = true
		picker.ModalActions = true
	}
	return picker
}

func catalogRowActions(cfg config.Config, ref catalog.Ref) []ui.KeyAction {
	var actions []ui.KeyAction
	switch ref.Kind {
	case catalog.KindIdentity:
		actions = append(actions, ui.KeyAction{Key: "a", Label: "Authentication", Action: "identity-auth"})
	case catalog.KindProject, catalog.KindKubernetes:
		actions = append(actions, ui.KeyAction{Key: "m", Label: "Map", Action: "entity-map"})
	case catalog.KindDocker:
		actions = append(actions, ui.KeyAction{Key: "m", Label: "Map workspace", Action: "docker-map-workspace"})
		if dockerHasWorkspaceMapping(cfg, ref.Name) {
			actions = append(actions, ui.KeyAction{Label: "Unmap workspace", Action: "docker-unmap-workspace"})
		}
	case catalog.KindWorkspace:
		actions = append(actions, ui.KeyAction{Key: "e", Label: "Configure", Action: "workspace-configure"})
	}
	if ref.Kind != catalog.KindWorkspace {
		actions = append(actions, ui.KeyAction{Key: "h", Label: "Hide", Action: "hide-entity"})
	}
	return append(actions,
		ui.KeyAction{Key: "t", Label: "Tags", Action: "entity-labels"},

		ui.KeyAction{Key: "ctrl+d", Label: "Delete", Action: "remove-entity"},
	)
}

func dockerHasWorkspaceMapping(cfg config.Config, dockerName string) bool {
	for _, workspace := range cfg.Destinations {
		if workspace.Docker == dockerName {
			return true
		}
	}
	return false
}

func catalogRefHidden(cfg config.Config, ref catalog.Ref) bool {
	switch ref.Kind {
	case catalog.KindIdentity:
		return cfg.Identities[ref.Name].Hidden
	case catalog.KindProject:
		return cfg.Projects[ref.Name].Hidden
	case catalog.KindKubernetes:
		return cfg.Kubernetes[ref.Name].Hidden
	case catalog.KindDocker:
		return cfg.Docker[ref.Name].Hidden
	case catalog.KindWorkspace:
		return false
	default:
		return false
	}
}

func identityHasProjectMapping(cfg config.Config, identityName string) bool {
	for _, project := range cfg.Projects {
		if slices.Contains(project.Identities, identityName) {
			return true
		}
	}
	return false
}

func catalogIdentityUnmapPicker(cfg config.Config, encoded string) ui.Picker {
	ref, err := decodeCatalogRef(encoded)
	options := []ui.Option{}
	if err == nil && ref.Kind == catalog.KindIdentity {
		for name, project := range cfg.Projects {
			if slices.Contains(project.Identities, ref.Name) {
				options = append(options, ui.Option{Name: name, Label: project.ProjectID, Detail: catalogNodeSummary(cfg, catalog.Ref{Kind: catalog.KindProject, Name: name}), Risk: project.Risk})
			}
		}
		slices.SortFunc(options, func(left, right ui.Option) int { return strings.Compare(left.Label, right.Label) })
	}
	title := "Catalog › Identities › Unmap project"
	if err == nil {
		title = "Catalog › Identities › " + catalogRefLabel(cfg, ref) + " › Unmap project"
	}
	return ui.Picker{
		Screen: screenCatalogIdentityUnmap, Title: title, Dimension: "catalog-target",
		Description: "Choose the project mapping to remove. The complete change is shown once for confirmation before it is saved.", Options: options,
	}
}

func catalogProjectUnmapPicker(cfg config.Config, encoded string) ui.Picker {
	ref, err := decodeCatalogRef(encoded)
	options := []ui.Option{}
	if err == nil && ref.Kind == catalog.KindProject {
		names := cfg.Projects[ref.Name].Identities
		if len(cfg.Projects[ref.Name].ManualIdentities) > 0 {
			names = cfg.Projects[ref.Name].ManualIdentities
		}
		for _, name := range names {
			identity := cfg.Identities[name]
			options = append(options, ui.Option{Name: name, Label: identity.Account, Detail: name})
		}
		slices.SortFunc(options, func(left, right ui.Option) int { return strings.Compare(left.Label, right.Label) })
	}
	title := "Catalog › Projects › Unmap identity"
	if err == nil {
		title = "Catalog › " + catalogRefLabel(cfg, ref) + " › Unmap identity"
	}
	return ui.Picker{
		Screen: screenCatalogProjectUnmap, Title: title, Dimension: "catalog-target",
		Description: "Choose the identity mapping to remove. The complete change is shown once for confirmation before it is saved.", Options: options,
	}
}

func catalogDockerUnmapPicker(cfg config.Config, encoded string) ui.Picker {
	ref, err := decodeCatalogRef(encoded)
	options := []ui.Option{}
	if err == nil && ref.Kind == catalog.KindDocker {
		for name, workspace := range cfg.Destinations {
			if workspace.Docker == ref.Name {
				options = append(options, ui.Option{Name: name, Label: name, Detail: catalogNodeSummary(cfg, catalog.Ref{Kind: catalog.KindWorkspace, Name: name}), Risk: workspace.Risk})
			}
		}
		slices.SortFunc(options, func(left, right ui.Option) int { return strings.Compare(left.Label, right.Label) })
	}
	title := "Catalog › Docker › Unmap workspace"
	if err == nil {
		title = "Catalog › " + catalogRefLabel(cfg, ref) + " › Unmap workspace"
	}
	return ui.Picker{
		Screen: screenCatalogDockerUnmap, Title: title, Dimension: "catalog-target",
		Description: "Choose the workspace mapping to remove. The complete change is shown once for confirmation before it is saved.", Options: options,
	}
}

func catalogItemCount(count int, singular string) string {
	label := singular
	if count != 1 {
		label += "s"
	}
	return fmt.Sprintf("%d %s", count, label)
}

func catalogNeedsIdentityCount(count int) string {
	if count == 1 {
		return "1 needs identity"
	}
	return fmt.Sprintf("%d need identity", count)
}

func catalogIdentityPicker(cfg config.Config, identity catalog.Ref) ui.Picker {
	item := cfg.Identities[identity.Name]
	options := catalogActionOptions(cfg, identity)
	for _, node := range catalog.IdentityTree(cfg) {
		if node.Node.Ref != identity {
			continue
		}
		for _, project := range node.Children {
			options = append(options, ui.Option{Name: encodeCatalogRef(project.Node.Ref), Label: "Project: " + project.Node.Label, Detail: catalogNodeSummary(cfg, project.Node.Ref), Risk: project.Node.Risk})
		}
		break
	}
	return ui.Picker{
		Screen: screenCatalogIdentity, Title: "Catalog › Identities › " + item.Account,
		Description: catalogNodeDetail(cfg, identity), Dimension: "catalog", Options: options,
	}
}

func catalogProjectPicker(cfg config.Config, project catalog.Ref) ui.Picker {
	item := cfg.Projects[project.Name]
	options := catalogActionOptions(cfg, project)
	for _, node := range catalog.IdentityTree(cfg) {
		var branch *catalog.TreeNode
		if node.Node.Ref == project {
			branch = &node
		} else {
			for index := range node.Children {
				if node.Children[index].Node.Ref == project {
					branch = &node.Children[index]
					break
				}
			}
		}
		if branch == nil {
			continue
		}
		for _, target := range branch.Children {
			options = append(options, ui.Option{Name: encodeCatalogRef(target.Node.Ref), Label: "Kubernetes: " + target.Node.Label, Detail: catalogNodeSummary(cfg, target.Node.Ref), Risk: target.Node.Risk})
		}
		break
	}
	return ui.Picker{
		Screen: screenCatalogProject, Title: "Catalog › Projects › " + item.ProjectID,
		Description: catalogNodeDetail(cfg, project), Dimension: "catalog", Options: options,
	}
}
