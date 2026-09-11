package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/infurio/contexthop/internal/kubetarget"
	"github.com/infurio/contexthop/internal/resolver"
)

const gkeVerificationCacheVersion = 1
const gkeVerificationTTL = 5 * time.Minute

type gkeVerificationCache struct {
	Version int                  `json:"version"`
	Entries map[string]time.Time `json:"entries"`
}

var gkeVerificationCacheMu sync.Mutex

func cachedGKEVerification(resolved resolver.Resolved, fingerprint kubetarget.Fingerprint, sourceRevision string) bool {
	if sourceRevision == "" {
		return false
	}
	key, ok := gkeVerificationKey(resolved, fingerprint, sourceRevision)
	if !ok {
		return false
	}
	gkeVerificationCacheMu.Lock()
	defer gkeVerificationCacheMu.Unlock()
	cache, err := loadGKEVerificationCache()
	if err != nil {
		return false
	}
	verifiedAt, ok := cache.Entries[key]
	return ok && time.Since(verifiedAt) >= 0 && time.Since(verifiedAt) <= gkeVerificationTTL
}

func recordGKEVerification(resolved resolver.Resolved, fingerprint kubetarget.Fingerprint, sourceRevision string) {
	if sourceRevision == "" {
		return
	}
	key, ok := gkeVerificationKey(resolved, fingerprint, sourceRevision)
	if !ok {
		return
	}
	gkeVerificationCacheMu.Lock()
	defer gkeVerificationCacheMu.Unlock()
	cache, err := loadGKEVerificationCache()
	if err != nil {
		cache = gkeVerificationCache{Version: gkeVerificationCacheVersion, Entries: map[string]time.Time{}}
	}
	now := time.Now().UTC()
	for cachedKey, verifiedAt := range cache.Entries {
		if now.Sub(verifiedAt) > gkeVerificationTTL || now.Before(verifiedAt) {
			delete(cache.Entries, cachedKey)
		}
	}
	cache.Entries[key] = now
	_ = writeGKEVerificationCache(cache)
}

func gkeVerificationKey(resolved resolver.Resolved, fingerprint kubetarget.Fingerprint, sourceRevision string) (string, bool) {
	if resolved.Identity == nil || resolved.Project == nil || resolved.Kubernetes == nil || fingerprint.Digest == "" {
		return "", false
	}
	values := []string{
		resolved.Identity.Provider, resolved.Identity.Account, resolved.Identity.CloudSDKConfig,
		resolved.Project.Provider, resolved.Project.ProjectID,
		resolved.Kubernetes.Cluster, resolved.Kubernetes.Location,
		sourceRevision, fingerprint.Digest,
	}
	digest := sha256.Sum256([]byte(joinCacheKey(values)))
	return hex.EncodeToString(digest[:]), true
}

func joinCacheKey(values []string) string {
	result := ""
	for _, value := range values {
		result += value + "\x00"
	}
	return result
}

func gkeVerificationCachePath() (string, error) {
	root := os.Getenv("CONTEXTHOP_CACHE_DIR")
	if root != "" {
		expanded, err := resolver.ExpandPath(root)
		if err != nil {
			return "", err
		}
		root = expanded
	} else {
		var err error
		root, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(root, "contexthop", "gke-verifications.json"), nil
}

func loadGKEVerificationCache() (gkeVerificationCache, error) {
	path, err := gkeVerificationCachePath()
	if err != nil {
		return gkeVerificationCache{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return gkeVerificationCache{Version: gkeVerificationCacheVersion, Entries: map[string]time.Time{}}, nil
	}
	if err != nil {
		return gkeVerificationCache{}, err
	}
	var cache gkeVerificationCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.Version != gkeVerificationCacheVersion {
		return gkeVerificationCache{Version: gkeVerificationCacheVersion, Entries: map[string]time.Time{}}, nil
	}
	if cache.Entries == nil {
		cache.Entries = map[string]time.Time{}
	}
	return cache, nil
}

func writeGKEVerificationCache(cache gkeVerificationCache) error {
	path, err := gkeVerificationCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".gke-verifications-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
