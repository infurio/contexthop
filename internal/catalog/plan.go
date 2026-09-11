package catalog

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"gopkg.in/yaml.v3"
)

type Change struct {
	Action string
	From   Ref
	To     Ref
	Detail string
}

type WorkspaceState struct {
	Name       string
	Valid      bool
	Error      string
	Identity   string
	Project    string
	Kubernetes string
	Docker     string
	ADC        string
	Risk       string
}

type Impact struct {
	Workspace string
	Before    WorkspaceState
	After     WorkspaceState
}

type Plan struct {
	BaseRevision string
	Config       config.Config
	Changes      []Change
	Impacts      []Impact
	Problems     []string
}

func (p Plan) Valid() bool { return len(p.Problems) == 0 }

func PlanPromptPrefix(cfg config.Config, value string) (Plan, error) {
	after := cfg.Clone()
	after.PromptPrefix = value
	if err := after.Validate(); err != nil {
		return Plan{}, err
	}
	return buildPlan(cfg, after, nil), nil
}

func PlanAddIdentity(cfg config.Config, name string, item config.Identity) (Plan, error) {
	if _, exists := cfg.Identities[name]; exists {
		return Plan{}, duplicate(KindIdentity, name)
	}
	after := clone(cfg)
	after.Identities[name] = item
	return buildPlan(cfg, after, []Change{{Action: "add", To: Ref{KindIdentity, name}}}), nil
}

func PlanAddProject(cfg config.Config, name string, item config.Project) (Plan, error) {
	if _, exists := cfg.Projects[name]; exists {
		return Plan{}, duplicate(KindProject, name)
	}
	after := clone(cfg)
	item.Identities = slices.Clone(item.Identities)
	after.Projects[name] = item
	return buildPlan(cfg, after, []Change{{Action: "add", To: Ref{KindProject, name}}}), nil
}

func PlanAddKubernetes(cfg config.Config, name string, item config.Kubernetes) (Plan, error) {
	if _, exists := cfg.Kubernetes[name]; exists {
		return Plan{}, duplicate(KindKubernetes, name)
	}
	after := clone(cfg)
	after.Kubernetes[name] = item
	if item.Context != "" {
		after.KubernetesAliases[name] = []config.KubernetesAlias{{
			Context: item.Context, Kubeconfig: item.Kubeconfig, Namespace: item.Namespace,
			Provenance: item.Provenance, ObservedAt: item.ObservedAt,
		}}
	}
	return buildPlan(cfg, after, []Change{{Action: "add", To: Ref{KindKubernetes, name}}}), nil
}

// PlanConfigureGKE stages the identity mapping and the GKE access profile as
// one atomic change. The identity is deliberately attached to both the project
// and target so later activation never has to guess between accounts.
func PlanConfigureGKE(cfg config.Config, name string, item config.Kubernetes, identityName string) (Plan, error) {
	existing, exists := cfg.Kubernetes[name]
	if exists && (existing.Type != "gke" || existing.Project != item.Project || existing.Cluster != item.Cluster || existing.Location != item.Location || existing.Endpoint != item.Endpoint) {
		return Plan{}, duplicate(KindKubernetes, name)
	}
	project, ok := cfg.Projects[item.Project]
	if !ok {
		return Plan{}, unknown(KindProject, item.Project)
	}
	identity, ok := cfg.Identities[identityName]
	if !ok {
		return Plan{}, unknown(KindIdentity, identityName)
	}
	if item.Type != "gke" || project.Provider != "gcp" || identity.Provider != project.Provider {
		return Plan{}, fmt.Errorf("GKE target %q requires a compatible GCP project and identity", name)
	}

	after := clone(cfg)
	changes := make([]Change, 0, 2)
	if !slices.Contains(project.Identities, identityName) {
		project.Identities = append(project.Identities, identityName)
		sort.Strings(project.Identities)
		after.Projects[item.Project] = project
		changes = append(changes, Change{Action: "map", From: Ref{KindIdentity, identityName}, To: Ref{KindProject, item.Project}})
	}
	item.PreferredIdentity = identityName
	item.ManualIdentity = identityName
	if exists {
		// Preserve discovery/import metadata and aliases when the selected access
		// profile is already present; this operation only makes its dependency
		// choice explicit.
		existing.PreferredIdentity = identityName
		existing.ManualIdentity = identityName
		after.Kubernetes[name] = existing
	} else {
		after.Kubernetes[name] = item
	}
	detail := fmt.Sprintf("project:%s · location:%s · identity:%s · endpoint:%s", project.ProjectID, item.Location, identity.Account, gkeEndpointLabel(item.Endpoint))
	action := "add"
	if exists {
		action = "set"
	}
	changes = append(changes, Change{Action: action, To: Ref{KindKubernetes, name}, Detail: detail})
	return buildPlan(cfg, after, changes), nil
}

func gkeEndpointLabel(endpoint string) string {
	if endpoint == "" {
		return "external IP (default)"
	}
	return endpoint
}

func PlanAddDocker(cfg config.Config, name string, item config.Docker) (Plan, error) {
	if _, exists := cfg.Docker[name]; exists {
		return Plan{}, duplicate(KindDocker, name)
	}
	after := clone(cfg)
	after.Docker[name] = item
	return buildPlan(cfg, after, []Change{{Action: "add", To: Ref{KindDocker, name}}}), nil
}

func PlanAddWorkspace(cfg config.Config, name string, item config.Destination) (Plan, error) {
	if _, exists := cfg.Destinations[name]; exists {
		return Plan{}, duplicate(KindWorkspace, name)
	}
	after := clone(cfg)
	after.Destinations[name] = item
	return buildPlan(cfg, after, []Change{{Action: "add", To: Ref{KindWorkspace, name}}}), nil
}

// PlanUpdateWorkspace replaces a workspace draft in one atomic plan. Component,
// ADC, and risk edits are accumulated so the caller can present one preview and
// create one backup for the complete workspace change.
func PlanUpdateWorkspace(cfg config.Config, name string, item config.Destination) (Plan, error) {
	before, exists := cfg.Destinations[name]
	if !exists {
		return Plan{}, unknown(KindWorkspace, name)
	}
	after := clone(cfg)
	after.Destinations[name] = item
	workspaceRef := Ref{KindWorkspace, name}
	changes := make([]Change, 0, 10)
	for _, component := range []struct {
		kind       Kind
		beforeName string
		afterName  string
	}{
		{KindIdentity, before.Identity, item.Identity},
		{KindProject, before.Project, item.Project},
		{KindKubernetes, before.Kubernetes, item.Kubernetes},
		{KindDocker, before.Docker, item.Docker},
	} {
		if component.beforeName == component.afterName {
			continue
		}
		if component.beforeName != "" {
			changes = append(changes, Change{Action: "remove_mapping", From: Ref{component.kind, component.beforeName}, To: workspaceRef})
		}
		if component.afterName != "" {
			changes = append(changes, Change{Action: "map", From: Ref{component.kind, component.afterName}, To: workspaceRef})
		}
	}
	if before.ADC != item.ADC {
		changes = append(changes, Change{Action: "set", To: workspaceRef, Detail: "ADC: " + workspaceSetting(before.ADC) + " → " + workspaceSetting(item.ADC)})
	}
	if before.Risk != item.Risk {
		changes = append(changes, Change{Action: "set", To: workspaceRef, Detail: "risk: " + workspaceSetting(before.Risk) + " → " + workspaceSetting(item.Risk)})
	}
	return buildPlan(cfg, after, changes), nil
}

func workspaceSetting(value string) string {
	if value == "" {
		return "automatic"
	}
	return value
}

// PlanForget removes a discoverable resource from the current catalog without
// suppressing future discovery. References that would dangle are reported in
// Problems and the plan cannot be applied until the user explicitly remaps or
// removes them.
func PlanForget(cfg config.Config, ref Ref) (Plan, error) {
	switch ref.Kind {
	case KindIdentity, KindProject, KindKubernetes, KindDocker:
		return planRemove(cfg, ref, "forget")
	default:
		return Plan{}, fmt.Errorf("%s %q cannot be forgotten", ref.Kind, ref.Name)
	}
}

// PlanRemove deletes a catalog item without silently deleting or rewriting its
// dependants. It remains the operation for user-owned workspaces.
func PlanRemove(cfg config.Config, ref Ref) (Plan, error) {
	return planRemove(cfg, ref, "remove")
}

// PlanSetHidden changes only ContextHop catalog visibility. Hidden resources stay
// in the configuration so existing relationships remain valid and discovery
// can continue refreshing the same stable record.
func PlanSetHidden(cfg config.Config, ref Ref, hidden bool) (Plan, error) {
	after := clone(cfg)
	changed := false
	switch ref.Kind {
	case KindIdentity:
		item, ok := after.Identities[ref.Name]
		if !ok {
			return Plan{}, unknown(ref.Kind, ref.Name)
		}
		changed = item.Hidden != hidden
		item.Hidden = hidden
		after.Identities[ref.Name] = item
	case KindProject:
		item, ok := after.Projects[ref.Name]
		if !ok {
			return Plan{}, unknown(ref.Kind, ref.Name)
		}
		changed = item.Hidden != hidden
		item.Hidden = hidden
		after.Projects[ref.Name] = item
	case KindKubernetes:
		item, ok := after.Kubernetes[ref.Name]
		if !ok {
			return Plan{}, unknown(ref.Kind, ref.Name)
		}
		changed = item.Hidden != hidden
		item.Hidden = hidden
		after.Kubernetes[ref.Name] = item
	case KindDocker:
		item, ok := after.Docker[ref.Name]
		if !ok {
			return Plan{}, unknown(ref.Kind, ref.Name)
		}
		changed = item.Hidden != hidden
		item.Hidden = hidden
		after.Docker[ref.Name] = item
	case KindWorkspace:
		item, ok := after.Destinations[ref.Name]
		if !ok {
			return Plan{}, unknown(ref.Kind, ref.Name)
		}
		changed = item.Hidden != hidden
		item.Hidden = hidden
		after.Destinations[ref.Name] = item
	default:
		return Plan{}, fmt.Errorf("unsupported catalog kind %q", ref.Kind)
	}
	if !changed {
		return buildPlan(cfg, cfg, nil), nil
	}
	action := "unhide"
	if hidden {
		action = "hide"
	}
	return buildPlan(cfg, after, []Change{{Action: action, From: ref}}), nil
}

func planRemove(cfg config.Config, ref Ref, action string) (Plan, error) {
	if ref.Kind == KindWorkspace {
		if _, exists := cfg.Destinations[ref.Name]; !exists {
			return Plan{}, unknown(ref.Kind, ref.Name)
		}
	} else if !componentExists(cfg, ref) {
		return Plan{}, unknown(ref.Kind, ref.Name)
	}
	after := clone(cfg)
	switch ref.Kind {
	case KindIdentity:
		delete(after.Identities, ref.Name)
		delete(after.Discovery, ref.Name)
	case KindProject:
		delete(after.Projects, ref.Name)
	case KindKubernetes:
		delete(after.Kubernetes, ref.Name)
		delete(after.KubernetesAliases, ref.Name)
	case KindDocker:
		delete(after.Docker, ref.Name)
	case KindWorkspace:
		delete(after.Destinations, ref.Name)
	default:
		return Plan{}, fmt.Errorf("unsupported catalog kind %q", ref.Kind)
	}
	return buildPlan(cfg, after, []Change{{Action: action, From: ref}}), nil
}

func PlanMapIdentity(cfg config.Config, projectName, identityName string) (Plan, error) {
	project, ok := cfg.Projects[projectName]
	if !ok {
		return Plan{}, unknown(KindProject, projectName)
	}
	identity, ok := cfg.Identities[identityName]
	if !ok {
		return Plan{}, unknown(KindIdentity, identityName)
	}
	if project.Provider != identity.Provider {
		return Plan{}, fmt.Errorf("project %q provider %q is incompatible with identity %q provider %q", projectName, project.Provider, identityName, identity.Provider)
	}
	if slices.Contains(project.ManualIdentities, identityName) {
		return buildPlan(cfg, cfg, nil), nil
	}
	after := clone(cfg)
	project = after.Projects[projectName]
	if !slices.Contains(project.Identities, identityName) {
		project.Identities = append(project.Identities, identityName)
	}
	project.ManualIdentities = append(project.ManualIdentities, identityName)
	sort.Strings(project.Identities)
	after.Projects[projectName] = project
	return buildPlan(cfg, after, []Change{{Action: "map", From: Ref{KindIdentity, identityName}, To: Ref{KindProject, projectName}}}), nil
}

func PlanRemoveIdentityMapping(cfg config.Config, projectName, identityName string) (Plan, error) {
	project, ok := cfg.Projects[projectName]
	if !ok {
		return Plan{}, unknown(KindProject, projectName)
	}
	if _, ok := cfg.Identities[identityName]; !ok {
		return Plan{}, unknown(KindIdentity, identityName)
	}
	if !slices.Contains(project.Identities, identityName) {
		return Plan{}, fmt.Errorf("identity %q is not mapped to project %q", identityName, projectName)
	}
	after := clone(cfg)
	project = after.Projects[projectName]
	project.Identities = slices.DeleteFunc(project.Identities, func(name string) bool { return name == identityName })
	project.ManualIdentities = slices.DeleteFunc(project.ManualIdentities, func(name string) bool { return name == identityName })
	if project.PreferredIdentity == identityName {
		project.PreferredIdentity = ""
	}
	after.Projects[projectName] = project
	for targetName, target := range after.Kubernetes {
		if target.Project == projectName && target.ManualIdentity == identityName {
			target.ManualIdentity = ""
			after.Kubernetes[targetName] = target
		}
		if target.Project == projectName && target.PreferredIdentity == identityName {
			target.PreferredIdentity = ""
			after.Kubernetes[targetName] = target
		}
	}
	return buildPlan(cfg, after, []Change{{Action: "remove_mapping", From: Ref{KindIdentity, identityName}, To: Ref{KindProject, projectName}}}), nil
}

func PlanSetKubernetesProject(cfg config.Config, targetName, projectName string) (Plan, error) {
	target, ok := cfg.Kubernetes[targetName]
	if !ok {
		return Plan{}, unknown(KindKubernetes, targetName)
	}
	project, ok := cfg.Projects[projectName]
	if !ok {
		return Plan{}, unknown(KindProject, projectName)
	}
	if target.Type == "gke" {
		if project.Provider != "gcp" {
			return Plan{}, fmt.Errorf("GKE target %q requires a GCP project", targetName)
		}
		if target.Project != "" && target.Project != projectName {
			return Plan{}, fmt.Errorf("GKE target %q is provider-bound to project %q and cannot be reassigned to %q", targetName, target.Project, projectName)
		}
	}
	if target.Project == projectName {
		return buildPlan(cfg, cfg, nil), nil
	}
	after := clone(cfg)
	target = after.Kubernetes[targetName]
	oldProject := target.Project
	target.Project = projectName
	if target.ManualIdentity != "" && !slices.Contains(project.Identities, target.ManualIdentity) {
		target.ManualIdentity = ""
	}
	if target.PreferredIdentity != "" && !slices.Contains(project.Identities, target.PreferredIdentity) {
		target.PreferredIdentity = ""
	}
	after.Kubernetes[targetName] = target
	// Encode each removed and added edge independently for an unambiguous
	// confirmation preview.
	var changes []Change
	if oldProject != "" {
		changes = []Change{
			{Action: "remove_mapping", From: Ref{KindProject, oldProject}, To: Ref{KindKubernetes, targetName}},
			{Action: "map", From: Ref{KindProject, projectName}, To: Ref{KindKubernetes, targetName}},
		}
	} else {
		changes = []Change{{Action: "map", From: Ref{KindProject, projectName}, To: Ref{KindKubernetes, targetName}}}
	}
	return buildPlan(cfg, after, changes), nil
}

func PlanRemoveKubernetesProject(cfg config.Config, targetName string) (Plan, error) {
	target, ok := cfg.Kubernetes[targetName]
	if !ok {
		return Plan{}, unknown(KindKubernetes, targetName)
	}
	if target.Type == "gke" {
		return Plan{}, fmt.Errorf("GKE target %q cannot remove its provider project mapping", targetName)
	}
	if target.Project == "" {
		return buildPlan(cfg, cfg, nil), nil
	}
	after := clone(cfg)
	target = after.Kubernetes[targetName]
	oldProject := target.Project
	target.Project = ""
	target.ManualIdentity = ""
	target.PreferredIdentity = ""
	after.Kubernetes[targetName] = target
	return buildPlan(cfg, after, []Change{{Action: "remove_mapping", From: Ref{KindProject, oldProject}, To: Ref{KindKubernetes, targetName}}}), nil
}

// PlanSetWorkspaceComponent maps, remaps, or removes one explicit workspace
// component. An empty componentName removes the mapping.
func PlanSetWorkspaceComponent(cfg config.Config, workspaceName string, kind Kind, componentName string) (Plan, error) {
	workspace, ok := cfg.Destinations[workspaceName]
	if !ok {
		return Plan{}, unknown(KindWorkspace, workspaceName)
	}
	if componentName != "" && !componentExists(cfg, Ref{kind, componentName}) {
		return Plan{}, unknown(kind, componentName)
	}
	old, err := workspaceComponent(workspace, kind)
	if err != nil {
		return Plan{}, err
	}
	if old == componentName {
		return buildPlan(cfg, cfg, nil), nil
	}
	after := clone(cfg)
	workspace = after.Destinations[workspaceName]
	setWorkspaceComponent(&workspace, kind, componentName)
	after.Destinations[workspaceName] = workspace
	var changes []Change
	workspaceRef := Ref{KindWorkspace, workspaceName}
	if old != "" {
		changes = append(changes, Change{Action: "remove_mapping", From: Ref{kind, old}, To: workspaceRef})
	}
	if componentName != "" {
		changes = append(changes, Change{Action: "map", From: Ref{kind, componentName}, To: workspaceRef})
	}
	return buildPlan(cfg, after, changes), nil
}

func buildPlan(before, after config.Config, changes []Change) Plan {
	plan := Plan{BaseRevision: revision(before), Config: clone(after), Changes: changes}
	beforeStates := workspaceStates(before)
	afterStates := workspaceStates(after)
	names := map[string]bool{}
	for name := range beforeStates {
		names[name] = true
	}
	for name := range afterStates {
		names[name] = true
	}
	for _, name := range sortedKeys(names) {
		if beforeStates[name] != afterStates[name] {
			plan.Impacts = append(plan.Impacts, Impact{Workspace: name, Before: beforeStates[name], After: afterStates[name]})
		}
	}
	if err := after.Validate(); err != nil {
		plan.Problems = append(plan.Problems, strings.Split(err.Error(), "\n")...)
	}
	for _, name := range sortedKeys(after.Destinations) {
		if state := afterStates[name]; !state.Valid && !containsProblem(plan.Problems, name, state.Error) {
			plan.Problems = append(plan.Problems, fmt.Sprintf("workspace %q cannot resolve: %s", name, state.Error))
		}
	}
	sort.Strings(plan.Problems)
	return plan
}

func workspaceStates(cfg config.Config) map[string]WorkspaceState {
	result := make(map[string]WorkspaceState, len(cfg.Destinations))
	for name := range cfg.Destinations {
		state := WorkspaceState{Name: name}
		resolved, err := resolver.Destination(cfg, name)
		if err != nil {
			state.Error = err.Error()
			result[name] = state
			continue
		}
		state.Valid = true
		state.Identity = resolved.IdentityName
		state.Project = resolved.ProjectName
		state.Kubernetes = resolved.KubernetesName
		state.Docker = resolved.DockerName
		state.ADC = resolved.ADCMode
		state.Risk = resolved.Risk
		result[name] = state
	}
	return result
}

func revision(cfg config.Config) string {
	cfg = cfg.Clone()
	cfg.MigrateLabels()
	cfg.MigrateTags()
	data, _ := yaml.Marshal(cfg)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func clone(source config.Config) config.Config { return source.Clone() }

func componentExists(cfg config.Config, ref Ref) bool {
	switch ref.Kind {
	case KindIdentity:
		_, ok := cfg.Identities[ref.Name]
		return ok
	case KindProject:
		_, ok := cfg.Projects[ref.Name]
		return ok
	case KindKubernetes:
		_, ok := cfg.Kubernetes[ref.Name]
		return ok
	case KindDocker:
		_, ok := cfg.Docker[ref.Name]
		return ok
	default:
		return false
	}
}

func workspaceComponent(workspace config.Destination, kind Kind) (string, error) {
	switch kind {
	case KindIdentity:
		return workspace.Identity, nil
	case KindProject:
		return workspace.Project, nil
	case KindKubernetes:
		return workspace.Kubernetes, nil
	case KindDocker:
		return workspace.Docker, nil
	default:
		return "", fmt.Errorf("%q is not a workspace component kind", kind)
	}
}

func setWorkspaceComponent(workspace *config.Destination, kind Kind, value string) {
	switch kind {
	case KindIdentity:
		workspace.Identity = value
	case KindProject:
		workspace.Project = value
	case KindKubernetes:
		workspace.Kubernetes = value
	case KindDocker:
		workspace.Docker = value
	}
}

func unknown(kind Kind, name string) error { return fmt.Errorf("unknown %s %q", kind, name) }

func duplicate(kind Kind, name string) error { return fmt.Errorf("%s %q already exists", kind, name) }

func containsProblem(problems []string, workspace, detail string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, fmt.Sprintf("workspace %q", workspace)) || problem == detail {
			return true
		}
	}
	return false
}

var ErrInvalidPlan = errors.New("catalog mutation plan is invalid")
