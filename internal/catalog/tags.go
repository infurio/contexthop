package catalog

import (
	"fmt"
	"github.com/infurio/contexthop/internal/config"
	"slices"
	"strings"
)

// PlanTag updates a shared definition and/or one entity's assignments atomically.
func PlanTag(cfg config.Config, ref Ref, name, color, action string) (Plan, error) {
	if err := config.ValidateName(name); err != nil {
		return Plan{}, err
	}
	after := cfg.Clone()
	after.MigrateTags()
	if color != "" {
		if !config.ValidTagColor(color) {
			return Plan{}, fmt.Errorf("use a colour in #RRGGBB format")
		}
		after.Tags[name] = config.Tag{Color: color}
	}
	if _, ok := after.Tags[name]; !ok {
		return Plan{}, fmt.Errorf("unknown tag %q", name)
	}
	labels, _ := EntityLabels(after, ref)
	names := labels.TagNames()
	switch action {
	case "assign":
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	case "remove":
		names = slices.DeleteFunc(names, func(n string) bool { return n == name })
	case "color":
	default:
		return Plan{}, fmt.Errorf("unknown tag action")
	}
	labels.Tags = names
	labels.TagsConfigured = true
	intermediate, err := PlanLabels(after, ref, labels)
	if err != nil {
		return Plan{}, err
	}
	detail := action + " tag " + name
	if color != "" {
		detail += " · colour " + color + " (shared by all entities)"
	}
	detail += " · tags: " + strings.Join(names, ", ")
	return buildPlan(cfg, intermediate.Config, []Change{{Action: "set", To: ref, Detail: detail}}), nil
}

// PlanDeleteTag removes the shared definition and every explicit assignment in
// one revision-checked write. Other labels and resource settings are preserved.
func PlanDeleteTag(cfg config.Config, name string) (Plan, error) {
	after := cfg.Clone()
	after.MigrateTags()
	if _, ok := after.Tags[name]; !ok {
		return Plan{}, fmt.Errorf("unknown tag %q", name)
	}
	changes := []Change{{Action: "delete_tag", To: Ref{Kind: Kind("tag"), Name: name}, Detail: "delete shared definition everywhere"}}
	remove := func(labels config.LabelSet, ref Ref) config.LabelSet {
		if !slices.Contains(labels.TagNames(), name) {
			return labels
		}
		labels = labels.Clone()
		labels.Tags = slices.DeleteFunc(labels.Tags, func(v string) bool { return v == name })
		labels.TagsConfigured = true
		changes = append(changes, Change{Action: "untag", To: ref, Detail: "remove tag " + name})
		return labels
	}
	rewriteTagAssignments(&after, remove)
	delete(after.Tags, name)
	slices.SortFunc(changes[1:], func(a, b Change) int {
		return strings.Compare(string(a.To.Kind)+":"+a.To.Name, string(b.To.Kind)+":"+b.To.Name)
	})
	return buildPlan(cfg, after, changes), nil
}

// PlanRenameTag changes the shared name and its assignments in one checked write.
func PlanRenameTag(cfg config.Config, oldName, newName string) (Plan, error) {
	if err := config.ValidateName(newName); err != nil {
		return Plan{}, err
	}
	after := cfg.Clone()
	after.MigrateTags()
	tag, ok := after.Tags[oldName]
	if !ok {
		return Plan{}, fmt.Errorf("unknown tag %q", oldName)
	}
	if newName == oldName {
		return Plan{}, fmt.Errorf("enter a different tag name")
	}
	if _, ok := after.Tags[newName]; ok {
		return Plan{}, fmt.Errorf("tag %q already exists; choose another name", newName)
	}
	changes := []Change{{Action: "rename_tag", From: Ref{Kind: Kind("tag"), Name: oldName}, To: Ref{Kind: Kind("tag"), Name: newName}, Detail: "rename shared tag everywhere"}}
	rewriteTagAssignments(&after, func(labels config.LabelSet, ref Ref) config.LabelSet {
		if !slices.Contains(labels.TagNames(), oldName) {
			return labels
		}
		labels = labels.Clone()
		for i, name := range labels.Tags {
			if name == oldName {
				labels.Tags[i] = newName
			}
		}
		labels.TagsConfigured = true
		changes = append(changes, Change{Action: "retag", To: ref, Detail: oldName + " → " + newName})
		return labels
	})
	delete(after.Tags, oldName)
	after.Tags[newName] = tag
	slices.SortFunc(changes[1:], func(a, b Change) int {
		return strings.Compare(string(a.To.Kind)+":"+a.To.Name, string(b.To.Kind)+":"+b.To.Name)
	})
	return buildPlan(cfg, after, changes), nil
}

func rewriteTagAssignments(cfg *config.Config, update func(config.LabelSet, Ref) config.LabelSet) {
	for key, v := range cfg.Identities {
		v.LabelSet = update(v.LabelSet, Ref{KindIdentity, key})
		cfg.Identities[key] = v
	}
	for key, v := range cfg.Projects {
		v.LabelSet = update(v.LabelSet, Ref{KindProject, key})
		cfg.Projects[key] = v
	}
	for key, v := range cfg.Kubernetes {
		v.LabelSet = update(v.LabelSet, Ref{KindKubernetes, key})
		cfg.Kubernetes[key] = v
	}
	for key, v := range cfg.Docker {
		v.LabelSet = update(v.LabelSet, Ref{KindDocker, key})
		cfg.Docker[key] = v
	}
	for key, v := range cfg.Destinations {
		v.LabelSet = update(v.LabelSet, Ref{KindWorkspace, key})
		cfg.Destinations[key] = v
	}
}
