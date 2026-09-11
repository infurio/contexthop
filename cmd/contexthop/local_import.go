package main

import (
	"context"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/discovery"
	"github.com/infurio/contexthop/internal/ui"
)

const screenLocalImport ui.Screen = "import-local-contexts"

func localImportFlow(cfg config.Config, choice ui.Choice, editor *catalogEditorState) (ui.Transition, bool) {
	if choice.Action == "import-local-contexts" {
		if blocked, ok := discoveryBlocked(); ok {
			return blocked, true
		}
	} else if choice.Screen != screenLocalImport {
		return ui.Transition{}, false
	} else if choice.Option.Name == "cancel" {
		return ui.Transition{Dismiss: true}, true
	}

	ctx, cancel := context.WithTimeout(operationContext([]context.Context{choice.Context}), 30*time.Second)
	defer cancel()
	observations, err := discovery.ObserveLocal(ctx)
	if err != nil {
		return catalogPreviewError(err, editor), true
	}
	normalized, err := discovery.Normalize(observations)
	if err != nil {
		return catalogPreviewError(err, editor), true
	}
	plan, err := catalog.PlanImportLocal(cfg, normalized.Config)
	if err == nil && len(plan.Changes) == 0 {
		return ui.Transition{ReplaceCurrent: choice.Screen == screenLocalImport, Picker: ui.Picker{Screen: "local-import-results", Title: "Local contexts are up to date", ReadOnlyText: "No catalog changes needed.\n" + strings.Join(normalized.Warnings, "\n"), HideSearch: true}}, true
	}
	tr := catalogPreviewTransition(plan, err, editor)
	if len(normalized.Warnings) > 0 {
		tr.Picker.Description += "\n\n" + strings.Join(normalized.Warnings, "\n")
	}
	return tr, true
}
