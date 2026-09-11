// Package catalog discovers provider resources without changing provider-global
// defaults and retains only non-secret discovery metadata between runs.
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

const cacheVersion = 1

const catalogFreshnessTTL = 24 * time.Hour

type Freshness string

const (
	FreshnessLive        Freshness = "live"
	FreshnessCached      Freshness = "cached"
	FreshnessStale       Freshness = "stale"
	FreshnessUnavailable Freshness = "unavailable"
)

type Project struct {
	Labels         map[string]string `json:"labels"`
	ProjectID      string            `json:"projectId"`
	Name           string            `json:"name,omitempty"`
	LifecycleState string            `json:"lifecycleState,omitempty"`
}

type Cluster struct {
	Labels    map[string]string `json:"resourceLabels"`
	ProjectID string            `json:"projectId"`
	Name      string            `json:"name"`
	Location  string            `json:"location"`
	Status    string            `json:"status,omitempty"`
}

type ProjectResult struct {
	Projects     []Project
	Freshness    Freshness
	LastAttempt  time.Time
	LastSuccess  time.Time
	RefreshError string
}

type ClusterResult struct {
	Clusters     []Cluster
	Freshness    Freshness
	LastAttempt  time.Time
	LastSuccess  time.Time
	RefreshError string
}

// Client owns a metadata cache. Provider refresh failures are returned as an
// error as well as recorded in the result and cache, so callers can keep
// showing the last successful enumeration without mistaking it for live data.
type Client struct {
	cachePath string
	now       func() time.Time
	mu        sync.Mutex
}

func New(cachePath string) *Client {
	return &Client{cachePath: cachePath, now: time.Now}
}

func NewDefault() (*Client, error) {
	if configured := os.Getenv("CONTEXTHOP_CACHE_DIR"); configured != "" {
		cacheDir, err := resolver.ExpandPath(configured)
		if err != nil {
			return nil, fmt.Errorf("resolve configured cache directory: %w", err)
		}
		return New(filepath.Join(cacheDir, "contexthop", "catalog.json")), nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("find user cache directory: %w", err)
	}
	return New(filepath.Join(cacheDir, "contexthop", "catalog.json")), nil
}

func (c *Client) CachedProjects(identity config.Identity) (ProjectResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	identityKey, err := canonicalIdentityKey(identity)
	if err != nil {
		return ProjectResult{}, err
	}
	cache, err := c.load()
	if err != nil {
		return ProjectResult{}, err
	}
	return projectResult(cache.Entries[entryKey(identityKey, "projects", "")], false, c.now()), nil
}

func (c *Client) CachedClusters(identity config.Identity, projectID string) (ClusterResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	identityKey, err := canonicalIdentityKey(identity)
	if err != nil {
		return ClusterResult{}, err
	}
	cache, err := c.load()
	if err != nil {
		return ClusterResult{}, err
	}
	return clusterResult(cache.Entries[entryKey(identityKey, "clusters", projectID)], false, c.now()), nil
}

func (c *Client) RefreshProjects(ctx context.Context, identity config.Identity) (ProjectResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	identityKey, environment, err := identityEnvironment(identity, "")
	if err != nil {
		return ProjectResult{}, err
	}
	cache, err := c.load()
	if err != nil {
		return ProjectResult{}, err
	}
	key := entryKey(identityKey, "projects", "")
	entry := cache.Entries[key]
	entry.Kind, entry.IdentityKey, entry.Scope = "projects", identityKey, ""
	entry.LastAttempt = c.now().UTC()

	output, refreshErr := run(ctx, environment, "gcloud", "projects", "list", "--account", identity.Account, "--format=json(projectId,name,lifecycleState,labels)", "--quiet")
	if refreshErr == nil {
		var projects []Project
		if err := decodeEnumeration(output, &projects); err != nil {
			refreshErr = fmt.Errorf("decode GCP projects: %w", err)
		} else {
			for _, project := range projects {
				if strings.TrimSpace(project.ProjectID) == "" {
					refreshErr = errors.New("incomplete project enumeration: project ID missing")
					break
				}
			}
			if refreshErr == nil {
				projects = validProjects(projects)
				entry.Projects = projects
				entry.LastSuccess = entry.LastAttempt
				entry.RefreshError = ""
			}
		}
	}
	if refreshErr != nil {
		entry.RefreshError = safeError(refreshErr)
	}
	cache.Entries[key] = entry
	if err := c.write(cache); err != nil {
		if refreshErr != nil {
			return projectResult(entry, false, c.now()), fmt.Errorf("%w; record refresh result: %v", refreshErr, err)
		}
		return ProjectResult{}, err
	}
	return projectResult(entry, refreshErr == nil, c.now()), refreshErr
}

func (c *Client) RefreshClusters(ctx context.Context, identity config.Identity, projectID string) (ClusterResult, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ClusterResult{}, errors.New("GCP project ID is required for cluster enumeration")
	}
	identityKey, environment, err := identityEnvironment(identity, projectID)
	if err != nil {
		return ClusterResult{}, err
	}
	output, refreshErr := run(ctx, environment, "gcloud", "container", "clusters", "list", "--project", projectID, "--account", identity.Account, "--format=json(name,location,status,resourceLabels)", "--quiet")
	// Cloud requests run concurrently; cache read-modify-write remains serialized.
	c.mu.Lock()
	defer c.mu.Unlock()
	cache, err := c.load()
	if err != nil {
		return ClusterResult{}, err
	}
	key := entryKey(identityKey, "clusters", projectID)
	entry := cache.Entries[key]
	entry.Kind, entry.IdentityKey, entry.Scope = "clusters", identityKey, projectID
	entry.LastAttempt = c.now().UTC()

	if refreshErr == nil {
		var discovered []struct {
			Labels   map[string]string `json:"resourceLabels"`
			Name     string            `json:"name"`
			Location string            `json:"location"`
			Zone     string            `json:"zone"`
			Status   string            `json:"status"`
		}
		if err := decodeEnumeration(output, &discovered); err != nil {
			refreshErr = fmt.Errorf("decode GKE clusters for project %q: %w", projectID, err)
		} else {
			clusters := make([]Cluster, 0, len(discovered))
			for _, item := range discovered {
				location := item.Location
				if location == "" {
					location = item.Zone
				}
				if item.Name == "" || location == "" {
					refreshErr = errors.New("incomplete GKE cluster enumeration: cluster name or location missing")
					break
				}
				clusters = append(clusters, Cluster{Labels: item.Labels, ProjectID: projectID, Name: item.Name, Location: location, Status: item.Status})
			}
			sort.Slice(clusters, func(i, j int) bool {
				if clusters[i].Name == clusters[j].Name {
					return clusters[i].Location < clusters[j].Location
				}
				return clusters[i].Name < clusters[j].Name
			})
			if refreshErr == nil {
				entry.Clusters = clusters
				entry.LastSuccess = entry.LastAttempt
				entry.RefreshError = ""
			}
		}
	}
	if refreshErr != nil {
		entry.RefreshError = safeError(refreshErr)
	}
	cache.Entries[key] = entry
	if err := c.write(cache); err != nil {
		if refreshErr != nil {
			return clusterResult(entry, false, c.now()), fmt.Errorf("%w; record refresh result: %v", refreshErr, err)
		}
		return ClusterResult{}, err
	}
	return clusterResult(entry, refreshErr == nil, c.now()), refreshErr
}

// Clear removes all cached discovery metadata without touching provider state.
func (c *Client) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(c.cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear catalog cache: %w", err)
	}
	return nil
}

// ClearIdentity removes only metadata collected with the selected identity.
func (c *Client) ClearIdentity(identity config.Identity) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	identityKey, err := canonicalIdentityKey(identity)
	if err != nil {
		return err
	}
	cache, err := c.load()
	if err != nil {
		return err
	}
	for key, entry := range cache.Entries {
		if entry.IdentityKey == identityKey {
			delete(cache.Entries, key)
		}
	}
	if len(cache.Entries) == 0 {
		if err := os.Remove(c.cachePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear identity catalog cache: %w", err)
		}
		return nil
	}
	return c.write(cache)
}

type cacheFile struct {
	Version int                   `json:"version"`
	Entries map[string]cacheEntry `json:"entries"`
}

type cacheEntry struct {
	Kind         string    `json:"kind"`
	IdentityKey  string    `json:"identityKey"`
	Scope        string    `json:"scope,omitempty"`
	Projects     []Project `json:"projects,omitempty"`
	Clusters     []Cluster `json:"clusters,omitempty"`
	LastAttempt  time.Time `json:"lastAttempt"`
	LastSuccess  time.Time `json:"lastSuccess,omitempty"`
	RefreshError string    `json:"refreshError,omitempty"`
}

func (c *Client) load() (cacheFile, error) {
	cache := cacheFile{Version: cacheVersion, Entries: map[string]cacheEntry{}}
	data, err := os.ReadFile(c.cachePath)
	if os.IsNotExist(err) {
		return cache, nil
	}
	if err != nil {
		return cacheFile{}, fmt.Errorf("read catalog cache: %w", err)
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return cacheFile{}, fmt.Errorf("decode catalog cache: %w", err)
	}
	if cache.Version != cacheVersion {
		return cacheFile{}, fmt.Errorf("unsupported catalog cache version %d", cache.Version)
	}
	if cache.Entries == nil {
		cache.Entries = map[string]cacheEntry{}
	}
	return cache, nil
}

func (c *Client) write(cache cacheFile) error {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode catalog cache: %w", err)
	}
	directory := filepath.Dir(c.cachePath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create catalog cache directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure catalog cache directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".catalog-*.json")
	if err != nil {
		return fmt.Errorf("create catalog cache staging file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure catalog cache staging file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write catalog cache staging file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync catalog cache staging file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close catalog cache staging file: %w", err)
	}
	if err := os.Rename(temporaryPath, c.cachePath); err != nil {
		return fmt.Errorf("replace catalog cache: %w", err)
	}
	if err := os.Chmod(c.cachePath, 0o600); err != nil {
		return fmt.Errorf("secure catalog cache: %w", err)
	}
	return nil
}

func identityEnvironment(identity config.Identity, projectID string) (string, []string, error) {
	identityKey, err := canonicalIdentityKey(identity)
	if err != nil {
		return "", nil, err
	}
	configDir, err := resolver.ExpandPath(identity.CloudSDKConfig)
	if err != nil {
		return "", nil, err
	}
	environment := environmentMap(os.Environ())
	for _, key := range []string{
		"CLOUDSDK_ACTIVE_CONFIG_NAME",
		"CLOUDSDK_AUTH_ACCESS_TOKEN", "CLOUDSDK_AUTH_ACCESS_TOKEN_FILE",
		"CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE",
		"CLOUDSDK_AUTH_DISABLE_CREDENTIALS",
		"CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT",
		"CLOUDSDK_BILLING_QUOTA_PROJECT",
		"CLOUDSDK_CONFIG",
		"CLOUDSDK_CORE_ACCOUNT",
		"CLOUDSDK_CORE_PROJECT",
		"CLOUDSDK_CORE_QUOTA_PROJECT",
		"GCLOUD_PROJECT",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"GOOGLE_CLOUD_PROJECT",
		"GOOGLE_CLOUD_QUOTA_PROJECT",
	} {
		delete(environment, key)
	}
	environment["CLOUDSDK_CONFIG"] = configDir
	environment["CLOUDSDK_CORE_ACCOUNT"] = identity.Account
	if projectID != "" {
		environment["CLOUDSDK_CORE_PROJECT"] = projectID
	}
	return identityKey, flattenEnvironment(environment), nil
}

func canonicalIdentityKey(identity config.Identity) (string, error) {
	if identity.Provider != "gcp" {
		return "", fmt.Errorf("catalog provider %q is not implemented", identity.Provider)
	}
	account := strings.ToLower(strings.TrimSpace(identity.Account))
	if account == "" {
		return "", errors.New("GCP account is required for catalog discovery")
	}
	configDir, err := resolver.ExpandPath(identity.CloudSDKConfig)
	if err != nil {
		return "", err
	}
	if configDir == "" {
		return "", errors.New("isolated cloudSdkConfig is required for catalog discovery")
	}
	digest := sha256.Sum256([]byte("gcp\x00" + account + "\x00" + configDir))
	return "gcp:" + hex.EncodeToString(digest[:]), nil
}

func entryKey(identityKey, kind, scope string) string {
	digest := sha256.Sum256([]byte(identityKey + "\x00" + kind + "\x00" + scope))
	return hex.EncodeToString(digest[:])
}

func projectResult(entry cacheEntry, live bool, now time.Time) ProjectResult {
	freshness := cachedFreshness(entry, live, now)
	return ProjectResult{
		Projects: append([]Project(nil), entry.Projects...), Freshness: freshness,
		LastAttempt: entry.LastAttempt, LastSuccess: entry.LastSuccess, RefreshError: entry.RefreshError,
	}
}

func clusterResult(entry cacheEntry, live bool, now time.Time) ClusterResult {
	freshness := cachedFreshness(entry, live, now)
	return ClusterResult{
		Clusters: append([]Cluster(nil), entry.Clusters...), Freshness: freshness,
		LastAttempt: entry.LastAttempt, LastSuccess: entry.LastSuccess, RefreshError: entry.RefreshError,
	}
}

func cachedFreshness(entry cacheEntry, live bool, now time.Time) Freshness {
	if live {
		return FreshnessLive
	}
	if entry.RefreshError != "" && !entry.LastSuccess.IsZero() {
		return FreshnessStale
	}
	if !entry.LastSuccess.IsZero() {
		if now.Sub(entry.LastSuccess) > catalogFreshnessTTL {
			return FreshnessStale
		}
		return FreshnessCached
	}
	return FreshnessUnavailable
}

func validProjects(projects []Project) []Project {
	result := projects[:0]
	for _, project := range projects {
		project.ProjectID = strings.TrimSpace(project.ProjectID)
		if project.ProjectID != "" {
			result = append(result, project)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ProjectID < result[j].ProjectID })
	return result
}

type commandError struct {
	command string
	detail  string
	err     error
}

func (e *commandError) Error() string {
	if e.detail == "" {
		return fmt.Sprintf("%s: %v", e.command, e.err)
	}
	return fmt.Sprintf("%s: %v: %s", e.command, e.err, e.detail)
}

func (e *commandError) Unwrap() error { return e.err }

func run(ctx context.Context, environment []string, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = environment
	output, err := command.CombinedOutput()
	if err == nil {
		return output, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, &commandError{command: name, detail: safeText(string(output)), err: err}
}

func safeError(err error) string { return safeText(err.Error()) }

func safeText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const limit = 300
	if len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}

func environmentMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, item, ok := strings.Cut(value, "=")
		if ok {
			result[key] = item
		}
	}
	return result
}

func flattenEnvironment(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

// CombinedOutput intentionally makes provider warnings fail JSON decoding: a
// zero exit code with warnings about unreachable locations is not a complete
// enumeration and must never remove prior scope membership.
func decodeEnumeration(output []byte, destination any) error {
	if !strings.HasPrefix(strings.TrimSpace(string(output)), "[") {
		return errors.New("provider enumeration did not return a complete array")
	}
	return json.Unmarshal(output, destination)
}
