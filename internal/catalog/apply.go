package catalog

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/infurio/contexthop/internal/config"
)

var ErrStalePlan = errors.New("catalog mutation plan is stale")

func Apply(path string, plan Plan) error {
	if !plan.Valid() {
		return fmt.Errorf("%w: %s", ErrInvalidPlan, joinProblems(plan.Problems))
	}
	if err := plan.Config.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPlan, err)
	}
	states := workspaceStates(plan.Config)
	for _, name := range sortedKeys(states) {
		state := states[name]
		if !state.Valid {
			return fmt.Errorf("%w: workspace %q cannot resolve: %s", ErrInvalidPlan, name, state.Error)
		}
	}
	// Lock a stable sidecar: config.Write replaces the configuration inode.
	// Keep the lock file after unlocking so all terminals use the same inode.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open catalog lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock catalog: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck
	current, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load current configuration: %w", err)
	}
	if revision(current) != plan.BaseRevision {
		return ErrStalePlan
	}
	if err := config.Write(path, plan.Config); err != nil {
		return fmt.Errorf("apply catalog mutation: %w", err)
	}
	return nil
}

func joinProblems(problems []string) string {
	if len(problems) == 0 {
		return "validation failed"
	}
	result := problems[0]
	for _, problem := range problems[1:] {
		result += "; " + problem
	}
	return result
}
