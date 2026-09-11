package config

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"time"
)

// DiscoveryScope is the last complete enumeration for one credential profile
// and scope. Failed refreshes retain Resources and mark that evidence stale.
// Resource IDs are provider IDs, not editable catalog names.
type DiscoveryScope struct {
	Issue      DiscoveryIssue `yaml:"issue,omitempty"`
	Resources  []string       `yaml:"resources,omitempty"`
	ObservedAt string         `yaml:"observedAt,omitempty"`
	Stale      bool           `yaml:"stale,omitempty"`
}

type IdentityDiscovery struct {
	Profile  string                    `yaml:"profile"`
	Projects *DiscoveryScope           `yaml:"projects,omitempty"`
	Clusters map[string]DiscoveryScope `yaml:"clusters,omitempty"`
}

func DiscoveryProfile(identity Identity) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity.Provider+"\x00"+identity.Kind+"\x00"+identity.Account+"\x00"+identity.CloudSDKConfig)))
}

func (c Config) DiscoveryFor(name string) IdentityDiscovery {
	identity, exists := c.Identities[name]
	item := c.Discovery[name]
	if !exists || item.Profile != DiscoveryProfile(identity) {
		return IdentityDiscovery{}
	}
	return item
}

func CloneDiscovery(source map[string]IdentityDiscovery) map[string]IdentityDiscovery {
	result := make(map[string]IdentityDiscovery, len(source))
	for name, item := range source {
		if item.Projects != nil {
			scope := *item.Projects
			scope.Resources = slices.Clone(scope.Resources)
			item.Projects = &scope
		}
		scopes := make(map[string]DiscoveryScope, len(item.Clusters))
		for key, scope := range item.Clusters {
			scope.Resources = slices.Clone(scope.Resources)
			scopes[key] = scope
		}
		item.Clusters = scopes
		result[name] = item
	}
	return result
}

func (s DiscoveryScope) IsStale() bool {
	observed, err := time.Parse(time.RFC3339, s.ObservedAt)
	return s.Stale || err != nil || time.Since(observed) > 24*time.Hour
}

// DiscoveryIssue describes the latest unsuccessful scan without discarding the
// last successful enumeration or confusing an attempted scan with no scan.
type DiscoveryIssue string

const (
	DiscoveryBillingDisabled DiscoveryIssue = "billing-disabled"
	DiscoveryGKEDisabled     DiscoveryIssue = "gke-disabled"
	DiscoveryAccessDenied    DiscoveryIssue = "access-denied"
	DiscoveryScanFailed      DiscoveryIssue = "scan-failed"
)

func (i DiscoveryIssue) Label() string {
	switch i {
	case DiscoveryBillingDisabled:
		return "Billing disabled"
	case DiscoveryGKEDisabled:
		return "GKE disabled"
	case DiscoveryAccessDenied:
		return "Access denied"
	case DiscoveryScanFailed:
		return "Scan failed"
	}
	return ""
}
