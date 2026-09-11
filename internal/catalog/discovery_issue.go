package catalog

import (
	"context"
	"errors"
	"github.com/infurio/contexthop/internal/config"
	"strings"
)

// ClassifyClusterDiscoveryError distinguishes unavailable GKE scopes from
// transport, authentication, and other operational failures. It grants no access.
func ClassifyClusterDiscoveryError(err error) config.DiscoveryIssue {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return config.DiscoveryScanFailed
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "billing_disabled") || strings.Contains(message, "billing_not_enabled") || strings.Contains(message, "requires billing to be enabled") || strings.Contains(message, "billing is disabled") || strings.Contains(message, "billing has not been enabled") {
		return config.DiscoveryBillingDisabled
	}
	if strings.Contains(message, "service_disabled") || ((strings.Contains(message, "kubernetes engine api") || strings.Contains(message, "container.googleapis.com")) && (strings.Contains(message, "is disabled") || strings.Contains(message, "has been disabled") || strings.Contains(message, "has not been used") || strings.Contains(message, "not enabled"))) {
		return config.DiscoveryGKEDisabled
	}
	if strings.Contains(message, "permission_denied") || strings.Contains(message, "permission denied") || strings.Contains(message, "required \"container.clusters.list\" permission") {
		return config.DiscoveryAccessDenied
	}
	return config.DiscoveryScanFailed
}
