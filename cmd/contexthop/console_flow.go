package main

import (
	"fmt"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/destination"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

// Web navigation resolves within the originating application. It neither
// authenticates cloud tools nor changes the pending shell selection.
func webConsoleFlow(cfg config.Config, target launchTarget, identity string) ui.Transition {
	if !destination.HasWebConsole(cfg, target) {
		return webConsoleResult(fmt.Errorf("no web console is available for this destination"), false)
	}
	if identity != "" && (target.Kind == "project" || target.Kind == "kubernetes") {
		if resolved, err := destination.Resolve(cfg, target, identity); err == nil {
			return webConsoleProcess(resolved, false)
		}
	}
	resolution := destination.Prepare(cfg, target)
	if resolution.Err == nil {
		return webConsoleProcess(resolution.Resolved, false)
	}
	if len(resolution.Candidates) == 0 {
		return webConsoleResult(resolution.Err, false)
	}
	picker := launchIdentityPicker(cfg, target, resolution.Candidates)
	picker.Screen = "web-console-identity"
	picker.Description = "Choose the account whose browser profile should open the web console."
	picker.Flow = func(choice ui.Choice, _ ui.Draft) ui.Transition {
		resolved, err := destination.Resolve(cfg, target, choice.Option.Name)
		if err != nil {
			return webConsoleResult(err, true)
		}
		return webConsoleProcess(resolved, true)
	}
	return ui.Transition{Picker: picker}
}

func webConsoleProcess(resolved resolver.Resolved, replace bool) ui.Transition {
	command, err := prepareConsoleCommand(resolved, "details")
	if err != nil {
		return webConsoleResult(err, replace)
	}
	return ui.Transition{Process: &ui.Process{Label: "Opening web console", Command: command, Done: func(err error) ui.Transition { return webConsoleResult(err, replace) }}}
}

func webConsoleResult(err error, replace bool) ui.Transition {
	message := "Web console opened in the identity’s browser profile."
	if err != nil {
		message = "Could not open web console: " + err.Error()
	}
	return ui.Transition{ReplaceCurrent: replace, Picker: ui.Picker{Screen: "console-result", Title: "Web console", Description: message, HideSearch: true, DisableEnter: true}}
}
