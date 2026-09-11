package catalog

import (
	"fmt"
	"github.com/infurio/contexthop/internal/config"
)

func EntityLabels(cfg config.Config, ref Ref) (config.LabelSet, string) {
	switch ref.Kind {
	case KindIdentity:
		v := cfg.Identities[ref.Name]
		return v.LabelSet.Clone(), v.LabelSet.Summary("")
	case KindProject:
		v := cfg.Projects[ref.Name]
		return legacyLabels(v.LabelSet, v.Risk)
	case KindKubernetes:
		v := cfg.Kubernetes[ref.Name]
		return legacyLabels(v.LabelSet, v.Risk)
	case KindDocker:
		v := cfg.Docker[ref.Name]
		return legacyLabels(v.LabelSet, v.Risk)
	case KindWorkspace:
		v := cfg.Destinations[ref.Name]
		return legacyLabels(v.LabelSet, v.Risk)
	}
	return config.LabelSet{}, ""
}
func legacyLabels(s config.LabelSet, risk string) (config.LabelSet, string) {
	s = s.Clone()
	if risk != "" {
		if s.Labels == nil {
			s.Labels = map[string]string{}
		}
		if _, ok := s.Labels["risk"]; !ok {
			s.Labels["risk"] = risk
		}
	}
	return s, s.Summary("")
}
func PlanLabels(cfg config.Config, ref Ref, labels config.LabelSet) (Plan, error) {
	for k, v := range labels.Effective("") {
		if err := config.ValidateName(k); err != nil {
			return Plan{}, fmt.Errorf("label key: %w", err)
		}
		if v != "" {
			if err := config.ValidateName(v); err != nil {
				return Plan{}, fmt.Errorf("label value: %w", err)
			}
		}
	}
	after := cfg.Clone()
	labels = labels.Clone()
	switch ref.Kind {
	case KindIdentity:
		v, ok := after.Identities[ref.Name]
		if !ok {
			return Plan{}, fmt.Errorf("unknown identity")
		}
		v.LabelSet = labels
		after.Identities[ref.Name] = v
	case KindProject:
		v, ok := after.Projects[ref.Name]
		if !ok {
			return Plan{}, fmt.Errorf("unknown project")
		}
		v.LabelSet = labels
		v.Risk = ""
		after.Projects[ref.Name] = v
	case KindKubernetes:
		v, ok := after.Kubernetes[ref.Name]
		if !ok {
			return Plan{}, fmt.Errorf("unknown target")
		}
		v.LabelSet = labels
		v.Risk = ""
		after.Kubernetes[ref.Name] = v
	case KindDocker:
		v, ok := after.Docker[ref.Name]
		if !ok {
			return Plan{}, fmt.Errorf("unknown target")
		}
		v.LabelSet = labels
		v.Risk = ""
		after.Docker[ref.Name] = v
	case KindWorkspace:
		v, ok := after.Destinations[ref.Name]
		if !ok {
			return Plan{}, fmt.Errorf("unknown workspace")
		}
		v.LabelSet = labels
		v.Risk = ""
		after.Destinations[ref.Name] = v
	default:
		return Plan{}, fmt.Errorf("unknown entity")
	}
	return buildPlan(cfg, after, []Change{{Action: "set", To: ref, Detail: "labels: " + labels.Summary("")}}), nil
}
