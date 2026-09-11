package catalog

import (
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/discovery"
)

// PlanImportLocal uses the same reconciliation as the CLI, including stale-plan
// and workspace-impact checks when the reviewed import is applied.
func PlanImportLocal(saved, observed config.Config) (Plan, error) {
	after, report, err := discovery.Reconcile(saved, observed)
	if err != nil {
		return Plan{}, err
	}
	changes := []Change{}
	for _, detail := range report.Changes {
		changes = append(changes, Change{Action: "import", Detail: detail})
	}
	return buildPlan(saved, after, changes), nil
}
