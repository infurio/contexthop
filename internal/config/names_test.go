package config

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestHumanReadableNamesRoundTrip(t *testing.T) {
	cfg := New()
	identity, project, kubernetes, docker, workspace := "Developer's Work Account", "2026 / Payments: Dev", "開発クラスタ 🚀", "Docker_Local (Mac)", "__Payments Dev: café / team"
	cfg.Identities[identity] = Identity{Provider: "gcp", Account: "user@example.com"}
	cfg.Projects[project] = Project{Provider: "gcp", ProjectID: "payments-dev", Identities: []string{identity}}
	cfg.Kubernetes[kubernetes] = Kubernetes{Type: "gke", Project: project, Cluster: "cluster", Location: "us-east1"}
	cfg.Docker[docker] = Docker{Context: "desktop-linux"}
	cfg.Destinations[workspace] = Destination{Identity: identity, Project: project, Kubernetes: kubernetes, Docker: docker}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := Encode(&data, cfg); err != nil {
		t.Fatal(err)
	}
	restored, err := Decode(&data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, cfg) {
		t.Fatalf("names or references changed: %#v", restored)
	}
}

func TestNamesRejectEmptyAndControlCharacters(t *testing.T) {
	for _, name := range []string{"", "   ", "a\nb", "a\tb", "a\x00b", "a\x1bb", "a\u2028b", string([]byte{0xff})} {
		if ValidateName(name) == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, name := range []string{"Payments Dev", "2026", "a_b.c", "開発 🚀", "__create_workspace__", "Developer's $(work)", "a/b"} {
		if err := ValidateName(name); err != nil {
			t.Errorf("rejected %q: %v", name, err)
		}
	}
	cfg := New()
	cfg.Docker[" "] = Docker{Context: "local"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("validation error: %v", err)
	}
}
