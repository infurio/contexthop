package destination

import (
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

// Resolution is either ready, a real identity choice, or a terminal resolution
// error. Both the CLI and application use this policy before presenting a picker.
type Resolution struct {
	Resolved   resolver.Resolved
	Candidates []string
	Err        error
}

func Prepare(cfg config.Config, target Target) Resolution {
	resolved, err := Resolve(cfg, target, "")
	result := Resolution{Resolved: resolved, Err: err}
	if err == nil || (target.Kind != "project" && target.Kind != "kubernetes") {
		return result
	}
	suggested, candidates, suggestionErr := IdentitySuggestion(cfg, target)
	if suggestionErr == nil && suggested == "" && len(candidates) > 1 {
		result.Candidates = candidates
	}
	return result
}
