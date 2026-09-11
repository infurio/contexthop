package main

import (
	"os"
	"path/filepath"

	"github.com/infurio/contexthop/internal/config"
	"github.com/infurio/contexthop/internal/resolver"
	"gopkg.in/yaml.v3"
)

func previewKubernetesContext(target config.Kubernetes, name, projectID string) string {
	if target.Kubeconfig != "" && target.Context != "" {
		return target.Context
	}
	if target.Type == "gke" && projectID != "" && target.Location != "" && target.Cluster != "" {
		return "gke_" + projectID + "_" + target.Location + "_" + target.Cluster
	}
	return firstNonEmpty(target.Context, target.Cluster, name)
}

// Previews only read local source files. Never invoke kubectl, authenticate, or
// read the active session's mutable namespace as a substitute for the source.
func previewKubernetesNamespace(target config.Kubernetes) string {
	if target.Namespace != "" {
		return target.Namespace
	}
	if target.Kubeconfig == "" || target.Context == "" {
		return "default"
	}
	namespace := ""
	for _, path := range filepath.SplitList(target.Kubeconfig) {
		if path == "" {
			continue
		}
		expanded, err := resolver.ExpandPath(path)
		if err != nil {
			return "source default"
		}
		data, err := os.ReadFile(expanded)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "source default"
		}
		var document struct {
			Contexts []struct {
				Name    string `yaml:"name"`
				Context struct {
					Namespace string `yaml:"namespace"`
				} `yaml:"context"`
			} `yaml:"contexts"`
		}
		if yaml.Unmarshal(data, &document) != nil {
			return "source default"
		}
		// Kubernetes merging keeps the first definition of each context, including
		// an omitted namespace (which means default, not the next file's namespace).
		for _, entry := range document.Contexts {
			if entry.Name == target.Context && namespace == "" {
				namespace = firstNonEmpty(entry.Context.Namespace, "default")
			}
		}
	}
	return firstNonEmpty(namespace, "source default")
}
