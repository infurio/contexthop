package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"github.com/infurio/contexthop/internal/state"
)

func TestSharedDefaultSnapshotsAreDurableAndIndependent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	publish := func(name string) {
		t.Helper()
		prepared, err := Prepare(context.Background(), resolver.Resolved{Name: name, PromptColors: map[string]string{"docker": "#123456"}, DockerName: name, Docker: &config.Docker{Context: name}})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Close()
		if err := PublishShared(prepared); err != nil {
			t.Fatal(err)
		}
	}
	publish("dev")
	published, err := SharedConfig()
	if err != nil || published == nil || published.Destination != "dev" {
		t.Fatalf("shared preview: %v %v", published, err)
	}
	first, revision, err := FollowShared("")
	if err != nil || first == nil {
		t.Fatalf("follow: %v", err)
	}
	defer first.Close()
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	second, secondRevision, err := FollowShared("")
	if err != nil || second == nil || revision != secondRevision || first.Directory == second.Directory {
		t.Fatalf("followers are not independent: %v", err)
	}
	defer second.Close()
	manifest, err := state.LoadManifest(second.Manifest)
	if err != nil || manifest.PromptColors["docker"] != "#123456" {
		t.Fatalf("shared snapshot lost prompt colors: %v", err)
	}
	if env := environmentMap(second.Env); env["DOCKER_CONTEXT"] != "dev" {
		t.Fatal(env["DOCKER_CONTEXT"])
	}
	if unchanged, next, err := FollowShared(revision); err != nil || unchanged != nil || next != revision {
		t.Fatal("unchanged default recopied")
	}
	root, err := sharedRoot()
	if err != nil {
		t.Fatal(err)
	}
	old, err := readShared(root)
	if err != nil {
		t.Fatal(err)
	}
	publish("prod")
	if _, err := os.Stat(old.Manifest); !os.IsNotExist(err) {
		t.Fatal("superseded snapshot leaked")
	}
	next, nextRevision, err := FollowShared(revision)
	if err != nil || next == nil || nextRevision == revision {
		t.Fatalf("did not follow new revision: %v", err)
	}
	defer next.Close()
	if environmentMap(first.Env)["DOCKER_CONTEXT"] != "dev" {
		t.Fatal("old follower changed")
	}
	if err := ClearShared(); err != nil {
		t.Fatal(err)
	}
	if preview, err := SharedConfig(); err != nil || preview != nil {
		t.Fatal("cleared config still previewed", err)
	}
	if p, r, err := FollowShared(nextRevision); err != nil || p != nil || r != "" {
		t.Fatal("default not cleared")
	}
	if _, err := os.Stat(filepath.Join(first.Directory, "session.json")); err != nil {
		t.Fatal("clearing removed live follower")
	}
}

func TestFailedSharedPublicationPreservesDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CONTEXTHOP_CACHE_DIR", t.TempDir())
	first, err := Prepare(context.Background(), resolver.Resolved{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := PublishShared(first); err != nil {
		t.Fatal(err)
	}
	root, _ := sharedRoot()
	before, _ := os.ReadFile(filepath.Join(root, "default.json"))
	if err := PublishShared(&Session{Manifest: filepath.Join(t.TempDir(), "missing.json")}); err == nil {
		t.Fatal("invalid publication succeeded")
	}
	after, _ := os.ReadFile(filepath.Join(root, "default.json"))
	if string(before) != string(after) {
		t.Fatal("failed publication changed default")
	}
}
