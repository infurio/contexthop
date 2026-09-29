package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/selection"
	"github.com/infurio/contexthop/internal/testenv"
)

func TestParseLaunchArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		exec bool
		want launchArguments
	}{
		{"workspace", []string{"--no-login", "Payments Dev"}, false, launchArguments{request: selection.Request{Workspace: "Payments Dev"}, noLogin: true}},
		{"literal shell workspace", []string{"--no-login", "--", "--Acme workspace"}, false, launchArguments{request: selection.Request{Workspace: "--Acme workspace"}, noLogin: true}},
		{"explicit exec workspace", []string{"--workspace=--Acme workspace", "--", "true"}, true, launchArguments{request: selection.Request{Workspace: "--Acme workspace"}, command: []string{"true"}}},
		{"components", []string{"--identity", "Acme Engineering", "--project=acme-development", "--kubernetes", "payments-dev", "--docker", "Local Docker", "--no-login"}, false, launchArguments{request: selection.Request{Identity: "Acme Engineering", Project: "acme-development", Kubernetes: "payments-dev", Docker: "Local Docker"}, noLogin: true}},
		{"exec preserves child arguments", []string{"--docker", "Local Docker", "--", "sh", "-c", "printf 'one\\ntwo\\n'", "--identity", "child-value"}, true, launchArguments{request: selection.Request{Docker: "Local Docker"}, command: []string{"sh", "-c", "printf 'one\\ntwo\\n'", "--identity", "child-value"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLaunchArguments(tt.args, tt.exec)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parse = %#v, %v; want %#v", got, err, tt.want)
			}
		})
	}
}

func TestParseLaunchArgumentsRejectsAmbiguousOrIncompleteInput(t *testing.T) {
	for _, args := range [][]string{
		nil, {"--no-login"}, {"Payments Dev", "--identity", "Acme Engineering"},
		{"Payments Dev", "Local containers"}, {"--identity"}, {"--identity", "--docker", "Local Docker"},
		{"--identity="}, {"--identity", "Acme Engineering", "--identity", "Personal"},
		{"--unknown", "value"}, {"--no-login=true", "Payments Dev"},
		{"Payments Dev", "--workspace", "Local containers"}, {"--workspace", "Payments Dev", "Local containers"},
		{"--workspace", "Payments Dev", "--docker", "Local Docker"}, {"--"}, {"--", "Payments Dev", "Local containers"},
	} {
		if _, err := parseLaunchArguments(args, false); err == nil {
			t.Errorf("accepted shell arguments %q", args)
		}
	}
	for _, args := range [][]string{{"Payments Dev"}, {"Payments Dev", "--"}, {"--", "true"}} {
		if _, err := parseLaunchArguments(args, true); err == nil {
			t.Errorf("accepted exec arguments %q", args)
		}
	}
}

func TestLaunchComponentsUseSharedDependencyPolicy(t *testing.T) {
	env := testenv.New(t, testenv.Options{Scenario: "acme"})
	cfg, err := config.Load(env.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	// Ambient selections must not add unrelated components.
	t.Setenv("CLOUDSDK_CORE_PROJECT", "ambient-project")
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "ambient@acme.example")
	t.Setenv("DOCKER_CONTEXT", "ambient-docker")
	for _, request := range []selection.Request{
		{Identity: "Acme Engineering"},
		{Project: "acme-development"},
		{Kubernetes: "payments-dev"},
		{Docker: "Local Docker"},
		{Identity: "Acme Engineering", Project: "acme-development", Kubernetes: "payments-dev", Docker: "Local Docker"},
	} {
		got, err := resolveLaunchComponents(cfg, request)
		if err != nil {
			t.Fatal(err)
		}
		want, err := request.Resolve(cfg)
		if err != nil {
			t.Fatal(err)
		}
		want.Name = got.Name
		if !reflect.DeepEqual(got, want) || got.Name == "" || got.WorkspaceName != "" {
			t.Fatalf("component resolution differs from shared policy: %#v, %#v", got, want)
		}
	}
	for _, tt := range []struct {
		request selection.Request
		message string
	}{
		{selection.Request{Project: "acme-staging", Kubernetes: "payments-dev"}, "does not match"},
		{selection.Request{Identity: "Personal", Project: "acme-development"}, "not mapped"},
		{selection.Request{Identity: "Unknown Acme Identity"}, "unknown identity"},
		{selection.Request{Docker: "Unknown Acme Docker"}, "unknown docker"},
	} {
		if _, err := resolveLaunchComponents(cfg, tt.request); err == nil || !strings.Contains(err.Error(), tt.message) {
			t.Fatalf("resolution error = %v; want %q", err, tt.message)
		}
	}
	project := cfg.Projects["acme-development"]
	project.Identities = []string{"Acme Engineering", "Personal"}
	cfg.Projects["acme-development"] = project
	if _, err := resolveLaunchComponents(cfg, selection.Request{Project: "acme-development"}); err == nil || !strings.Contains(err.Error(), "multiple mapped identities") {
		t.Fatalf("ambiguous identity should require an explicit flag: %v", err)
	}
}
