package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func providerAuthPicker(cfg config.Config, scope, identityName, projectName string, failure ...string) ui.Picker {
	identity, ok := cfg.Identities[identityName]
	if !ok {
		return ui.Picker{Screen: screenProviderAuth, Title: "Authentication unavailable", Description: "Unknown identity " + identityName + ".", Dimension: "provider-auth", HideSearch: true}
	}
	description := "Choose how to sign in. Browser sign-in supports your password manager. Terminal sign-in lets gcloud handle password and hardware-key prompts; Google may still require a browser. Discovery retries automatically when you finish."
	if scope == "identity" {
		description = "Manage credentials for this identity. Your shell selection stays unchanged."
	}
	if scope == "discovery" {
		description = "Log in, then retry with the same discovery identity, project and scope."
	}
	if len(failure) > 0 && failure[0] != "" {
		description = "Operation did not complete: " + strings.Join(strings.Fields(failure[0]), " ") + "\n\n" + description
	}
	detail := "authenticate only the isolated credentials for this account"
	if projectName != "" {
		detail += " · project " + cfg.Projects[projectName].ProjectID
	}
	picker := ui.Picker{
		Screen: screenProviderAuth, Title: "Authenticate Google › " + identity.Account, Description: description,
		Dimension: "provider-auth", HideSearch: true, CompactDialog: scope == "discovery", EnterLabel: "Authenticate",
		Options: []ui.Option{
			{Name: providerAuthPrefix + scope + "\x00" + identityName + "\x00" + projectName + "\x00browser", Label: "Sign in with browser", Detail: "open a fresh Google sign-in"},
			{Name: providerAuthPrefix + scope + "\x00" + identityName + "\x00" + projectName + "\x00terminal", Label: "Sign in with terminal", Detail: detail},
		},
	}
	if scope == "identity" {
		picker.Title = "Authentication › " + identity.Account + " [" + identityName + "]"
		picker.ActionRows = true
		picker.Options[0].Label = "CLI login · browser"
		picker.Options[0].Detail = "Open a fresh Google sign-in for gcloud commands."
		picker.Options[1].Label = "CLI login · terminal"
		picker.Options[1].Detail = "Reuse CLI credentials if valid. Google may request a browser."
		picker.Options = append(picker.Options, ui.Option{Name: providerAuthPrefix + scope + "\x00" + identityName + "\x00\x00adc", Label: "ADC login", Detail: "Prepare credentials for Terraform and application code. No CLI login required."})
		picker.Options = append(picker.Options, ui.Option{Name: providerAuthPrefix + scope + "\x00" + identityName + "\x00\x00logout", Label: "CLI logout", Detail: "Revoke CLI credentials. ADC files are retained.", WorkLabel: "Logging out " + identity.Account + " [" + identityName + "]"})
		picker.Options = append(picker.Options, ui.Option{Name: providerAuthPrefix + scope + "\x00" + identityName + "\x00\x00adc-logout", Label: "ADC logout", Detail: "Revoke ADC for all shells and applications using it. Shared CLI credentials may also stop working.", WorkLabel: "Logging out ADC for " + identity.Account})
	}
	return picker
}

func providerAuthTransition(selectionCfg, catalogCfg config.Config, encoded string, editor *catalogEditorState, drafts ...ui.Draft) ui.Transition {
	value := strings.TrimPrefix(encoded, providerAuthPrefix)
	scope, remainder, ok := strings.Cut(value, "\x00")
	if !ok {
		return catalogPreviewError(errors.New("invalid provider authentication request"), editor)
	}
	identityName, projectName, ok := strings.Cut(remainder, "\x00")
	if !ok {
		return catalogPreviewError(errors.New("invalid provider authentication scope"), editor)
	}
	projectName, mode, _ := strings.Cut(projectName, "\x00")
	activeCfg := catalogCfg
	if scope == "projects" || scope == "browser-clusters" || scope == "identity" || scope == "discovery" {
		activeCfg = selectionCfg
	}
	identity, ok := activeCfg.Identities[identityName]
	if !ok {
		return catalogPreviewError(fmt.Errorf("unknown identity %q", identityName), editor)
	}
	draft := ui.Draft{}
	if len(drafts) > 0 {
		draft = cloneApplicationDraft(drafts[0])
	}
	returnToIdentity := func(notice string) ui.Transition {
		picker := interactiveBrowserPicker(activeCfg, catalogCfg, ui.ScreenIdentity, draft)
		picker.Focus = identityName
		picker.Description = notice + " · " + identity.Account + " [" + identityName + "]"
		return ui.Transition{ReplaceCurrent: true, Picker: picker}
	}
	if scope == "identity" && mode == "adc" {
		return identityADCTransition(activeCfg, catalogCfg, identityName, draft)
	}
	if scope == "identity" && mode == "adc-logout" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := cloudauth.LogoutADC(ctx, identityName, identity); err != nil {
			return ui.Transition{ReplaceCurrent: true, Picker: providerAuthPicker(activeCfg, scope, identityName, projectName, err.Error())}
		}
		return returnToIdentity("ADC logged out. Shared CLI credentials may need login again")
	}
	if scope == "identity" && mode == "logout" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := cloudauth.Logout(ctx, identityName, identity); err != nil {
			return ui.Transition{ReplaceCurrent: true, Picker: providerAuthPicker(activeCfg, scope, identityName, projectName, err.Error())}
		}
		return returnToIdentity("Logged out")
	}
	login := cloudauth.LoginCommand
	if mode == "browser" {
		login = cloudauth.BrowserLoginCommand
	}
	command, _, err := login(context.Background(), identityName, identity)
	if err != nil {
		return ui.Transition{ReplaceCurrent: true, Picker: providerAuthPicker(activeCfg, scope, identityName, projectName, err.Error())}
	}
	return ui.Transition{Process: &ui.Process{
		Command: command,
		Done: func(processErr error) ui.Transition {
			if processErr != nil {
				return ui.Transition{ReplaceCurrent: true, Picker: providerAuthPicker(activeCfg, scope, identityName, projectName, processErr.Error())}
			}
			cloudauth.RecordStatus(identity, "Authenticated")
			switch scope {
			case "discovery":
				return ui.Transition{ReturnToPrevious: true, Picker: discoveryDialog(activeCfg, draft, "Logged in. Start discovery to retry these settings.")}
			case "identity":
				return returnToIdentity("Logged in")
			case "browser-clusters":
				transition := discoverBrowserClusters(activeCfg, catalogCfg, identityName, projectName)
				transition.ReplaceCurrent = true
				return transition
			case "gke":
				if editor.gke == nil {
					return ui.Transition{ReplaceCurrent: true, Picker: providerAuthPicker(activeCfg, scope, identityName, projectName, "GKE draft was lost")}
				}
				return ui.Transition{ReturnToPrevious: true, Picker: catalogClusterDiscoveryPicker(activeCfg, projectName, identityName).Picker}
			case "projects":
				transition := projectSearchPicker(activeCfg, catalogCfg, identityName)
				if !transition.ReplaceCurrent {
					transition.ReplaceCurrent = true
				}
				return transition
			default:
				return ui.Transition{ReplaceCurrent: true, Picker: providerAuthPicker(activeCfg, scope, identityName, projectName, "unsupported retry scope")}
			}
		},
	}}
}
