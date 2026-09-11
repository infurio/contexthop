package main

import (
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/selection"
	"github.com/infurio/contexthop/internal/ui"
)

// Translate dialog-only sentinels and discovery results once at the boundary.
func selectionRequest(draft ui.Draft) selection.Request {
	request := draft.ContextSelection()
	request.Identity = firstNonEmpty(request.Identity, draft[screenProjectSearchIdentity])
	if searched := draft[screenProjectSearchResults]; (request.Project == searchProjectsSelection || request.Project == "") && searched != "" {
		request.Project = searched
	}
	if request.Project == noProjectSelection {
		request.Project = ""
	}
	if request.Kubernetes == noKubernetesSelection {
		request.Kubernetes = ""
	}
	return request
}

func selectionFromDraft(cfg config.Config, draft ui.Draft) (resolver.Selection, error) {
	return selectionRequest(draft).Components(cfg)
}

func resolveShellSelection(cfg config.Config, draft ui.Draft) (resolver.Resolved, error) {
	return selectionRequest(draft).Resolve(cfg)
}
