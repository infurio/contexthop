package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

func projectSearchPickerAuthenticated(cfg, catalogCfg config.Config, identityName string, contexts ...context.Context) ui.Transition {
	identity, ok := cfg.Identities[identityName]
	if !ok {
		return ui.Transition{Picker: ui.Picker{Screen: screenProjectSearchResults, Title: "Projects › Search", Description: "Unknown identity.", Dimension: "project"}}
	}
	client, err := catalog.NewDefault()
	if err != nil {
		return ui.Transition{Picker: ui.Picker{Screen: screenProjectSearchResults, Title: "Projects › Search", Description: err.Error(), Dimension: "project"}}
	}
	ctx, cancel := context.WithTimeout(operationContext(contexts), 15*time.Second)
	result, refreshErr := client.RefreshProjects(ctx, identity)
	cancel()
	ids := make([]string, 0, len(result.Projects))
	for _, project := range result.Projects {
		ids = append(ids, project.ProjectID)
	}
	catalog.RecordDiscovery(cfg, identityName, "", ids, result.LastSuccess, refreshErr == nil && result.Freshness == catalog.FreshnessLive)
	if refreshErr == nil && result.Freshness == catalog.FreshnessLive {
		cloudauth.RecordStatus(identity, "Authenticated")
	} else if projectRefreshNeedsAuthentication(refreshErr) {
		cloudauth.RecordStatus(identity, "Sign-in required")
		return ui.Transition{PersistDiscovery: true, Picker: providerAuthPicker(cfg, "projects", identityName, "")}
	}
	options := make([]ui.Option, 0, len(result.Projects)+1)
	description := fmt.Sprintf("%s projects available to %s. This selection is used for this session and is not saved to the catalog.", strings.ToUpper(string(result.Freshness)), identity.Account)
	if refreshErr != nil {
		if projectRefreshNeedsAuthentication(refreshErr) {
			description = "Authentication is required for " + identity.Account + ". Select Authenticate and retry to refresh its isolated login."
			options = append(options, ui.Option{
				Name: authenticateProjectSearchPrefix + identityName, Project: true,
				ProjectID: "Authenticate and retry…", IdentityAccount: identity.Account, KubernetesContext: "required",
			})
		} else {
			description = discoveryFailureNotice(refreshErr)
			options = append(options, ui.Option{
				Name: retryProjectSearchPrefix + identityName, Project: true,
				ProjectID: "Retry project search…", IdentityAccount: identity.Account, KubernetesContext: "unavailable",
			})
		}
	}
	options = append(options, mergeProjectDiscovery(cfg, identityName, result)...)
	if refreshErr != nil && len(result.Projects) == 0 {
		if projectRefreshNeedsAuthentication(refreshErr) {
			return ui.Transition{PersistDiscovery: true, Picker: providerAuthPicker(cfg, "projects", identityName, "")}
		}
		description = strings.Replace(description, "Press d to retry", "Select Retry project search to try again", 1)
		picker := projectSearchRecoveryPicker(identity.Account, options[0], false, description)
		picker.OperationDetails = refreshErr.Error()
		picker.Description = identity.Account + "\n\n" + discoveryErrorReason(refreshErr) + "\n\nSelect Retry project search to try again. Saved records remain available."
		return ui.Transition{PersistDiscovery: true, Picker: picker}
	}
	transition := discoveredProjectsTransition(cfg, catalogCfg, identityName, options, refreshErr)
	if refreshErr != nil {
		transition.Picker.OperationDetails = refreshErr.Error()
		transition.Picker.Description = "Refresh failed · cached results. " + discoveryFailureNotice(refreshErr)
	} else {
		transition.Picker.Description = "Fetched just now · discovered projects saved automatically."
	}
	return transition
}

func discoveredProjectsTransition(cfg, catalogCfg config.Config, identityName string, results []ui.Option, refreshErr error) ui.Transition {
	picker := interactiveBrowserPicker(cfg, catalogCfg, ui.ScreenProject, ui.Draft{ui.ScreenIdentity: identityName})
	for _, option := range results {
		if strings.HasPrefix(option.Name, authenticateProjectSearchPrefix) || strings.HasPrefix(option.Name, retryProjectSearchPrefix) {
			picker.Options = append([]ui.Option{option}, picker.Options...)
		}
	}
	picker.Description = "Discovery complete. Unsaved = project not saved; Unmapped = selected identity not saved for this project. Press s to save. Enter continues to Kubernetes."
	if refreshErr != nil {
		picker.Description = discoveryFailureNotice(refreshErr)
	}
	if len(results) == 0 && refreshErr == nil {
		picker.Description = "No accessible projects found. Saved projects remain available; use n to create a project."
	}
	return ui.Transition{PersistDiscovery: true, ReplaceCurrent: true, Picker: picker, DraftUpdates: ui.Draft{
		ui.ScreenIdentity: identityName, ui.ScreenProject: "", ui.ScreenKubernetes: "", ui.ScreenWorkspace: "",
		screenProjectSearchIdentity: "", screenProjectSearchResults: "",
	}}
}

func projectSearchRecoveryPicker(account string, action ui.Option, authentication bool, description string) ui.Picker {
	title, label := "Project search unavailable", "Retry project search"
	if authentication {
		title, label = "Sign in to search projects", "Authenticate and retry"
		description = "The isolated Google login for " + account + " needs authentication. Continue to review the sign-in step, then retry project discovery."
	}
	return ui.Picker{
		Screen: screenProjectSearchResults, Title: title, Description: description,
		Dimension: "action", HideSearch: true, EnterLabel: "Continue",
		Options: []ui.Option{{Name: action.Name, Label: label}},
	}
}

func projectRefreshNeedsAuthentication(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"invalid_grant", "invalid_rapt", "token has been expired or revoked", "valid credentials", "gcloud auth login", "login required", "reauthenticate", "reauthentication", "authentication required", "cannot prompt during non-interactive execution"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func discoveryFailureNotice(err error) string {
	message := strings.ToLower(err.Error())
	reason := "Discovery failed: " + discoveryErrorReason(err)
	switch {
	case projectRefreshNeedsAuthentication(err):
		return "Google sign-in expired. Sign in from the identity actions, then press d to retry; saved records remain available."
	case strings.Contains(message, "deadline exceeded"), strings.Contains(message, "timeout"), strings.Contains(message, "timed out"):
		reason = "Google Cloud request timed out"
	case strings.Contains(message, "permission_denied"), strings.Contains(message, "permission denied"), strings.Contains(message, "403"):
		reason = "Google Cloud denied access; check this identity's permissions"
	case strings.Contains(message, "connection"), strings.Contains(message, "no such host"), strings.Contains(message, "network"):
		reason = "Could not connect to Google Cloud"
	}
	return reason + ". Press d to retry; saved records remain available."
}

// Keep the provider's actual reason visible, while preserving the complete
// original diagnostic separately for the scrollable details view.
func discoveryErrorReason(err error) string {
	message := err.Error()
	if _, detail, ok := strings.Cut(message, "ERROR:"); ok {
		message = strings.TrimSpace(detail)
		if strings.HasPrefix(message, "(") {
			if _, detail, ok := strings.Cut(message, ")"); ok {
				message = detail
			}
		}
	} else if strings.HasPrefix(message, "gcloud: exit status ") {
		if _, detail, ok := strings.Cut(strings.TrimPrefix(message, "gcloud: exit status "), ":"); ok {
			message = detail
		}
	}
	message = strings.Join(strings.Fields(message), " ")
	characters := []rune(message)
	if len(characters) > 280 {
		message = string(characters[:277]) + "…"
	}
	return message
}

func projectSearchPicker(cfg, catalogCfg config.Config, identityName string) ui.Transition {
	return authenticatedOperation(context.Background(), cfg, identityName, "Discovery", nil, func(ctx context.Context, _ func(string)) ui.Transition {
		return projectSearchPickerAuthenticated(cfg, catalogCfg, identityName, ctx)
	})
}
