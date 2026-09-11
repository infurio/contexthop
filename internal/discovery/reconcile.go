package discovery

import (
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

	cloudconfig "github.com/infurio/contexthop/internal/config"
)

// ReconcileReport describes the deterministic changes required to conform an
// existing catalog to a normalized set of observations.
type ReconcileReport struct {
	Known         int
	New           int
	Updated       int
	AliasesMerged int
	Ambiguous     int
	Stale         int
	Changes       []string
}

func (r ReconcileReport) Changed() bool {
	return r.New > 0 || r.Updated > 0 || r.AliasesMerged > 0
}

// Reconcile merges normalized observations into an existing catalog. It
// preserves user policy (names, risk, preferences, and workspaces), collapses
// only access profiles proven equivalent by an access signature, and leaves
// unobserved resources in place as stale rather than deleting them.
func Reconcile(current, observed cloudconfig.Config) (cloudconfig.Config, ReconcileReport, error) {
	result := cloneConfig(current)
	report := ReconcileReport{}

	identityNames := reconcileIdentities(&result, observed, &report)
	projectNames := reconcileProjects(&result, observed, identityNames, &report)
	reconcileKubernetes(&result, observed, projectNames, &report)
	reconcileDocker(&result, observed, &report)

	if err := result.Validate(); err != nil {
		return cloudconfig.Config{}, report, fmt.Errorf("validate reconciled catalog: %w", err)
	}
	sort.Strings(report.Changes)
	return result, report, nil
}

func reconcileIdentities(result *cloudconfig.Config, observed cloudconfig.Config, report *ReconcileReport) map[string]string {
	mapped := map[string]string{}
	for _, observedName := range sortedMapKeys(observed.Identities) {
		item := observed.Identities[observedName]
		name := findIdentity(*result, item)
		if name == "" {
			name = availableName(observedName, result.Identities)
			result.Identities[name] = item
			report.New++
			report.Changes = append(report.Changes, "add identity "+name)
		} else {
			existing := result.Identities[name]
			updated := existing
			if updated.Kind == "" {
				updated.Kind = item.Kind
			}
			if updated.CloudSDKConfig == "" {
				updated.CloudSDKConfig = item.CloudSDKConfig
			}
			if updated.ADC == "" {
				updated.ADC = item.ADC
			}
			if updated.Provenance == "" {
				updated.Provenance, updated.VerifiedBy, updated.ObservedAt = item.Provenance, item.VerifiedBy, item.ObservedAt
			}
			if !reflect.DeepEqual(updated, existing) {
				result.Identities[name] = updated
				report.Updated++
				report.Changes = append(report.Changes, "update identity "+name)
			} else {
				report.Known++
			}
		}
		mapped[observedName] = name
	}
	return mapped
}

func reconcileProjects(result *cloudconfig.Config, observed cloudconfig.Config, identityNames map[string]string, report *ReconcileReport) map[string]string {
	mapped := map[string]string{}
	for _, observedName := range sortedMapKeys(observed.Projects) {
		item := observed.Projects[observedName]
		name := findProject(*result, item)
		translatedIdentities := make([]string, 0, len(item.Identities))
		for _, identity := range item.Identities {
			if translated := identityNames[identity]; translated != "" {
				translatedIdentities = append(translatedIdentities, translated)
			}
		}
		if name == "" {
			name = availableName(observedName, result.Projects)
			item.Identities = uniqueSorted(translatedIdentities)
			result.Projects[name] = item
			report.New++
			report.Changes = append(report.Changes, "add project "+name)
		} else {
			existing := result.Projects[name]
			updated := existing
			updated.Identities = uniqueSorted(append(slices.Clone(existing.Identities), translatedIdentities...))
			if updated.ProjectNumber == "" {
				updated.ProjectNumber = item.ProjectNumber
			}
			if updated.Provenance == "" {
				updated.Provenance, updated.VerifiedBy, updated.ObservedAt = item.Provenance, item.VerifiedBy, item.ObservedAt
			}
			if !reflect.DeepEqual(updated, existing) {
				result.Projects[name] = updated
				report.Updated++
				report.Changes = append(report.Changes, "update project "+name)
			} else {
				report.Known++
			}
		}
		mapped[observedName] = name
	}
	return mapped
}

func reconcileKubernetes(result *cloudconfig.Config, observed cloudconfig.Config, projectNames map[string]string, report *ReconcileReport) {
	aliasOwners := map[string]int{}
	for observedName, target := range observed.Kubernetes {
		for _, alias := range aliasesFor(observed, observedName, target) {
			aliasOwners[alias.Context]++
		}
	}
	matchedCurrent := map[string]bool{}

	for _, observedName := range sortedMapKeys(observed.Kubernetes) {
		item := observed.Kubernetes[observedName]
		if translated := projectNames[item.Project]; translated != "" {
			item.Project = translated
		}
		aliases := aliasesFor(observed, observedName, item)
		var candidates []string
		for currentName, currentItem := range result.Kubernetes {
			if item.AccessID != "" && currentItem.AccessID == item.AccessID {
				candidates = append(candidates, currentName)
				continue
			}
			for _, alias := range aliases {
				if aliasOwners[alias.Context] == 1 && currentItem.Context == alias.Context && compatiblePhysicalTarget(*result, currentItem, item) {
					candidates = append(candidates, currentName)
					break
				}
			}
		}
		candidates = uniqueSorted(candidates)
		winner := preferredExistingTarget(candidates, result.Kubernetes, item.Context)
		if winner == "" {
			winner = availableName(observedName, result.Kubernetes)
			result.Kubernetes[winner] = item
			result.KubernetesAliases[winner] = slices.Clone(aliases)
			report.New++
			report.Changes = append(report.Changes, "add kubernetes access profile "+winner)
			continue
		}

		matchedCurrent[winner] = true
		existing := result.Kubernetes[winner]
		updated := mergeKubernetes(existing, item)
		for _, candidate := range candidates {
			if result.Kubernetes[candidate].Hidden {
				updated.Hidden = true
				break
			}
		}
		combinedAliases := mergeAliases(result.KubernetesAliases[winner], aliases, candidates, result.Kubernetes)
		if !reflect.DeepEqual(updated, existing) || !reflect.DeepEqual(combinedAliases, result.KubernetesAliases[winner]) {
			result.Kubernetes[winner] = updated
			result.KubernetesAliases[winner] = combinedAliases
			report.Updated++
			report.Changes = append(report.Changes, "update kubernetes access profile "+winner)
		} else {
			report.Known++
		}

		for _, duplicate := range candidates {
			if duplicate == winner {
				continue
			}
			delete(result.Kubernetes, duplicate)
			delete(result.KubernetesAliases, duplicate)
			for workspaceName, workspace := range result.Destinations {
				if workspace.Kubernetes == duplicate {
					workspace.Kubernetes = winner
					result.Destinations[workspaceName] = workspace
				}
			}
			report.AliasesMerged++
			report.Changes = append(report.Changes, "merge kubernetes access profile "+duplicate+" into "+winner)
		}
	}

	for name := range currentKubernetesNames(result, observed) {
		if !matchedCurrent[name] {
			report.Stale++
		}
	}
	for contextName, owners := range aliasOwners {
		if owners > 1 {
			report.Ambiguous++
			report.Changes = append(report.Changes, "ambiguous kubernetes context "+contextName)
		}
	}
}

func reconcileDocker(result *cloudconfig.Config, observed cloudconfig.Config, report *ReconcileReport) {
	for _, observedName := range sortedMapKeys(observed.Docker) {
		item := observed.Docker[observedName]
		name := ""
		for currentName, current := range result.Docker {
			if current.Context == item.Context {
				name = currentName
				break
			}
		}
		if name == "" {
			name = availableName(observedName, result.Docker)
			result.Docker[name] = item
			report.New++
			report.Changes = append(report.Changes, "add docker target "+name)
		} else {
			report.Known++
		}
	}
}

func findIdentity(cfg cloudconfig.Config, wanted cloudconfig.Identity) string {
	for _, name := range sortedMapKeys(cfg.Identities) {
		item := cfg.Identities[name]
		itemKind, wantedKind := item.Kind, wanted.Kind
		if itemKind == "" {
			itemKind = "user"
		}
		if wantedKind == "" {
			wantedKind = "user"
		}
		if item.Provider == wanted.Provider && itemKind == wantedKind && strings.EqualFold(item.Account, wanted.Account) {
			return name
		}
	}
	return ""
}

func findProject(cfg cloudconfig.Config, wanted cloudconfig.Project) string {
	for _, name := range sortedMapKeys(cfg.Projects) {
		item := cfg.Projects[name]
		if item.Provider == wanted.Provider && item.ProjectID == wanted.ProjectID {
			return name
		}
	}
	return ""
}

func compatiblePhysicalTarget(cfg cloudconfig.Config, current, observed cloudconfig.Kubernetes) bool {
	if current.ProviderID != "" && observed.ProviderID != "" {
		return current.ProviderID == observed.ProviderID
	}
	if current.Type == "gke" && observed.Type == "gke" && current.Cluster != "" && current.Location != "" {
		return current.Cluster == observed.Cluster && current.Location == observed.Location && projectID(cfg, current.Project) == projectID(cfg, observed.Project)
	}
	// A legacy generic target can be promoted only through an exact observed
	// context alias. The reconciler never merges two generic targets by name.
	return current.Type == "kubeconfig" && observed.Type != ""
}

func projectID(cfg cloudconfig.Config, name string) string {
	if project, ok := cfg.Projects[name]; ok {
		return project.ProjectID
	}
	return name
}

func mergeKubernetes(existing, observed cloudconfig.Kubernetes) cloudconfig.Kubernetes {
	updated := observed
	updated.LabelSet = existing.LabelSet.Clone()
	updated.Hidden = existing.Hidden
	if existing.ObservedAt != "" {
		updated.ObservedAt = existing.ObservedAt
	}
	if existing.Risk != "" {
		updated.Risk = existing.Risk
	}
	updated.ManualIdentity = existing.ManualIdentity
	if existing.PreferredIdentity != "" {
		updated.PreferredIdentity = existing.PreferredIdentity
	}
	if existing.Provenance == "manual" {
		updated.Provenance = existing.Provenance
	}
	return updated
}

func aliasesFor(cfg cloudconfig.Config, name string, target cloudconfig.Kubernetes) []cloudconfig.KubernetesAlias {
	aliases := slices.Clone(cfg.KubernetesAliases[name])
	if target.Context != "" {
		aliases = append(aliases, cloudconfig.KubernetesAlias{
			Context: target.Context, Kubeconfig: target.Kubeconfig, Namespace: target.Namespace,
			Provenance: target.Provenance, ObservedAt: target.ObservedAt,
		})
	}
	return uniqueAliases(aliases)
}

func mergeAliases(existing, observed []cloudconfig.KubernetesAlias, candidates []string, targets map[string]cloudconfig.Kubernetes) []cloudconfig.KubernetesAlias {
	existingByKey := map[string]cloudconfig.KubernetesAlias{}
	for _, alias := range existing {
		existingByKey[alias.Kubeconfig+"\x00"+alias.Context] = alias
	}
	all := slices.Clone(observed)
	for index, alias := range all {
		if previous, ok := existingByKey[alias.Kubeconfig+"\x00"+alias.Context]; ok && previous.ObservedAt != "" {
			all[index].ObservedAt = previous.ObservedAt
		}
	}
	observedContexts := map[string]bool{}
	for _, alias := range observed {
		observedContexts[alias.Context] = true
	}
	for _, name := range candidates {
		target := targets[name]
		if target.Context != "" && !observedContexts[target.Context] && !strings.Contains(target.Kubeconfig, string(os.PathListSeparator)) {
			all = append(all, cloudconfig.KubernetesAlias{Context: target.Context, Kubeconfig: target.Kubeconfig, Namespace: target.Namespace, Provenance: target.Provenance, ObservedAt: target.ObservedAt})
		}
	}
	for _, alias := range existing {
		if !observedContexts[alias.Context] && !strings.Contains(alias.Kubeconfig, string(os.PathListSeparator)) {
			all = append(all, alias)
		}
	}
	return uniqueAliases(all)
}

func uniqueAliases(values []cloudconfig.KubernetesAlias) []cloudconfig.KubernetesAlias {
	byKey := map[string]cloudconfig.KubernetesAlias{}
	for _, value := range values {
		if value.Context == "" {
			continue
		}
		key := value.Kubeconfig + "\x00" + value.Context
		byKey[key] = value
	}
	result := make([]cloudconfig.KubernetesAlias, 0, len(byKey))
	for _, value := range byKey {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Context != result[j].Context {
			return result[i].Context < result[j].Context
		}
		return result[i].Kubeconfig < result[j].Kubeconfig
	})
	return result
}

func preferredExistingTarget(candidates []string, targets map[string]cloudconfig.Kubernetes, preferredContext string) string {
	for _, name := range candidates {
		if targets[name].Context == preferredContext {
			return name
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

func currentKubernetesNames(result *cloudconfig.Config, observed cloudconfig.Config) map[string]bool {
	// Resources still in the result but not represented by the current scan are
	// intentionally retained. This helper exists to keep stale counting separate
	// from deletion semantics.
	names := map[string]bool{}
	observedAccess := map[string]bool{}
	for _, item := range observed.Kubernetes {
		observedAccess[item.AccessID] = true
	}
	for name, item := range result.Kubernetes {
		if item.AccessID == "" || !observedAccess[item.AccessID] {
			names[name] = true
		}
	}
	return names
}

func cloneConfig(source cloudconfig.Config) cloudconfig.Config {
	return source.Clone()
}

func availableName[V any](preferred string, values map[string]V) string {
	if _, exists := values[preferred]; !exists {
		return preferred
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", preferred, suffix)
		if _, exists := values[candidate]; !exists {
			return candidate
		}
	}
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func sortedMapKeys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
