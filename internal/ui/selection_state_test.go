package ui

import (
	"testing"

	"github.com/infurio/contexthop/internal/selection"
)

func TestTypedSelectionExcludesAndPreservesDialogInputs(t *testing.T) {
	draft := Draft{ScreenIdentity: "work", ScreenProject: "project", ScreenDocker: "local",
		ScreenWorkspaceSource: "source", ScreenShellADCOverride: "identity", ScreenInput: "typed name", "operation-identity": "other"}
	staged := draft.ContextSelection()
	staged.Unstage(selection.Identity)
	draft.setContextSelection(staged)
	if draft[ScreenInput] != "typed name" || draft["operation-identity"] != "other" {
		t.Fatal("selection operation changed dialog inputs", draft)
	}
	if draft[ScreenIdentity] != "" || draft[ScreenProject] != "" || draft[ScreenDocker] != "local" {
		t.Fatal("wrong staged context", draft)
	}
}
