package catalog

import "github.com/infurio/contexthop/internal/config"

func PlanBrowser(cfg config.Config, name string, browser config.BrowserProfile) (Plan, error) {
	if err := browser.Validate(); err != nil {
		return Plan{}, err
	}
	after := cfg.Clone()
	identity, ok := after.Identities[name]
	if !ok {
		return Plan{}, unknown(KindIdentity, name)
	}
	identity.Browser = browser
	after.Identities[name] = identity
	return buildPlan(cfg, after, []Change{{Action: "browser", To: Ref{KindIdentity, name}}}), nil
}
