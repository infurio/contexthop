package recency

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const version = 1

// History is local presentation state. It is deliberately separate from the
// portable ContextHop catalog.
type History struct {
	Version  int                             `json:"version"`
	LastUsed map[string]map[string]time.Time `json:"lastUsed"`
}

func Load() History {
	history := empty()
	path, err := historyPath()
	if err != nil {
		return history
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &history) != nil || history.Version != version {
		return empty()
	}
	if history.LastUsed == nil {
		history.LastUsed = map[string]map[string]time.Time{}
	}
	return history
}

func (h History) Time(kind, name string) time.Time {
	return h.LastUsed[kind][name]
}

// Record stores one successful resolved selection. Concurrent terminals are
// serialized so one activation cannot overwrite another terminal's recency.
func Record(resources map[string]string) error {
	path, err := historyPath()
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	history := Load()
	now := time.Now().UTC()
	for kind, name := range resources {
		if name == "" {
			continue
		}
		if history.LastUsed[kind] == nil {
			history.LastUsed[kind] = map[string]time.Time{}
		}
		history.LastUsed[kind][name] = now
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".recency-*.json")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
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
	return os.Rename(temporaryName, path)
}

func empty() History {
	return History{Version: version, LastUsed: map[string]map[string]time.Time{}}
}

func historyPath() (string, error) {
	if configured := os.Getenv("CONTEXTHOP_CACHE_DIR"); configured != "" {
		return filepath.Join(configured, "contexthop", "recency.json"), nil
	}
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "contexthop", "recency.json"), nil
}
