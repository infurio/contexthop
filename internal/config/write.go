package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

func Write(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temporary configuration: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}

	if _, err := os.Stat(path); err == nil {
		backup := fmt.Sprintf("%s.backup-%s", path, time.Now().Format("20060102-150405.000000000"))
		original, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read configuration for backup: %w", err)
		}
		if err := os.WriteFile(backup, original, 0o600); err != nil {
			return fmt.Errorf("back up configuration: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect configuration: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	return nil
}
