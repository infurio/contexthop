package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/infurio/contexthop/internal/config"
)

func TestBackupAndRestoreConfigurationAndMetadata(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config", "config.yaml")
	cacheBase := filepath.Join(root, "cache")
	t.Setenv("CONTEXTHOP_CACHE_DIR", cacheBase)

	original := config.New()
	original.Identities["work"] = config.Identity{Provider: "gcp", Account: "work@example.com"}
	if err := config.Write(configPath, original); err != nil {
		t.Fatal(err)
	}
	metadataRoot := filepath.Join(cacheBase, "contexthop")
	if err := os.MkdirAll(filepath.Join(metadataRoot, "sessions", "transient"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"catalog.json":           "catalog-before",
		"recency.json":           "recency-before",
		"gke-verifications.json": "validation-before",
	} {
		if err := os.WriteFile(filepath.Join(metadataRoot, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	backup, err := Create(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backup.Directory, "sessions")); !os.IsNotExist(err) {
		t.Fatalf("transient sessions were included in backup: %v", err)
	}

	changed := config.New()
	changed.Identities["other"] = config.Identity{Provider: "gcp", Account: "other@example.com"}
	if err := config.Write(configPath, changed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadataRoot, "catalog.json"), []byte("catalog-after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Restore(configPath, backup); err != nil {
		t.Fatal(err)
	}

	restored, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.Identities["work"]; !ok || len(restored.Identities) != 1 {
		t.Fatalf("restored identities = %#v", restored.Identities)
	}
	data, err := os.ReadFile(filepath.Join(metadataRoot, "catalog.json"))
	if err != nil || string(data) != "catalog-before" {
		t.Fatalf("restored catalog = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(metadataRoot, "sessions")); !os.IsNotExist(err) {
		t.Fatalf("transient sessions survived restore: %v", err)
	}
}

func TestResolveLatestAndID(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	t.Setenv("CONTEXTHOP_CACHE_DIR", filepath.Join(root, "cache"))
	if err := config.Write(configPath, config.New()); err != nil {
		t.Fatal(err)
	}
	backup, err := Create(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{"", "latest", backup.ID, backup.Directory} {
		resolved, err := Resolve(configPath, reference)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", reference, err)
		}
		if resolved.ID != backup.ID {
			t.Fatalf("Resolve(%q) = %q, want %q", reference, resolved.ID, backup.ID)
		}
	}
}

func TestClearMetadataPreservesCredentials(t *testing.T) {
	root := t.TempDir()
	cacheBase := filepath.Join(root, "cache")
	home := filepath.Join(root, "home")
	t.Setenv("CONTEXTHOP_CACHE_DIR", cacheBase)
	t.Setenv("HOME", home)
	metadata := filepath.Join(cacheBase, "contexthop", "catalog.json")
	credential := filepath.Join(home, ".config", "contexthop", "gcloud", "work", "credentials.db")
	for _, path := range []string{metadata, credential} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ClearMetadata(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(metadata); !os.IsNotExist(err) {
		t.Fatalf("metadata was not cleared: %v", err)
	}
	if _, err := os.Stat(credential); err != nil {
		t.Fatalf("credentials changed during metadata reset: %v", err)
	}
	if err := ClearCredentials(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(credential); !os.IsNotExist(err) {
		t.Fatalf("credentials were not cleared: %v", err)
	}
}
