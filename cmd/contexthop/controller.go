package main

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/infurio/contexthop/internal/appstate"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/recency"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/session"
	"github.com/infurio/contexthop/internal/ui"
)

// applicationController owns accepted application state. Prepare and process
// callbacks work on private snapshots; Accept is called only by the UI loop.
type applicationController struct {
	autoSaveDiscovery bool
	resources         appstate.Catalog
	editor            catalogEditorState
	path              string
	history           recency.History
	sessions          []session.Active
	apply             func(string, catalog.Plan) error
}

type applicationResult struct {
	resources appstate.Catalog
	editor    catalogEditorState
}

func newApplicationController(path string, saved config.Config, history recency.History, sessions []session.Active) *applicationController {
	return &applicationController{resources: appstate.New(saved), path: path, history: history, sessions: slices.Clone(sessions), apply: catalog.Apply}
}

func (c *applicationController) Accept(value any) {
	result := value.(applicationResult)
	// Process callbacks may continue using their original working snapshot.
	c.resources, c.editor = result.resources.Clone(), cloneCatalogEditor(result.editor)
}

func (c *applicationController) Browser(screen ui.Screen, draft ui.Draft) ui.Picker {
	picker := interactiveBrowserPicker(c.resources.Selection(), c.resources.Saved(), screen, draft)
	orderResourcePicker(&picker)
	return picker
}

func (c *applicationController) Pickers() map[ui.Screen]ui.Picker {
	return c.pickers(c.resources)
}

func (c *applicationController) pickers(resources appstate.Catalog) map[ui.Screen]ui.Picker {
	pickers := interactivePickers(resources.Selection(), resources.Saved(), c.sessions)
	for screen, picker := range pickers {
		orderResourcePicker(&picker)
		pickers[screen] = picker
	}
	return pickers
}

func (c *applicationController) Prepare(choice ui.Choice, draft ui.Draft) ui.Transition {
	resources := c.resources.Clone()
	view := resources.Selection()
	editor := cloneCatalogEditor(c.editor)
	transition := interactiveFlow(view, resources.Saved(), &editor)(choice, cloneApplicationDraft(draft))
	return c.finish(resources, view, &editor, transition, choice, draft)
}

func (c *applicationController) finish(resources appstate.Catalog, view config.Config, editor *catalogEditorState, transition ui.Transition, choice ui.Choice, draft ui.Draft) ui.Transition {
	resources.Observe(view)
	merged := cloneApplicationDraft(draft)
	for screen, value := range transition.DraftUpdates {
		merged[screen] = value
	}
	if transition.PersistDiscovery && c.autoSaveDiscovery {
		plan := catalog.PlanSaveDiscovery(resources.Saved(), view)
		if !plan.Valid() {
			return catalogPreviewError(errors.New(strings.Join(plan.Problems, "; ")), editor)
		}
		if len(plan.Changes) > 0 {
			if err := c.apply(c.path, plan); err != nil {
				return catalogPreviewError(err, editor)
			}
		}
		resources.SavedChange(plan.Config)
		screen := transition.Picker.Screen
		notice := transition.Picker.Description
		details := transition.Picker.OperationDetails
		statusKind, operationActions := transition.Picker.StatusKind, transition.Picker.OperationActions
		operationDraft := transition.Picker.OperationDraft
		if transition.Picker.ResourceBrowser {
			transition.Picker = interactiveBrowserPicker(resources.Selection(), resources.Saved(), screen, merged)
		}
		if screen == ui.ScreenProject && (strings.HasPrefix(notice, "Discovery complete.") || strings.HasPrefix(notice, "Project discovery complete.")) {
			notice = "Discovery complete. Projects and identity mappings saved."
		}
		transition.Picker.Description = notice
		transition.Picker.OperationDetails = details
		transition.Picker.StatusKind, transition.Picker.OperationActions = statusKind, operationActions
		transition.Picker.OperationDraft = operationDraft
	}
	if handler := transition.Picker.Flow; handler != nil {
		transition.Picker.Flow = func(next ui.Choice, nextDraft ui.Draft) ui.Transition {
			return c.finish(resources.Clone(), view, editor, handler(next, nextDraft), next, nextDraft)
		}
	}
	if transition.Process != nil {
		process := *transition.Process
		continuation := process.Continue
		if continuation != nil {
			process.Continue = func(ctx context.Context, report func(string), err error) ui.Transition {
				return c.finish(resources.Clone(), view, editor, continuation(ctx, report, err), choice, merged)
			}
		}
		done := process.Done
		process.Done = func(err error) ui.Transition {
			if done == nil {
				return ui.Transition{}
			}
			return c.finish(resources.Clone(), view, editor, done(err), choice, merged)
		}
		transition.Process = &process
	}
	if transition.Complete && choice.Screen == ui.ScreenConfirm && choice.Option.Name == "cancel" {
		if editor.workspaceReview && editor.workspace != nil {
			editor.plan, editor.err = nil, nil
			editor.selectionWorkspace = ""
			transition = ui.Transition{ReturnToPrevious: true, Picker: workspaceBuilderPicker(resources.Saved(), editor.workspace)}
		} else {
			*editor = catalogEditorState{}
			transition = c.returnToBrowser(resources, merged, catalogPostApply{Entity: catalogDraftEntity(merged)})
			// Restore the original browser frame, including search, cursor and selection.
			transition.PreservePosition = true
			// Also preserve the setting for consumers without a browser frame.
			transition.DraftUpdates[ui.ScreenShellADCOverride] = merged[ui.ScreenShellADCOverride]
		}
	} else if transition.Complete && (editor.applyImmediately || catalogVisibilityPlan(editor.plan) || choice.Screen == ui.ScreenConfirm && choice.Option.Name == "apply") {
		if editor.err != nil {
			transition = catalogPreviewError(editor.err, editor)
		} else if editor.plan == nil {
			transition = catalogPreviewError(errors.New("no catalog change was prepared"), editor)
		} else {
			plan := *editor.plan
			if err := c.apply(c.path, plan); err != nil {
				// Failed writes leave the browser open and the saved snapshot intact.
				transition = catalogPreviewError(err, editor)
			} else {

				returnTo := catalogPostApplyDestination(plan, merged)
				if editor.savingProject {
					returnTo.Notice = "Project and identity mapping saved."
				}
				resources.SavedChange(plan.Config)
				if editor.selectionWorkspace != "" {
					if resolved, err := resolver.Destination(plan.Config, editor.selectionWorkspace); err == nil {
						for key, value := range workspaceSelectionDraft(resolved) {
							merged[key] = value
						}
					}
				}
				*editor = catalogEditorState{}
				transition = c.returnToBrowser(resources, merged, returnTo)

			}
		}
	}
	orderResourcePicker(&transition.Picker)
	transition.Result = applicationResult{resources: resources, editor: cloneCatalogEditor(*editor)}
	return transition
}

func (c *applicationController) returnToBrowser(resources appstate.Catalog, draft ui.Draft, target catalogPostApply) ui.Transition {
	view := resources.Selection()
	selection := ui.Draft{}
	for _, screen := range []ui.Screen{ui.ScreenIdentity, ui.ScreenProject, ui.ScreenKubernetes, ui.ScreenDocker, ui.ScreenWorkspace} {
		name := draft[screen]
		if name != "" && !strings.HasPrefix(name, "\x00__") && catalogRefExists(view, catalog.Ref{Kind: catalog.Kind(screen), Name: name}) {
			selection[screen] = name
		}
	}
	if name := draft[ui.ScreenWorkspaceSource]; name != "" {
		if _, exists := view.Destinations[name]; exists {
			selection[ui.ScreenWorkspaceSource] = name
		}
	}
	// Removing a dependency invalidates downstream navigation selections.
	if draft[ui.ScreenProject] != "" && selection[ui.ScreenProject] == "" {
		delete(selection, ui.ScreenKubernetes)
	}
	screen, focus := ui.ScreenProject, ""
	if ref, err := decodeCatalogRef(target.Entity); err == nil {
		screen, focus = resourceScreenForKind(ref.Kind), ref.Name
	} else if target.Category != "" {
		screen = resourceScreenForCategory(target.Category)
	}
	picker := interactiveBrowserPicker(view, resources.Saved(), screen, selection)
	if target.Category == "hidden" {
		picker = catalogCategoryPicker(resources.Saved(), "hidden")
	}
	picker.Focus, picker.Description = focus, target.Notice
	updates := ui.Draft{}
	for key := range draft {
		updates[key] = ""
	}
	for key, value := range selection {
		updates[key] = value
	}

	return ui.Transition{ResetNavigation: true, Picker: picker, Pickers: c.pickers(resources), DraftUpdates: updates}
}

func cloneApplicationDraft(draft ui.Draft) ui.Draft {
	copy := ui.Draft{}
	for key, value := range draft {
		copy[key] = value
	}
	return copy
}

func cloneCatalogEditor(editor catalogEditorState) catalogEditorState {
	if editor.plan != nil {
		plan := *editor.plan
		plan.Config = plan.Config.Clone()
		plan.Changes, plan.Impacts, plan.Problems = slices.Clone(plan.Changes), slices.Clone(plan.Impacts), slices.Clone(plan.Problems)
		editor.plan = &plan
	}
	if editor.gke != nil {
		value := *editor.gke
		editor.gke = &value
	}
	if editor.workspace != nil {
		value := *editor.workspace
		editor.workspace = &value
	}
	return editor
}
