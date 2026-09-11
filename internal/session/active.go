package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/infurio/contexthop/internal/kubetarget"
	"github.com/infurio/contexthop/internal/state"
)

// Active describes a managed context currently owned by a live shell.
type Active struct {
	Key         string          `json:"key"`
	ShellPID    int             `json:"shellPid"`
	ShellStart  string          `json:"shellStart"`
	Manifest    string          `json:"manifest"`
	Destination string          `json:"destination"`
	Expected    state.Component `json:"expected"`
	ActivatedAt time.Time       `json:"activatedAt"`
	LastSeen    time.Time       `json:"lastSeen"`
}

// RegisterActive records a child shell as the owner of a managed session.
func RegisterActive(shellPID int, manifestPath string) error {
	if shellPID <= 0 || manifestPath == "" {
		return nil
	}
	manifest, err := state.LoadManifest(manifestPath)
	if err != nil {
		return err
	}
	root, err := activeRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	path := filepath.Join(root, strconv.Itoa(shellPID)+".json")
	now := time.Now()
	record := Active{ShellPID: shellPID, Manifest: manifestPath, Destination: manifest.Destination, Expected: manifest.Expected, ActivatedAt: now, LastSeen: now}
	if existing, readErr := readActive(path); readErr == nil && existing.Manifest == manifestPath {
		record.ActivatedAt = existing.ActivatedAt
		record.ShellStart = existing.ShellStart
	}
	if record.ShellStart == "" {
		record.ShellStart, _ = processStart(shellPID)
	}
	record.Key = activeKey(shellPID, record.ShellStart)
	return writeJSONAtomic(path, record)
}

// UnregisterActive removes the live-shell record for a session.
func UnregisterActive(manifestPath string) error {
	return removeActiveManifest(manifestPath)
}

// CleanupShellSession removes the context currently registered to a child
// shell. The owning ContextHop process calls it after that shell exits.
func CleanupShellSession(shellPID int) error {
	root, err := activeRoot()
	if err != nil {
		return err
	}
	path := filepath.Join(root, strconv.Itoa(shellPID)+".json")
	record, err := readActive(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		_ = os.Remove(path)
		return nil
	}
	return CleanupManifest(record.Manifest)
}

// ActiveForShell returns the session currently owned by a child shell. The
// record may point at a context selected in place after that shell started.
func ActiveForShell(shellPID int) (Active, error) {
	root, err := activeRoot()
	if err != nil {
		return Active{}, err
	}
	return readActive(filepath.Join(root, strconv.Itoa(shellPID)+".json"))
}

// ActiveSessions returns live sessions other than the calling shell. Stale
// registry entries are removed opportunistically.
func ActiveSessions(excludeShellPID int) ([]Active, error) {
	root, err := activeRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []Active
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		record, readErr := readActive(path)
		if readErr != nil || !activeAlive(record) {
			_ = os.Remove(path)
			continue
		}
		if record.ShellPID != excludeShellPID {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ActivatedAt.After(result[j].ActivatedAt) })
	return result, nil
}

// FindActive rechecks a selected session immediately before it is cloned.
func FindActive(key string, excludeShellPID int) (Active, error) {
	records, err := ActiveSessions(excludeShellPID)
	if err != nil {
		return Active{}, err
	}
	for _, record := range records {
		if record.Key == key {
			return record, nil
		}
	}
	return Active{}, errors.New("the selected session is no longer active")
}

// Reuse clones a live session into a new independent staged session. It does
// not authenticate, discover resources, or contact a provider.
func Reuse(record Active) (*Session, error) {
	if !activeAlive(record) {
		return nil, errors.New("the selected session is no longer active")
	}
	return cloneSession(record.Manifest, "")
}

// cloneSession makes an independent copy; a nonempty root is used for durable shared snapshots.
func cloneSession(manifestPath, root string) (*Session, error) {
	sourceManifest, err := state.LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read reusable session: %w", err)
	}
	sourceDir := filepath.Dir(manifestPath)
	if err := validateCloneSource(sourceDir, sourceManifest.SessionID); err != nil {
		return nil, err
	}
	environmentData, err := os.ReadFile(filepath.Join(sourceDir, environmentStateFile))
	if err != nil {
		return nil, errors.New("this session predates reuse support; reconnect it once normally")
	}
	var environment map[string]string
	if err := json.Unmarshal(environmentData, &environment); err != nil {
		return nil, fmt.Errorf("read reusable environment: %w", err)
	}

	cacheDir, err := sessionCacheDir()
	if err != nil {
		return nil, err
	}
	id, err := sessionID()
	if err != nil {
		return nil, err
	}
	if root == "" {
		root = filepath.Join(cacheDir, "contexthop", "sessions")
	}
	directory := filepath.Join(root, id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	prepared := &Session{Directory: directory}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(directory)
		}
	}()
	if err := copySessionFiles(sourceDir, directory); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(directory, transactionStateFile), []byte("staging\n"), 0o600); err != nil {
		return nil, err
	}

	rewritePath := func(value string) string {
		if value == sourceDir {
			return directory
		}
		if strings.HasPrefix(value, sourceDir+string(os.PathSeparator)) {
			return directory + strings.TrimPrefix(value, sourceDir)
		}
		return value
	}
	for key, value := range environment {
		environment[key] = rewritePath(value)
	}
	environment["CONTEXTHOP_SESSION_ID"] = id
	environment["CONTEXTHOP_SESSION_DIR"] = directory
	environment[state.SessionFileEnv] = filepath.Join(directory, "session.json")

	manifest := sourceManifest
	manifest.Previous = nil
	manifest.SessionID = id
	manifest.Expected.ADC = rewritePath(manifest.Expected.ADC)
	manifest.CloudSDKConfig = rewritePath(manifest.CloudSDKConfig)
	environment["CONTEXTHOP_PROMPT_PREFIX"] = promptPrefix(manifest)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	prepared.Manifest = filepath.Join(directory, "session.json")
	if err := os.WriteFile(prepared.Manifest, manifestData, 0o600); err != nil {
		return nil, err
	}
	updatedEnvironment, err := json.MarshalIndent(managedEnvironment(environment), "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(directory, environmentStateFile), updatedEnvironment, 0o600); err != nil {
		return nil, err
	}
	if kubeconfig := environment["KUBECONFIG"]; kubeconfig != "" && manifest.KubernetesControlPlaneFingerprint != "" {
		fingerprint, fingerprintErr := kubetarget.FingerprintFile(kubeconfig)
		if fingerprintErr != nil || fingerprint.Digest != manifest.KubernetesControlPlaneFingerprint {
			return nil, errors.New("the reusable Kubernetes configuration changed; reconnect normally")
		}
	}
	prepared.Env = mergeManagedEnvironment(os.Environ(), environment)
	failed = false
	return prepared, nil
}

func mergeManagedEnvironment(base []string, managed map[string]string) []string {
	environment := environmentMap(base)
	for _, key := range managedEnvironmentKeys() {
		delete(environment, key)
	}
	for key, value := range managed {
		environment[key] = value
	}
	return flattenEnvironment(environment)
}

func copySessionFiles(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		if relative == "zsh" || strings.HasPrefix(relative, "zsh"+string(os.PathSeparator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to reuse symlink %s", relative)
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func validateSessionDirectory(directory, sessionID string) error {
	cacheDir, err := sessionCacheDir()
	if err != nil {
		return err
	}
	root := filepath.Join(cacheDir, "contexthop", "sessions")
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative != sessionID || strings.Contains(relative, string(os.PathSeparator)) {
		return errors.New("refusing to reuse a session outside the ContextHop cache")
	}
	return nil
}

func activeAlive(record Active) bool {
	if record.ShellPID <= 0 || record.Manifest == "" {
		return false
	}
	if err := syscall.Kill(record.ShellPID, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	if record.ShellStart != "" {
		started, err := processStart(record.ShellPID)
		if err != nil || started != record.ShellStart {
			return false
		}
	}
	manifest, err := state.LoadManifest(record.Manifest)
	if err != nil || manifest.Destination != record.Destination {
		return false
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(record.Manifest), environmentStateFile)); err != nil {
		return false
	}
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(record.Manifest), transactionStateFile))
	return err == nil && strings.TrimSpace(string(contents)) == "committed"
}

func processStart(pid int) (string, error) {
	output, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	value := strings.Join(strings.Fields(string(output)), " ")
	if value == "" {
		return "", errors.New("process has no start time")
	}
	return value, nil
}

func activeRoot() (string, error) {
	cacheDir, err := sessionCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "contexthop", "active-sessions"), nil
}

func activeKey(pid int, started string) string {
	return strconv.Itoa(pid) + ":" + started
}

func readActive(path string) (Active, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Active{}, err
	}
	var record Active
	if err := json.Unmarshal(data, &record); err != nil {
		return Active{}, err
	}
	return record, nil
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".active-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func removeActiveManifest(manifest string) error {
	root, err := activeRoot()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if record, readErr := readActive(path); readErr == nil && record.Manifest == manifest {
			_ = os.Remove(path)
		}
	}
	return nil
}
