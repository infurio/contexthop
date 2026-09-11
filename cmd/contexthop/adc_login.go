package main

import (
	"context"
	"fmt"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

// Interactive launches can recover ADC independently of CLI authentication.
// The activation gate still rechecks the principal immediately before commit.
func authenticatedADCLaunch(ctx context.Context, resolved resolver.Resolved, launch ui.Choice, report func(string)) ui.Transition {
	return authenticatedADCOperation(ctx, resolved, report, func() ui.Transition { return ui.Transition{Complete: true, CompletionChoice: &launch} })
}

func authenticatedADCOperation(ctx context.Context, resolved resolver.Resolved, report func(string), complete func() ui.Transition) ui.Transition {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ui.Transition{Dismiss: true}
	}
	if report != nil && resolved.ADCMode != "" {
		report("Checking application credentials")
	}
	if err := verifyLaunchADC(ctx, resolved); err != nil {
		if ctx.Err() != nil {
			return ui.Transition{Dismiss: true}
		}
		return adcLoginGate(resolved, complete, err.Error())
	}
	return complete()
}

func adcLoginGate(resolved resolver.Resolved, complete func() ui.Transition, failure string) ui.Transition {
	picker := ui.Picker{Screen: "adc-authentication-required", Title: "Application credentials required", CompactDialog: true, HideSearch: true,
		Description: failure + fmt.Sprintf("\n\nPrepare application credentials as %s. Existing credentials will be reused where possible; Google may request a browser sign-in.", resolved.Identity.Account),
		Options:     []ui.Option{{Name: "login", Label: "Log in for application credentials (ADC)"}, {Name: "cancel", Label: "Cancel"}}}
	picker.Flow = func(choice ui.Choice, _ ui.Draft) ui.Transition {
		if choice.Option.Name == "cancel" {
			return ui.Transition{Dismiss: true}
		}
		retry := func(err error) ui.Transition {
			next := adcLoginGate(resolved, complete, "ADC login did not complete: "+err.Error())
			next.ReplaceCurrent = true
			return next
		}
		command, _, err := cloudauth.LoginADCCommand(context.Background(), resolved.IdentityName, *resolved.Identity)
		if err != nil {
			return retry(err)
		}
		return ui.Transition{Process: &ui.Process{Command: command, Cancellable: true, Label: "Application credentials for " + resolved.Identity.Account,
			Continue: func(ctx context.Context, report func(string), err error) ui.Transition {
				if err != nil {
					return retry(err)
				}
				if err := cloudauth.CompleteADCLogin(*resolved.Identity); err != nil {
					return retry(err)
				}
				next := authenticatedADCOperation(ctx, resolved, report, complete)
				next.ReplaceCurrent = true
				return next
			}}}
	}
	return ui.Transition{Picker: picker}
}

func identityADCTransition(cfg, catalogCfg config.Config, name string, draft ui.Draft) ui.Transition {
	identity, ok := cfg.Identities[name]
	if !ok {
		return ui.Transition{Picker: ui.Picker{Title: "Identity unavailable", Description: "Choose an identity before preparing ADC.", HideSearch: true, DisableEnter: true}}
	}
	selected := cloneApplicationDraft(draft)
	resolved := resolver.Resolved{IdentityName: name, Identity: &identity, ADCMode: "identity"}
	return adcLoginGate(resolved, func() ui.Transition {
		picker := interactiveBrowserPicker(cfg, catalogCfg, ui.ScreenIdentity, selected)
		picker.Focus = name
		picker.Description = "Application credentials verified for " + identity.Account
		return ui.Transition{ReplaceCurrent: true, Picker: picker}
	}, "")
}
