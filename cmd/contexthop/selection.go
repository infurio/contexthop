package main

import (
	"fmt"
	"reflect"
	"slices"

	cloudauth "github.com/infurio/contexthop/internal/auth"
	"github.com/infurio/contexthop/internal/catalog"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/destination"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/ui"
)

func validateSelectionFresh(baseline config.Config, selection resolver.Selection) error {
	fresh, err := loadConfig()
	if err != nil {
		return fmt.Errorf("reload configuration before switching: %w", err)
	}
	changed := func(kind, name string) error {
		return fmt.Errorf("selected %s %q changed while it was being selected; refresh and select it again", kind, name)
	}
	if selection.Identity != "" {
		before, beforeOK := baseline.Identities[selection.Identity]
		after, afterOK := fresh.Identities[selection.Identity]
		if !beforeOK || !afterOK || !reflect.DeepEqual(before, after) {
			return changed("identity", selection.Identity)
		}
	}
	if selection.Project != "" {
		before, beforeOK := baseline.Projects[selection.Project]
		after, afterOK := fresh.Projects[selection.Project]
		// A project absent from the baseline may have been discovered during
		// this picker session. It is validated live and is not saved.
		if beforeOK && (!afterOK || before.Provider != after.Provider || before.ProjectID != after.ProjectID || before.ProjectNumber != after.ProjectNumber || before.PreferredIdentity != after.PreferredIdentity || before.Risk != after.Risk || !reflect.DeepEqual(before.LabelSet, after.LabelSet) || before.Provenance != after.Provenance || before.VerifiedBy != after.VerifiedBy || before.ObservedAt != after.ObservedAt || before.Hidden != after.Hidden || !slices.Equal(before.Identities, after.Identities) || !slices.Equal(before.ManualIdentities, after.ManualIdentities)) {
			return changed("project", selection.Project)
		}
	}
	if selection.Kubernetes != "" {
		before, beforeOK := baseline.Kubernetes[selection.Kubernetes]
		after, afterOK := fresh.Kubernetes[selection.Kubernetes]
		if !beforeOK || !afterOK || !reflect.DeepEqual(before, after) {
			return changed("Kubernetes target", selection.Kubernetes)
		}
	}
	if selection.Docker != "" {
		before, beforeOK := baseline.Docker[selection.Docker]
		after, afterOK := fresh.Docker[selection.Docker]
		if !beforeOK || !afterOK || !reflect.DeepEqual(before, after) {
			return changed("Docker target", selection.Docker)
		}
	}
	if selection.Identity != "" {
		if selection.Project != "" && resolver.ProjectIdentityAvailable(baseline, selection.Project, selection.Identity) != resolver.ProjectIdentityAvailable(fresh, selection.Project, selection.Identity) {
			return changed("project relationships", selection.Project)
		}
		if selection.Kubernetes != "" && resolver.KubernetesIdentityAvailable(baseline, selection.Kubernetes, selection.Identity) != resolver.KubernetesIdentityAvailable(fresh, selection.Kubernetes, selection.Identity) {
			return changed("Kubernetes relationships", selection.Kubernetes)
		}
	}
	return nil
}

func identityOptions(cfg config.Config, allowed []string, includeHidden ...bool) []ui.Option {
	excludeHidden := allowed == nil && !includeHiddenOptions(includeHidden)
	names := slices.Clone(allowed)
	if names == nil {
		names = make([]string, 0, len(cfg.Identities))
		for name := range cfg.Identities {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	options := make([]ui.Option, 0, len(names))
	for _, name := range names {
		identity, ok := cfg.Identities[name]
		if !ok || excludeHidden && identity.Hidden {
			continue
		}
		account := identity.Account
		for otherName, other := range cfg.Identities {
			if otherName != name && other.Provider == identity.Provider && other.Account == identity.Account {
				account += " [" + name + "]"
				break
			}
		}
		authStatus := cloudauth.Status(identity)
		options = append(options, ui.Option{
			Name: name, Label: account, Identity: true, IdentityAccount: account,
			Provider: identity.Provider, AuthStatus: authStatus,
		})
	}
	return options
}

func compatibleIdentityOptions(cfg config.Config, provider string) []ui.Option {
	names := make([]string, 0, len(cfg.Identities))
	for name, identity := range cfg.Identities {
		if identity.Provider == provider && !identity.Hidden {
			names = append(names, name)
		}
	}
	return identityOptions(cfg, names)
}

func projectOptions(cfg config.Config, identityName string, includeHidden ...bool) []ui.Option {
	names := make([]string, 0, len(cfg.Projects))
	for name := range cfg.Projects {
		names = append(names, name)
	}
	slices.Sort(names)
	options := make([]ui.Option, 0, len(names))
	for _, name := range names {
		project := cfg.Projects[name]
		if project.Hidden && !includeHiddenOptions(includeHidden) {
			continue
		}
		if identityName != "" && mappedIdentityForProject(cfg, name, identityName) == "" {
			continue
		}
		account := projectIdentitySummary(cfg, project)
		if identityName != "" {
			account = cfg.Identities[identityName].Account
		}
		options = append(options, ui.Option{
			Name: name, Project: true, ProjectID: project.ProjectID,
			IdentityAccount:   account,
			KubernetesContext: kubernetesCountSummary(cfg, name, identityName), Risk: project.Risk,
		})
	}
	return options
}

func sessionProjectOptions(cfg config.Config, identityName string, includeHidden ...bool) []ui.Option {
	options := projectOptions(cfg, identityName, includeHidden...)
	for index := range options {
		candidates := resolver.EligibleIdentities(cfg, options[index].Name, "")
		if len(candidates) > 1 {
			options[index].IdentityChoices = formatCatalogComponentOptions(catalog.KindIdentity, identityOptions(cfg, candidates, true))
		}
		setSelectionIdentity(cfg, destination.Target{Kind: "project", Name: options[index].Name}, identityName, &options[index])
	}
	return options
}

func includeHiddenOptions(values []bool) bool { return len(values) > 0 && values[0] }
