// Package lifecycle manages recoverable snapshots of ContextHop's portable
// configuration and non-secret local metadata.
package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
)

const manifestVersion = 1

var metadataFiles = []string{"catalog.json", "gke-verifications.json", "recency.json"}

// Backup describes one restorable lifecycle snapshot.
type Backup struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	Directory string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	Files     []string  `json:"files"`
}

// Create snapshots the validated configuration and any non-secret metadata.
// Credentials, generated kubeconfigs, and live session data are deliberately
// excluded.
func Create(configPath string) (Backup, error) {
	if _, err := config.Load(configPath); err != nil {
		return Backup{}, fmt.Errorf("back up configuration: %w", err)
	}
	root := backupRoot(configPath)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Backup{}, fmt.Errorf("create backup directory: %w", err)
	}
	now := time.Now().UTC()
	id := now.Format("20060102-150405.000000000")
	directory := filepath.Join(root, id)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return Backup{}, fmt.Errorf("create backup %s: %w", id, err)
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(directory)
		}
	}()

	backup := Backup{Version: manifestVersion, ID: id, Directory: directory, CreatedAt: now}
	if err := copyFile(configPath, filepath.Join(directory, "config.yaml")); err != nil {
		return Backup{}, err
	}
	backup.Files = append(backup.Files, "config.yaml")
	cacheRoot, err := CacheRoot()
	if err != nil {
		return Backup{}, err
	}
	for _, name := range metadataFiles {
		source := filepath.Join(cacheRoot, name)
		if _, statErr := os.Stat(source); os.IsNotExist(statErr) {
			continue
		} else if statErr != nil {
			return Backup{}, fmt.Errorf("inspect metadata %s: %w", name, statErr)
		}
		if err := copyFile(source, filepath.Join(directory, "metadata", name)); err != nil {
			return Backup{}, err
		}
		backup.Files = append(backup.Files, filepath.Join("metadata", name))
	}
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return Backup{}, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), data, 0o600); err != nil {
		return Backup{}, fmt.Errorf("write backup manifest: %w", err)
	}
	failed = false
	return backup, nil
}

// List returns valid backups in newest-first order.
func List(configPath string) ([]Backup, error) {
	root := backupRoot(configPath)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	var backups []Backup
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		backup, readErr := readBackup(filepath.Join(root, entry.Name()))
		if readErr == nil {
			backups = append(backups, backup)
		}
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].CreatedAt.After(backups[j].CreatedAt) })
	return backups, nil
}

// Resolve accepts a backup ID, "latest", or a backup directory path.
func Resolve(configPath, reference string) (Backup, error) {
	if reference == "" || reference == "latest" {
		backups, err := List(configPath)
		if err != nil {
			return Backup{}, err
		}
		if len(backups) == 0 {
			return Backup{}, errors.New("no ContextHop backups are available")
		}
		return backups[0], nil
	}
	directory := reference
	if !filepath.IsAbs(directory) && !strings.ContainsRune(directory, os.PathSeparator) {
		directory = filepath.Join(backupRoot(configPath), directory)
	}
	return readBackup(directory)
}

// Restore validates a snapshot before replacing the configuration and
// non-secret metadata. Transient session and generated kubeconfig data is not
// restored.
func Restore(configPath string, backup Backup) error {
	snapshotConfig := filepath.Join(backup.Directory, "config.yaml")
	cfg, err := config.Load(snapshotConfig)
	if err != nil {
		return fmt.Errorf("validate backup configuration: %w", err)
	}
	if err := config.Write(configPath, cfg); err != nil {
		return fmt.Errorf("restore configuration: %w", err)
	}
	cacheRoot, err := CacheRoot()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(cacheRoot); err != nil {
		return fmt.Errorf("clear current metadata: %w", err)
	}
	for _, name := range metadataFiles {
		source := filepath.Join(backup.Directory, "metadata", name)
		if _, statErr := os.Stat(source); os.IsNotExist(statErr) {
			continue
		} else if statErr != nil {
			return fmt.Errorf("inspect backup metadata %s: %w", name, statErr)
		}
		if err := copyFile(source, filepath.Join(cacheRoot, name)); err != nil {
			return err
		}
	}
	return nil
}

// ClearMetadata removes ContextHop's derived caches and session bookkeeping.
func ClearMetadata() error {
	root, err := CacheRoot()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("clear ContextHop metadata: %w", err)
	}
	return nil
}

// ClearCredentials removes only ContextHop's conventionally managed isolated
// gcloud credential root. It never follows paths supplied by configuration.
func ClearCredentials() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}
	root := filepath.Join(home, ".config", "contexthop", "gcloud")
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("clear ContextHop credentials: %w", err)
	}
	return nil
}

// CacheRoot returns the directory containing ContextHop's non-secret metadata.
func CacheRoot() (string, error) {
	root := os.Getenv("CONTEXTHOP_CACHE_DIR")
	if root != "" {
		expanded, err := resolver.ExpandPath(root)
		if err != nil {
			return "", fmt.Errorf("resolve configured cache directory: %w", err)
		}
		return filepath.Join(expanded, "contexthop"), nil
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find user cache directory: %w", err)
	}
	return filepath.Join(root, "contexthop"), nil
}

func backupRoot(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "backups")
}

func readBackup(directory string) (Backup, error) {
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return Backup{}, fmt.Errorf("read backup %q: %w", directory, err)
	}
	var backup Backup
	if err := json.Unmarshal(data, &backup); err != nil {
		return Backup{}, fmt.Errorf("decode backup %q: %w", directory, err)
	}
	if backup.Version != manifestVersion || backup.ID == "" || backup.CreatedAt.IsZero() {
		return Backup{}, fmt.Errorf("backup %q has an invalid manifest", directory)
	}
	backup.Directory = directory
	return backup, nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".copy-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return fmt.Errorf("copy %s: %w", source, err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("replace %s: %w", destination, err)
	}
	return nil
}
