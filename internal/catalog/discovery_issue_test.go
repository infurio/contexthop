package catalog

import (
	"bytes"
	"context"
	"errors"
	"github.com/infurio/contexthop/internal/config"
	"testing"
	"time"
)

func TestClusterDiscoveryErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want config.DiscoveryIssue
	}{
		{nil, ""},
		{errors.New("code=403, This API method requires billing to be enabled. Please enable billing on project #team-b-testing"), config.DiscoveryBillingDisabled},
		{errors.New("PERMISSION_DENIED reason: BILLING_DISABLED"), config.DiscoveryBillingDisabled},
		{errors.New("permission denied to manage billing account"), config.DiscoveryAccessDenied},
		{errors.New("code=403, Kubernetes Engine API has not been used in project p before or it is disabled"), config.DiscoveryGKEDisabled},
		{errors.New("SERVICE_DISABLED: container.googleapis.com"), config.DiscoveryGKEDisabled},
		{errors.New(`code=403, Required "container.clusters.list" permission(s) for "projects/p"`), config.DiscoveryAccessDenied},
		{errors.New("PERMISSION_DENIED"), config.DiscoveryAccessDenied},
		{errors.New("permission denied"), config.DiscoveryAccessDenied},
		{errors.New("Reauthentication failed"), config.DiscoveryScanFailed},
		{errors.New("code=403: unrelated policy"), config.DiscoveryScanFailed},
		{context.DeadlineExceeded, config.DiscoveryScanFailed},
		{context.Canceled, config.DiscoveryScanFailed},
	} {
		if got := ClassifyClusterDiscoveryError(tc.err); got != tc.want {
			t.Errorf("%v: got %q want %q", tc.err, got, tc.want)
		}
	}
}
func TestDiscoveryIssuePersistsAndClearsWithoutLosingMembership(t *testing.T) {
	cfg := config.New()
	cfg.Identities["i"] = config.Identity{Provider: "gcp", Account: "me@example.com"}
	RecordDiscovery(cfg, "i", "p", []string{"region/cluster"}, time.Now(), true)
	RecordDiscovery(cfg, "i", "p", nil, time.Time{}, false, errors.New("PERMISSION_DENIED"))
	scope := cfg.DiscoveryFor("i").Clusters["p"]
	if scope.Issue != config.DiscoveryAccessDenied || len(scope.Resources) != 1 || !scope.Stale {
		t.Fatal(scope)
	}
	var b bytes.Buffer
	if err := config.Encode(&b, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Decode(&b)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DiscoveryFor("i").Clusters["p"].Issue != config.DiscoveryAccessDenied {
		t.Fatal("issue lost on reload")
	}
	RecordDiscovery(loaded, "i", "p", nil, time.Now(), true)
	scope = loaded.DiscoveryFor("i").Clusters["p"]
	if scope.Issue != "" || scope.Stale || len(scope.Resources) != 0 {
		t.Fatal("successful empty scan did not clear old issue", scope)
	}
}
