package catalog

import (
	"reflect"
	"slices"

	"github.com/infurio/contexthop/internal/config"
)

// PlanSaveDiscovery adds discovered records and identity mappings without
// overwriting saved names, visibility, risk, or access-profile customizations.
func PlanSaveDiscovery(saved, observed config.Config) Plan {
	after := saved.Clone()
	changes := []Change{}
	for name, item := range config.CloneDiscovery(observed.Discovery) {
		if !reflect.DeepEqual(after.Discovery[name], item) {
			after.Discovery[name] = item
			changes = append(changes, Change{Action: "set", To: Ref{KindIdentity, name}, Detail: "refresh discovered relationships"})
		}
	}
	for name, item := range observed.Identities {
		if _, exists := after.Identities[name]; !exists {
			after.Identities[name] = item
			changes = append(changes, Change{Action: "add", To: Ref{KindIdentity, name}})
		}
	}
	for name, item := range observed.Projects {
		current, exists := after.Projects[name]
		if !exists {
			after.Projects[name] = item
			changes = append(changes, Change{Action: "add", To: Ref{KindProject, name}})
			continue
		}
		if !reflect.DeepEqual(current.GoogleLabels, item.GoogleLabels) {
			current.GoogleLabels = item.LabelSet.Clone().GoogleLabels
			changes = append(changes, Change{Action: "set", To: Ref{KindProject, name}, Detail: "refresh Google labels"})
		}
		for _, identity := range item.Identities {
			if !slices.Contains(current.Identities, identity) {
				current.Identities = append(current.Identities, identity)
				changes = append(changes, Change{Action: "map", From: Ref{KindIdentity, identity}, To: Ref{KindProject, name}})
			}
		}
		after.Projects[name] = current
	}
	for name, item := range observed.Kubernetes {
		if current, exists := after.Kubernetes[name]; exists && !reflect.DeepEqual(current.GoogleLabels, item.GoogleLabels) {
			current.GoogleLabels = item.LabelSet.Clone().GoogleLabels
			after.Kubernetes[name] = current
			changes = append(changes, Change{Action: "set", To: Ref{KindKubernetes, name}, Detail: "refresh Google labels"})
		}
		if _, exists := after.Kubernetes[name]; !exists {
			after.Kubernetes[name] = item
			changes = append(changes, Change{Action: "add", To: Ref{KindKubernetes, name}})
		}
	}
	slices.SortFunc(changes, func(a, b Change) int {
		if (string(a.To.Kind) + a.To.Name + string(a.From.Kind) + a.From.Name) < (string(b.To.Kind) + b.To.Name + string(b.From.Kind) + b.From.Name) {
			return -1
		}
		if (string(a.To.Kind) + a.To.Name + string(a.From.Kind) + a.From.Name) > (string(b.To.Kind) + b.To.Name + string(b.From.Kind) + b.From.Name) {
			return 1
		}
		return 0
	})
	after.MigrateTags()
	return buildPlan(saved, after, changes)
}
