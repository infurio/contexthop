package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/infurio/contexthop/internal/testenv"
)

func TestSkillInstallation(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		t.Run(agent, func(t *testing.T) {
			env := testenv.New(t, testenv.Options{})
			path := filepath.Join(env.Home, "."+agent, "skills", "chop", "SKILL.md")
			for range 2 {
				if err := runSkill([]string{"install", agent}); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != agentSkill {
				t.Fatalf("installed skill differs: %v", err)
			}
			if err := os.WriteFile(path, []byte("user-owned skill"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := runSkill([]string{"install", agent}); err == nil {
				t.Fatal("overwrote a user-owned skill")
			}
			data, _ = os.ReadFile(path)
			if string(data) != "user-owned skill" {
				t.Fatal("existing skill changed")
			}
		})
	}
}

func TestSkillInstallationUsesCodexHome(t *testing.T) {
	env := testenv.New(t, testenv.Options{})
	root := filepath.Join(env.Root, "custom-codex")
	t.Setenv("CODEX_HOME", root)
	if err := runSkill([]string{"install", "codex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "chop", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := runSkill([]string{"install", "unknown"}); err == nil {
		t.Fatal("accepted unknown agent")
	}
}
