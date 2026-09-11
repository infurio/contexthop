package main

import (
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
)

// Account labels are shared with resource tabs; internal names remain selection keys.
func launchIdentityPicker(cfg config.Config, target launchTarget, candidates []string) ui.Picker {
	cluster := cfg.Kubernetes[target.Name]
	fields := []ui.PickerField{{Label: "Project", Value: cfg.Projects[target.Name].ProjectID}}
	if target.Kind == "kubernetes" {
		fields = []ui.PickerField{{Label: "Cluster", Value: firstNonEmpty(cluster.Cluster, cluster.Context, target.Name)}, {Label: "Project", Value: cfg.Projects[cluster.Project].ProjectID}}
	}
	return ui.Picker{Screen: "launch-identity", Title: "Choose identity",
		Description:   "Choose the account to use for this target.",
		ContextFields: fields,
		Options:       identityOptions(cfg, candidates),
	}
}
