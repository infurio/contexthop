package catalog

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/infurio/contexthop/internal/config"
)

func TestGCPEnumerationUsesIsolatedIdentityAndExplicitProject(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "gcloud.log")
	configDir := filepath.Join(t.TempDir(), "identity-gcloud")
	writeFakeGcloud(t, bin, `
printf '%s|%s|%s|%s\n' "$*" "$CLOUDSDK_CONFIG" "$CLOUDSDK_CORE_ACCOUNT" "$CLOUDSDK_CORE_PROJECT" >> "$FAKE_GCLOUD_LOG"
[ "$CLOUDSDK_CONFIG" = "$EXPECTED_CONFIG" ] || exit 81
[ "$CLOUDSDK_CORE_ACCOUNT" = "person@example.com" ] || exit 82
[ -z "${CLOUDSDK_ACTIVE_CONFIG_NAME+x}" ] || exit 83
[ -z "${CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE+x}" ] || exit 84
[ -z "${CLOUDSDK_AUTH_ACCESS_TOKEN+x}" ] || exit 97
[ -z "${CLOUDSDK_AUTH_ACCESS_TOKEN_FILE+x}" ] || exit 85
[ -z "${CLOUDSDK_AUTH_DISABLE_CREDENTIALS+x}" ] || exit 86
[ -z "${CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT+x}" ] || exit 87
[ -z "${CLOUDSDK_BILLING_QUOTA_PROJECT+x}" ] || exit 88
[ -z "${CLOUDSDK_CORE_QUOTA_PROJECT+x}" ] || exit 89
[ -z "${GOOGLE_APPLICATION_CREDENTIALS+x}" ] || exit 90
[ -z "${GOOGLE_CLOUD_PROJECT+x}" ] || exit 91
[ -z "${GOOGLE_CLOUD_QUOTA_PROJECT+x}" ] || exit 92
[ -z "${GCLOUD_PROJECT+x}" ] || exit 93
case "$*" in
  "projects list --account person@example.com --format=json(projectId,name,lifecycleState,labels) --quiet")
	[ -z "${CLOUDSDK_CORE_PROJECT+x}" ] || exit 94
    printf '[{"projectId":"z-project","name":"Zed","lifecycleState":"ACTIVE"},{"projectId":"a-project","name":"Alpha","lifecycleState":"ACTIVE","labels":{"env":"production"}}]\n'
    ;;
  "container clusters list --project a-project --account person@example.com --format=json(name,location,status,resourceLabels) --quiet")
	[ "$CLOUDSDK_CORE_PROJECT" = "a-project" ] || exit 95
    printf '[{"name":"z-cluster","location":"us-east1","status":"RUNNING"},{"name":"a-cluster","zone":"us-central1-a","status":"RUNNING","resourceLabels":{"team":"payments"}}]\n'
    ;;
	*) exit 96 ;;
esac
`)
	t.Setenv("PATH", bin)
	t.Setenv("FAKE_GCLOUD_LOG", logPath)
	t.Setenv("EXPECTED_CONFIG", configDir)
	t.Setenv("CLOUDSDK_CONFIG", "/global/gcloud")
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "wrong@example.com")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "wrong-project")
	t.Setenv("CLOUDSDK_ACTIVE_CONFIG_NAME", "dangerous-global")
	t.Setenv("CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE", "/global/credential.json")
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "dummy-token")
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN_FILE", "/global/token")
	t.Setenv("CLOUDSDK_AUTH_DISABLE_CREDENTIALS", "true")
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "service@example.com")
	t.Setenv("CLOUDSDK_BILLING_QUOTA_PROJECT", "wrong-billing-project")
	t.Setenv("CLOUDSDK_CORE_QUOTA_PROJECT", "wrong-core-quota-project")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/global/adc.json")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "wrong-google-project")
	t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "wrong-google-quota-project")
	t.Setenv("GCLOUD_PROJECT", "wrong-gcloud-project")

	client := New(filepath.Join(t.TempDir(), "catalog.json"))
	identity := config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: configDir}
	projects, err := client.RefreshProjects(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if projects.Projects[0].Labels["env"] != "production" {
		t.Fatal("project labels missing", projects)
	}
	if projects.Freshness != FreshnessLive || len(projects.Projects) != 2 || projects.Projects[0].ProjectID != "a-project" {
		t.Fatalf("projects = %#v", projects)
	}
	clusters, err := client.RefreshClusters(context.Background(), identity, "a-project")
	if err != nil {
		t.Fatal(err)
	}
	if clusters.Clusters[0].Labels["team"] != "payments" {
		t.Fatal("cluster labels missing", clusters)
	}
	if clusters.Freshness != FreshnessLive || len(clusters.Clusters) != 2 || clusters.Clusters[0].Name != "a-cluster" || clusters.Clusters[0].Location != "us-central1-a" {
		t.Fatalf("clusters = %#v", clusters)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logData), "/global/") || !strings.Contains(string(logData), configDir+"|person@example.com|a-project") {
		t.Fatalf("gcloud log = %s", logData)
	}
	info, err := os.Stat(client.cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("cache permissions = %o", info.Mode().Perm())
	}
}

func TestPermissionFailureRetainsLastSuccessfulEnumeration(t *testing.T) {
	bin := t.TempDir()
	writeFakeGcloud(t, bin, `
if [ "$FAKE_GCLOUD_MODE" = "denied" ]; then
  printf 'ERROR: permission denied for selected account\n' >&2
  exit 1
fi
printf '[{"projectId":"remembered-project","name":"Remembered","lifecycleState":"ACTIVE"}]\n'
`)
	t.Setenv("PATH", bin)
	t.Setenv("FAKE_GCLOUD_MODE", "success")
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	client := New(cachePath)
	firstAttempt := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return firstAttempt }
	identity := config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: filepath.Join(t.TempDir(), "gcloud")}

	first, err := client.RefreshProjects(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Projects) != 1 || !first.LastSuccess.Equal(firstAttempt) {
		t.Fatalf("first result = %#v", first)
	}

	failedAttempt := firstAttempt.Add(time.Hour)
	client.now = func() time.Time { return failedAttempt }
	t.Setenv("FAKE_GCLOUD_MODE", "denied")
	failed, err := client.RefreshProjects(context.Background(), identity)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("refresh error = %v", err)
	}
	if failed.Freshness != FreshnessStale || len(failed.Projects) != 1 || failed.Projects[0].ProjectID != "remembered-project" {
		t.Fatalf("failed result = %#v", failed)
	}
	if !failed.LastSuccess.Equal(firstAttempt) || !failed.LastAttempt.Equal(failedAttempt) || !strings.Contains(failed.RefreshError, "permission denied") {
		t.Fatalf("failed metadata = %#v", failed)
	}

	reopened := New(cachePath)
	cached, err := reopened.CachedProjects(identity)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Freshness != FreshnessStale || len(cached.Projects) != 1 || cached.RefreshError == "" {
		t.Fatalf("reopened cache = %#v", cached)
	}
}

func TestClearIdentityRemovesOnlySelectedIdentityMetadata(t *testing.T) {
	bin := t.TempDir()
	writeFakeGcloud(t, bin, `printf '[{"projectId":"project","name":"Project","lifecycleState":"ACTIVE"}]\n'`)
	t.Setenv("PATH", bin)
	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	client := New(cachePath)
	first := config.Identity{Provider: "gcp", Account: "first@example.com", CloudSDKConfig: filepath.Join(t.TempDir(), "first")}
	second := config.Identity{Provider: "gcp", Account: "second@example.com", CloudSDKConfig: filepath.Join(t.TempDir(), "second")}
	if _, err := client.RefreshProjects(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RefreshProjects(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if err := client.ClearIdentity(first); err != nil {
		t.Fatal(err)
	}
	firstCached, err := client.CachedProjects(first)
	if err != nil {
		t.Fatal(err)
	}
	secondCached, err := client.CachedProjects(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstCached.Freshness != FreshnessUnavailable || secondCached.Freshness != FreshnessCached {
		t.Fatalf("after identity clear: first=%#v second=%#v", firstCached, secondCached)
	}
	if err := client.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("cache still exists after clear: %v", err)
	}
}

func TestNewDefaultHonorsConfiguredCacheDirectory(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("CONTEXTHOP_CACHE_DIR", cacheRoot)
	client, err := NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cacheRoot, "contexthop", "catalog.json")
	if client.cachePath != want {
		t.Fatalf("cache path = %q, want %q", client.cachePath, want)
	}
}

func TestCachedFreshnessBecomesStaleAfterTTL(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	entry := cacheEntry{LastSuccess: now.Add(-catalogFreshnessTTL - time.Minute)}
	if got := cachedFreshness(entry, false, now); got != FreshnessStale {
		t.Fatalf("freshness = %q, want %q", got, FreshnessStale)
	}
	if got := cachedFreshness(entry, true, now); got != FreshnessLive {
		t.Fatalf("live freshness = %q", got)
	}
}

func writeFakeGcloud(t *testing.T, directory, body string) {
	t.Helper()
	path := filepath.Join(directory, "gcloud")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestIncompleteEnumerationNeverReplacesCachedRelationships(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	identity := config.Identity{Provider: "gcp", Account: "person@example.com", CloudSDKConfig: filepath.Join(t.TempDir(), "profile")}
	for _, kind := range []string{"projects", "clusters"} {
		t.Run(kind, func(t *testing.T) {
			client := New(filepath.Join(t.TempDir(), "cache.json"))
			success := "[{\"projectId\":\"p\"}]"
			if kind == "clusters" {
				success = "[{\"name\":\"k\",\"location\":\"region\"}]"
			}
			writeFakeGcloud(t, bin, "printf '%s' '"+success+"'\n")
			refresh := func() (int, Freshness, error) {
				if kind == "projects" {
					r, e := client.RefreshProjects(context.Background(), identity)
					return len(r.Projects), r.Freshness, e
				}
				r, e := client.RefreshClusters(context.Background(), identity, "p")
				return len(r.Clusters), r.Freshness, e
			}
			if _, _, err := refresh(); err != nil {
				t.Fatal(err)
			}
			for _, output := range []string{"null", "[{}]", "WARNING: unreachable location\n[]"} {
				writeFakeGcloud(t, bin, "printf '%s' '"+output+"'\n")
				count, freshness, err := refresh()
				if err == nil || count != 1 || freshness != FreshnessStale {
					t.Fatalf("partial %s response discarded cache: %d %s %v", kind, count, freshness, err)
				}
			}
		})
	}
}

func TestConcurrentClusterRequestsKeepEveryCacheEntry(t *testing.T) {
	bin, gate := t.TempDir(), t.TempDir()
	t.Setenv("DISCOVERY_GATE", gate)
	writeFakeGcloud(t, bin, `
: > "$DISCOVERY_GATE/$CLOUDSDK_CORE_PROJECT"
while [ ! -f "$DISCOVERY_GATE/a" ] || [ ! -f "$DISCOVERY_GATE/b" ] || [ ! -f "$DISCOVERY_GATE/c" ]; do
 /bin/sleep 0.01
done
printf '[{"name":"cluster","location":"region"}]'
`)
	t.Setenv("PATH", bin)
	client := New(filepath.Join(t.TempDir(), "cache.json"))
	identity := config.Identity{Provider: "gcp", Account: "a@example.com", CloudSDKConfig: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 3)
	for _, project := range []string{"a", "b", "c"} {
		go func() { _, err := client.RefreshClusters(ctx, identity, project); results <- err }()
	}
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	for _, project := range []string{"a", "b", "c"} {
		result, err := client.CachedClusters(identity, project)
		if err != nil || len(result.Clusters) != 1 {
			t.Fatalf("cache lost %s: %v %v", project, result, err)
		}
	}
}
