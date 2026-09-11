package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/state"
)

const ScopeEnv = "CONTEXTHOP_SCOPE"
const SharedRevisionEnv = "CONTEXTHOP_SHARED_REVISION"

type sharedDefault struct {
	Manifest string `json:"manifest"`
}

func sharedRoot() (string, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "shared-default"), nil
}

func withSharedLock(run func(string) error) error {
	root, err := sharedRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(root, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return run(root)
}

func validateCloneSource(directory, id string) error {
	if err := validateSessionDirectory(directory, id); err == nil {
		return nil
	}
	root, err := sharedRoot()
	if err != nil {
		return err
	}
	if id == "" || filepath.Base(id) != id || id == "." || id == ".." || filepath.Clean(directory) != filepath.Join(root, "versions", id) {
		return fmt.Errorf("invalid shared session source")
	}
	return nil
}

func readShared(root string) (sharedDefault, error) {
	var value sharedDefault
	data, err := os.ReadFile(filepath.Join(root, "default.json"))
	if os.IsNotExist(err) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, err
	}
	if value.Manifest != "" {
		id := filepath.Base(filepath.Dir(value.Manifest))
		if value.Manifest != filepath.Join(root, "versions", id, "session.json") || id == "." || id == ".." {
			return value, fmt.Errorf("invalid shared config path")
		}
	}
	return value, nil
}

// PublishShared stores an immutable, already verified snapshot in chop's own config directory.
func PublishShared(prepared *Session) error {
	return withSharedLock(func(root string) error {
		old, err := readShared(root)
		if err != nil {
			return err
		}
		snapshot, err := cloneSession(prepared.Manifest, filepath.Join(root, "versions"))
		if err != nil {
			return err
		}
		if err := snapshot.Commit(); err != nil {
			snapshot.Close()
			return err
		}
		if err := writeJSONAtomic(filepath.Join(root, "default.json"), sharedDefault{Manifest: snapshot.Manifest}); err != nil {
			snapshot.Close()
			return err
		}
		if old.Manifest != "" {
			_ = os.RemoveAll(filepath.Dir(old.Manifest))
		}
		return nil
	})
}

// FollowShared is local-only. A changed revision gets an independent session;
// unchanged prompts do not copy files or invoke cloud authentication.
func FollowShared(revision string) (*Session, string, error) {
	root, err := sharedRoot()
	if err != nil {
		return nil, "", err
	}
	if _, err := os.Stat(filepath.Join(root, "default.json")); os.IsNotExist(err) {
		return nil, "", nil
	} else if err != nil {
		return nil, "", err
	}

	var prepared *Session
	var next string
	err = withSharedLock(func(root string) error {
		current, err := readShared(root)
		if err != nil {
			return err
		}
		if current.Manifest == "" {
			return nil
		}
		next = filepath.Base(filepath.Dir(current.Manifest))
		if next == revision {
			return nil
		}
		prepared, err = cloneSession(current.Manifest, "")
		return err
	})
	return prepared, next, err
}

func ClearShared() error {
	return withSharedLock(func(root string) error {
		current, err := readShared(root)
		if err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(root, "default.json")); err != nil && !os.IsNotExist(err) {
			return err
		}
		if current.Manifest != "" {
			return os.RemoveAll(filepath.Dir(current.Manifest))
		}
		return nil
	})
}

func (s *Session) SetScope(scope, revision string) {
	environment := environmentMap(s.Env)
	environment[ScopeEnv] = scope
	environment[SharedRevisionEnv] = revision
	s.Env = flattenEnvironment(environment)
}

// ShellBaseline returns quoted bindings so the integration can restore the
// environment it saw before following a shared default, including unset values.
func ShellBaseline() string {
	var text strings.Builder
	environment := environmentMap(os.Environ())
	for _, key := range managedEnvironmentKeys() {
		writeShellBinding(&text, key, environment)
	}
	return text.String()
}

// SharedConfig reads the published snapshot without copying it or invoking providers.
func SharedConfig() (*state.Manifest, error) {
	root, err := sharedRoot()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(root, "default.json")); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var manifest *state.Manifest
	err = withSharedLock(func(root string) error {
		current, err := readShared(root)
		if err != nil || current.Manifest == "" {
			return err
		}
		value, err := state.LoadManifest(current.Manifest)
		if err == nil {
			manifest = &value
		}
		return err
	})
	return manifest, err
}
