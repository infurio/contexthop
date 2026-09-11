package main

import (
	"context"
	"fmt"
	"os"
	"time"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

// A gate owns its continuation, so authentication cannot lose the selected
// identity, discovery scope, project, or pending guided operation.
func authenticatedOperation(ctx context.Context, cfg config.Config, name string, label string, report func(string), run func(context.Context, func(string)) ui.Transition, requireADC ...bool) ui.Transition {
	if label == "Discovery" {
		if blocked, ok := discoveryBlocked(); ok {
			return blocked
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if report == nil {
		report = func(string) {}
	}
	identity, ok := cfg.Identities[name]
	if !ok {
		return ui.Transition{Picker: ui.Picker{Screen: "authentication-error", Title: "Identity unavailable", Description: "Choose an identity before continuing.", CompactDialog: true, HideSearch: true, DisableEnter: true}}
	}
	report("Checking login for " + identity.Account + " [" + name + "]")
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	err := cloudauth.Check(checkCtx, resolver.Resolved{IdentityName: name, Identity: &identity})
	cancel()
	if ctx.Err() != nil {
		return ui.Transition{ReplaceCurrent: true, Picker: ui.Picker{Screen: "authentication-cancelled", Title: "Operation stopped", Description: "Stopped before the cloud operation started.", CompactDialog: true, HideSearch: true, DisableEnter: true}}
	}
	if err == nil {
		return run(ctx, report)
	}
	updateADC := false
	if len(requireADC) > 0 && requireADC[0] {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		updateADC = cloudauth.CheckADC(checkCtx, resolver.Resolved{IdentityName: name, Identity: &identity, ADCMode: "identity"}) != nil
		cancel()
		if ctx.Err() != nil {
			return ui.Transition{Dismiss: true}
		}
	}
	return authenticationGate(cfg, name, label, run, "", updateADC)
}

func authenticationGate(cfg config.Config, name, label string, run func(context.Context, func(string)) ui.Transition, failure string, updateADC ...bool) ui.Transition {
	identity := cfg.Identities[name]
	description := fmt.Sprintf("Log in as %s [%s] to continue. %s resumes automatically after login.", identity.Account, name, label)
	combined := len(updateADC) > 0 && updateADC[0]
	if combined {
		description += " This login prepares both gcloud and application credentials (ADC)."
	}
	if failure != "" {
		description = failure + "\n\n" + description
	}
	picker := ui.Picker{Screen: "authentication-required", Title: "Login required", Description: description, CompactDialog: true, HideSearch: true, Options: []ui.Option{{Name: "browser", Label: "Log in with browser"}, {Name: "terminal", Label: "Log in with terminal"}, {Name: "cancel", Label: "Cancel"}}}
	picker.Flow = func(choice ui.Choice, _ ui.Draft) ui.Transition {
		if choice.Option.Name == "cancel" {
			return ui.Transition{Dismiss: true}
		}
		login := cloudauth.BrowserLoginCommand
		if choice.Option.Name == "terminal" {
			login = cloudauth.LoginCommand
		}
		command, _, err := login(context.Background(), name, identity)
		if err != nil {
			next := authenticationGate(cfg, name, label, run, err.Error(), combined)
			next.ReplaceCurrent = true
			return next
		}
		if combined {
			command.Args = append(command.Args, "--update-adc")
		}
		return ui.Transition{Process: &ui.Process{Command: command, Cancellable: true, Label: label + " as " + identity.Account, Continue: func(ctx context.Context, report func(string), err error) ui.Transition {
			if err == nil && combined {
				err = cloudauth.CompleteADCLogin(identity)
			}
			if err != nil {
				next := authenticationGate(cfg, name, label, run, "Login did not complete: "+err.Error(), combined)
				next.ReplaceCurrent = true
				return next
			}
			next := authenticatedOperation(ctx, cfg, name, label, report, run, combined)
			next.ReplaceCurrent = true
			return next
		}}}
	}
	return ui.Transition{Picker: picker}
}

// Command-line operations cannot suspend into a TUI login dialog.
func ensureIdentityLogin(ctx context.Context, name string, identity config.Identity) error {
	check := func() error {
		checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		return cloudauth.Check(checkCtx, resolver.Resolved{IdentityName: name, Identity: &identity})
	}
	if err := check(); err == nil {
		return nil
	} else if ctx.Err() != nil {
		return ctx.Err()
	} else if !isTerminal(os.Stdin) {
		return fmt.Errorf("cannot verify login for %s [%s]: %w; run chop auth %s, then retry", identity.Account, name, err, name)
	}
	fmt.Fprintf(os.Stderr, "Login required for %s [%s]. The operation will resume after login.\n", identity.Account, name)
	if err := cloudauth.Login(ctx, name, identity); err != nil {
		return err
	}
	return check()
}

func operationContext(contexts []context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background()
}
