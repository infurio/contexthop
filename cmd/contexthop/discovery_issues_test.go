package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/infurio/contexthop/internal/catalog"
	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/ui"
	"strconv"
	"strings"
	"testing"
	"time"
)

type unavailableScopesFake struct{}

func (unavailableScopesFake) RefreshProjects(context.Context, config.Identity) (catalog.ProjectResult, error) {
	r := catalog.ProjectResult{Freshness: catalog.FreshnessLive, LastSuccess: time.Now()}
	for i := range 76 {
		r.Projects = append(r.Projects, catalog.Project{ProjectID: fmt.Sprintf("p%d", i)})
	}
	return r, nil
}
func (unavailableScopesFake) RefreshClusters(_ context.Context, _ config.Identity, project string) (catalog.ClusterResult, error) {
	n, _ := strconv.Atoi(strings.TrimPrefix(project, "p"))
	if n < 45 {
		return catalog.ClusterResult{}, errors.New("Kubernetes Engine API has not been used in project before or it is disabled")
	}
	if n == 45 {
		return catalog.ClusterResult{}, errors.New(`Required "container.clusters.list" permission(s) for project`)
	}
	if n == 56 {
		return catalog.ClusterResult{}, errors.New("This API method requires billing to be enabled")
	}
	r := catalog.ClusterResult{Freshness: catalog.FreshnessLive, LastSuccess: time.Now()}
	if n < 56 {
		r.Clusters = []catalog.Cluster{{ProjectID: project, Name: "cluster", Location: "region"}}
	}
	return r, nil
}
func TestDiscoverySummarizesUnavailableScopesAsPartialSuccess(t *testing.T) {
	cfg := discoveryDialogFixture()
	saved := cfg.Clone()
	tr := runScopedDiscovery(cfg, saved, ui.Draft{discoverIdentity: "a", discoverScope: "all", discoverOrigin: string(ui.ScreenProject), ui.ScreenIdentity: "a"}, nil, unavailableScopesFake{})
	for _, want := range []string{"Discovery partially complete", "76 projects and 10 clusters", "45 GKE disabled", "1 access denied", "1 billing disabled"} {
		if !strings.Contains(tr.Picker.Description, want) {
			t.Fatalf("missing %q: %s", want, tr.Picker.Description)
		}
	}
	if strings.Contains(tr.Picker.Description, "failed scopes") || !strings.Contains(tr.Picker.OperationDetails, "Failures: 0") {
		t.Fatal(tr.Picker.OperationDetails)
	}
	for _, tc := range []struct{ name, want string }{{"p0", "GKE disabled"}, {"p45", "Access denied"}, {"p46", "1 cluster"}, {"p56", "Billing disabled"}, {"p57", "none"}} {
		if got := kubernetesCountSummary(cfg, tc.name, "a"); got != tc.want {
			t.Fatalf("%s: %s", tc.name, got)
		}
	}
	p := interactiveBrowserPicker(cfg, cfg, ui.ScreenKubernetes, ui.Draft{ui.ScreenIdentity: "a", ui.ScreenProject: "p45"})
	if !strings.HasPrefix(p.Description, "Access denied") {
		t.Fatal(p.Description)
	}
}
