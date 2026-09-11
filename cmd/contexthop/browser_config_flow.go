package main

import (
	"fmt"

	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"

	"github.com/infurio/contexthop/internal/ui"
)

const browserIdentity ui.Screen = "configure-browser-identity"
const browserApp ui.Screen = "configure-browser-app"
const browserDirectory ui.Screen = "configure-browser-directory"
const screenBrowserConfigOrigin ui.Screen = "browser-config-origin"

func browserConfigFlow(cfg config.Config, choice ui.Choice, draft ui.Draft, editor *catalogEditorState) (ui.Transition, bool) {
	if choice.Action == "configure-browser" {
		if choice.Screen == ui.ScreenIdentity {
			identity, exists := cfg.Identities[choice.Option.Name]
			if !exists {
				return catalogPreviewError(fmt.Errorf("choose an identity first"), editor), true
			}
			picker := browserAppPicker()
			picker.Description = "Browser profile for " + identity.Account
			picker.Focus = identity.Browser.App
			if identity.Browser.Profile == "" {
				picker.Options = picker.Options[:2]
			}
			return ui.Transition{Picker: picker, DraftUpdates: ui.Draft{browserIdentity: choice.Option.Name, screenBrowserConfigOrigin: string(choice.Screen)}}, true
		}
		options := identityOptions(cfg, nil, true)
		for index := range options {
			identity := cfg.Identities[options[index].Name]
			options[index].Detail = "No browser profile configured"
			if identity.Browser.Profile != "" {
				options[index].Detail = identity.Browser.App + " / " + identity.Browser.Profile
			}
		}
		description := "Choose the account whose browser profile you want to configure."
		if len(options) == 0 {
			description = "No identities are saved. Add an identity in the Identities tab first."
		}
		return ui.Transition{Picker: ui.Picker{Screen: browserIdentity, Title: "Configure browser profile", Description: description, Options: options, DisableEnter: len(options) == 0}, DraftUpdates: ui.Draft{screenBrowserConfigOrigin: string(choice.Screen)}}, true
	}
	if choice.Screen == browserIdentity {
		return ui.Transition{Picker: browserAppPicker()}, true
	}
	if choice.Screen == browserApp {
		if choice.Option.Name == "clear" {
			plan, err := catalog.PlanBrowser(cfg, draft[browserIdentity], config.BrowserProfile{})
			return catalogPreviewTransition(plan, err, editor), true
		}
		picker := catalogInputPicker(browserDirectory, "Browser profile directory", "Profile directory", "Profile 1", "Use the final directory of Profile Path on chrome://version or edge://version. Sign into the intended account in that profile.")
		picker.Input.Initial = cfg.Identities[draft[browserIdentity]].Browser.Profile
		picker.Input.Validate = func(value string) error {
			return (config.BrowserProfile{App: choice.Option.Name, Profile: value}).Validate()
		}
		return ui.Transition{Picker: picker}, true
	}
	if choice.Screen == browserDirectory {
		plan, err := catalog.PlanBrowser(cfg, draft[browserIdentity], config.BrowserProfile{App: draft[browserApp], Profile: choice.Option.Name})
		return catalogPreviewTransition(plan, err, editor), true
	}

	if choice.Action == "open-console" {
		return webConsoleFlow(cfg, launchTarget{Kind: string(choice.Screen), Name: choice.Option.Name}, draft[ui.ScreenIdentity]), true
	}

	return ui.Transition{}, false
}

func browserAppPicker() ui.Picker {
	return ui.Picker{Screen: browserApp, Title: "Choose browser", HideSearch: true, CompactDialog: true, Options: []ui.Option{{Name: "chrome", Label: "Google Chrome"}, {Name: "edge", Label: "Microsoft Edge"}, {Name: "clear", Label: "Remove browser binding"}}}
}
