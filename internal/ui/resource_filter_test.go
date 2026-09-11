package ui

import "testing"

func TestResourceFiltersSearchOnlyDisplayedIdentifiers(t *testing.T) {
	for _, test := range []struct {
		dimension, query string
		match, unrelated Option
	}{
		{"project", "kubernetes", Option{Name: "alias", Project: true, ProjectID: "team-a-kubernetes"}, Option{Name: "kubernetes-alias", Project: true, ProjectID: "team-a-network", Summary: "Mappings: kubernetes:cluster", KubernetesContext: "No Kubernetes"}},
		{"identity", "team-b", Option{Name: "work", Identity: true, IdentityAccount: "developer@team-b.example.com"}, Option{Name: "team-b-alias", Identity: true, IdentityAccount: "developer@team-a.example.com", Summary: "Mappings: project:team-b"}},
		{"kubernetes", "production", Option{Name: "alias", Kubernetes: true, KubernetesCluster: "production-cluster"}, Option{Name: "alias", Kubernetes: true, KubernetesCluster: "development", ProjectID: "production", Summary: "production mapping"}},
		{"kubernetes", "friendly", Option{Name: "alias", Kubernetes: true, KubernetesCluster: "cluster", KubernetesContext: "friendly-context"}, Option{Name: "other", Kubernetes: true, KubernetesCluster: "other", IdentityAccount: "friendly@example.com"}},
		{"docker", "desktop", Option{Name: "alias", Docker: true, DockerContext: "docker-desktop"}, Option{Name: "desktop-alias", Docker: true, DockerContext: "colima", Summary: "Mappings: workspace:desktop"}},
		{"workspace", "payments", Option{Name: "alias", Label: "Payments development"}, Option{Name: "payments-alias", Label: "Analytics", ProjectID: "payments", Summary: "payments mapping"}},
	} {
		t.Run(test.dimension+"/"+test.query, func(t *testing.T) {
			picker := Picker{ResourceBrowser: true, Dimension: test.dimension, Options: []Option{test.match, test.unrelated}}
			got := filteredPickerOptions(picker, test.query)
			if len(got) != 1 || got[0].Name != test.match.Name {
				t.Fatalf("unexpected matches: %#v", got)
			}
		})
	}
}

func TestResourceFilterIsCaseInsensitiveSubstringAndRespectsHidden(t *testing.T) {
	picker := Picker{ResourceBrowser: true, Dimension: "project", Options: []Option{
		{Name: "visible", Project: true, ProjectID: "team-a-Kubernetes"},
		{Name: "hidden", Project: true, ProjectID: "hidden-kubernetes", Hidden: true},
		{Name: "scattered", Project: true, ProjectID: "k-u-b-e-r-n-e-t-e-s"},
	}}
	if got := filteredPickerOptions(picker, " KUBERNETES "); len(got) != 1 || got[0].Name != "visible" {
		t.Fatalf("unexpected matches: %#v", got)
	}
	picker.ShowHidden = true
	if got := filteredPickerOptions(picker, "kubernetes"); len(got) != 2 {
		t.Fatalf("hidden matches: %#v", got)
	}
	if got := filteredPickerOptions(picker, "missing"); len(got) != 0 {
		t.Fatalf("unexpected matches: %#v", got)
	}
}
