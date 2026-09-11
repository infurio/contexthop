package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type ProbeResult struct {
	Observed Component
	Errors   []string
}

func (r ProbeResult) RelevantErrors(snapshot Snapshot) []string {
	// Global tool state is informational outside a ContextHop session. A probe
	// failure there does not indicate drift and must not make the home screen
	// look unhealthy; access is validated when the user starts a session.
	if !snapshot.Managed {
		return nil
	}
	result := make([]string, 0, len(r.Errors))
	for _, probeError := range r.Errors {
		if strings.HasPrefix(probeError, "gcloud ") && snapshot.Managed && snapshot.Expected.Identity == "" {
			continue
		}
		if strings.HasPrefix(probeError, "kubectl ") && snapshot.Managed && snapshot.Expected.Kubernetes == "" {
			continue
		}
		if strings.HasPrefix(probeError, "docker ") && snapshot.Managed && snapshot.Expected.Docker == "contexthop-none" {
			continue
		}
		result = append(result, probeError)
	}
	return result
}

type commandProbe struct {
	name string
	args []string
	set  func(*Component, []byte) error
}

func Probe(ctx context.Context) ProbeResult {
	probes := []commandProbe{
		{name: "gcloud", args: []string{"config", "list", "--format=json", "--quiet"}, set: setGcloud},
		{name: "kubectl", args: []string{"config", "current-context"}, set: setText(func(c *Component, v string) { c.Kubernetes = v })},
		{name: "docker", args: []string{"context", "show"}, set: setText(func(c *Component, v string) { c.Docker = v })},
	}

	type outcome struct {
		probe commandProbe
		value []byte
		err   error
	}
	outcomes := make(chan outcome, len(probes))
	var wg sync.WaitGroup
	for _, probe := range probes {
		wg.Add(1)
		go func(probe commandProbe) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			output, err := exec.CommandContext(probeCtx, probe.name, probe.args...).Output()
			outcomes <- outcome{probe: probe, value: output, err: err}
		}(probe)
	}
	wg.Wait()
	close(outcomes)

	result := ProbeResult{Observed: InspectLocal().Observed}
	for outcome := range outcomes {
		if outcome.err != nil {
			var execErr *exec.Error
			if errors.As(outcome.err, &execErr) {
				continue
			}
			result.Errors = append(result.Errors, outcome.probe.name+" probe failed")
			continue
		}
		if err := outcome.probe.set(&result.Observed, outcome.value); err != nil {
			result.Errors = append(result.Errors, outcome.probe.name+" returned invalid state")
		}
	}
	return result
}

func setText(set func(*Component, string)) func(*Component, []byte) error {
	return func(component *Component, output []byte) error {
		value := strings.TrimSpace(string(output))
		if value == "(unset)" {
			value = ""
		}
		set(component, value)
		return nil
	}
}

func setGcloud(component *Component, output []byte) error {
	var result struct {
		Core struct {
			Account string `json:"account"`
			Project string `json:"project"`
		} `json:"core"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return fmt.Errorf("decode gcloud configuration: %w", err)
	}
	component.Identity = result.Core.Account
	component.Project = result.Core.Project
	if component.Identity != "" || component.Project != "" {
		component.Provider = "gcp"
	}
	return nil
}
