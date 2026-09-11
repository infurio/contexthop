package catalog

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func TestConcurrentApplyRejectsLosingPlans(t *testing.T) {
	cfg := config.New()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	const count = 24
	plans := make([]Plan, count)
	for i := range plans {
		name := fmt.Sprintf("docker-%d", i)
		var err error
		plans[i], err = PlanAddDocker(cfg, name, config.Docker{Context: name})
		if err != nil {
			t.Fatal(err)
		}
	}
	type result struct {
		index int
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, count)
	for i, plan := range plans {
		go func() {
			<-start
			results <- result{i, Apply(path, plan)}
		}()
	}
	close(start)
	successes, stale, winner := 0, 0, -1
	for range count {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			winner = result.index
		case errors.Is(result.err, ErrStalePlan):
			stale++
		default:
			t.Errorf("apply %d: %v", result.index, result.err)
		}
	}
	if successes != 1 || stale != count-1 {
		t.Fatalf("successful saves = %d, stale saves = %d; want 1 and %d", successes, stale, count-1)
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("docker-%d", winner)
	if len(saved.Docker) != 1 || saved.Docker[name].Context != name {
		t.Fatalf("successful change was lost: %#v", saved.Docker)
	}
	// A rejected caller can reload and successfully apply its change afterwards.
	retry, err := PlanAddDocker(saved, "retry", config.Docker{Context: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(path, retry); err != nil {
		t.Fatalf("retry after reload: %v", err)
	}
	saved, err = config.Load(path)
	if err != nil || len(saved.Docker) != 2 || saved.Docker[name].Context != name || saved.Docker["retry"].Context != "retry" {
		t.Fatalf("retry lost a change: %#v, %v", saved.Docker, err)
	}
}
