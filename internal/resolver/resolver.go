package resolver

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/infurio/contexthop/internal/config"
)

type Resolved struct {
	DisplayTags    map[string]string
	PromptColors   map[string]string
	Name           string
	WorkspaceName  string
	IdentityName   string
	Identity       *config.Identity
	ProjectName    string
	Project        *config.Project
	KubernetesName string
	Kubernetes     *config.Kubernetes
	DockerName     string
	Docker         *config.Docker
	ADCMode        string
	Risk           string // Legacy compatibility; resolution leaves this empty.
}

type Selection struct {
	Name       string
	Identity   string
	Project    string
	Kubernetes string
	Docker     string
	ADC        string
	Risk       string // Legacy compatibility; ignored by resolution.
}

func Destination(cfg config.Config, name string) (Resolved, error) {
	destination, ok := cfg.Destinations[name]
	if !ok {
		return Resolved{}, fmt.Errorf("unknown workspace %q", name)
	}
	// Saved workspaces retain their established dependency contract.
	cfg.Discovery = nil
	resolved, err := Components(cfg, Selection{
		Name:       name,
		Identity:   destination.Identity,
		Project:    destination.Project,
		Kubernetes: destination.Kubernetes,
		Docker:     destination.Docker,
		ADC:        destination.ADC,
	})
	if err == nil {
		if resolved.Project != nil && resolved.Identity != nil && !slices.Contains(resolved.Project.Identities, resolved.IdentityName) {
			return Resolved{}, fmt.Errorf("workspace %q identity %q is not mapped to project %q", name, resolved.IdentityName, resolved.ProjectName)
		}
		resolved.WorkspaceName = name
		resolved.PromptColors["workspace"] = firstTagColor(cfg, destination.LabelSet)
		addDisplayTags(&resolved, cfg, destination.LabelSet)
	}
	return resolved, err
}

// Components resolves a transient component selection without requiring it to
// be stored as a workspace. Empty fields are explicit absences.
func Components(cfg config.Config, selection Selection) (Resolved, error) {
	resolved := Resolved{Name: selection.Name, ADCMode: selection.ADC, PromptColors: map[string]string{}}

	if selection.Kubernetes != "" {
		target, ok := cfg.Kubernetes[selection.Kubernetes]
		if !ok {
			return Resolved{}, fmt.Errorf("unknown kubernetes target %q", selection.Kubernetes)
		}
		resolved.KubernetesName = selection.Kubernetes
		resolved.Kubernetes = &target
		if selection.Project == "" && target.Project != "" {
			selection.Project = target.Project
		} else if selection.Project != "" && target.Project != "" && selection.Project != target.Project {
			return Resolved{}, fmt.Errorf("project %q does not match kubernetes target project %q", selection.Project, target.Project)
		}
	}
	if selection.Project != "" {
		project, ok := cfg.Projects[selection.Project]
		if !ok {
			return Resolved{}, fmt.Errorf("unknown project %q", selection.Project)
		}
		resolved.ProjectName = selection.Project
		resolved.Project = &project
		if selection.Identity == "" {
			identity, candidates, err := SuggestedIdentity(cfg, selection.Project, selection.Kubernetes)
			if err != nil {
				return Resolved{}, err
			}
			if identity == "" {
				if len(candidates) == 0 {
					return Resolved{}, fmt.Errorf("context %q requires project %q, but that project has no mapped identity", selection.Name, selection.Project)
				}
				return Resolved{}, fmt.Errorf("context %q requires an explicit identity because project %q has multiple mapped identities", selection.Name, selection.Project)
			}
			selection.Identity = identity
		}
	}
	if selection.Identity != "" {
		identity, ok := cfg.Identities[selection.Identity]
		if !ok {
			return Resolved{}, fmt.Errorf("unknown identity %q", selection.Identity)
		}
		resolved.IdentityName = selection.Identity
		resolved.Identity = &identity
		if resolved.Project != nil {
			if (len(resolved.Project.Identities) > 0 || cfg.DiscoveryFor(selection.Identity).Projects != nil) && !ProjectIdentityAvailable(cfg, resolved.ProjectName, selection.Identity) {
				return Resolved{}, fmt.Errorf("identity %q is not mapped to project %q", selection.Identity, resolved.ProjectName)
			}
			if identity.Provider != resolved.Project.Provider {
				return Resolved{}, fmt.Errorf("identity %q provider %q is incompatible with project %q provider %q", selection.Identity, identity.Provider, resolved.ProjectName, resolved.Project.Provider)
			}
		}
	}
	if len(cfg.Discovery) > 0 && resolved.Kubernetes != nil && resolved.Identity != nil && resolved.Kubernetes.Project != "" && !KubernetesIdentityAvailable(cfg, resolved.KubernetesName, resolved.IdentityName) {
		return Resolved{}, fmt.Errorf("identity %q has no discovered access to Kubernetes target %q; refresh cluster discovery or choose another identity", resolved.IdentityName, resolved.KubernetesName)
	}
	if selection.Docker != "" {
		target, ok := cfg.Docker[selection.Docker]
		if !ok {
			return Resolved{}, fmt.Errorf("unknown docker target %q", selection.Docker)
		}
		resolved.DockerName = selection.Docker
		resolved.Docker = &target
	}
	if resolved.ADCMode == "identity" && resolved.Identity == nil {
		return Resolved{}, fmt.Errorf("context %q requires ADC but has no resolved identity", selection.Name)
	}
	if resolved.Kubernetes != nil {
		resolved.PromptColors["kubernetes"] = firstTagColor(cfg, resolved.Kubernetes.LabelSet)
		addDisplayTags(&resolved, cfg, resolved.Kubernetes.LabelSet)
	}
	if resolved.Docker != nil {
		resolved.PromptColors["docker"] = firstTagColor(cfg, resolved.Docker.LabelSet)
		addDisplayTags(&resolved, cfg, resolved.Docker.LabelSet)
	}
	if resolved.Project != nil {
		resolved.PromptColors["project"] = firstTagColor(cfg, resolved.Project.LabelSet)
		addDisplayTags(&resolved, cfg, resolved.Project.LabelSet)
	}
	if resolved.Identity != nil {
		resolved.PromptColors["identity"] = firstTagColor(cfg, resolved.Identity.LabelSet)
		addDisplayTags(&resolved, cfg, resolved.Identity.LabelSet)
	}
	return resolved, nil
}

func firstTagColor(cfg config.Config, labels config.LabelSet) string {
	names := labels.TagNames()
	if len(names) == 0 {
		return ""
	}
	color := cfg.Tags[names[0]].Color
	if !config.ValidTagColor(color) {
		return config.DefaultTagColor
	}
	return color
}

// SuggestedIdentity applies the shared resolution policy for project- and
// Kubernetes-led selection. An empty suggestion with multiple candidates means
// the caller must ask the user; recency is deliberately not consulted.
func SuggestedIdentity(cfg config.Config, projectName, kubernetesName string) (string, []string, error) {
	project, ok := cfg.Projects[projectName]
	if !ok {
		return "", nil, fmt.Errorf("unknown project %q", projectName)
	}
	candidates := EligibleIdentities(cfg, projectName, kubernetesName)
	if kubernetesName != "" {
		if target, exists := cfg.Kubernetes[kubernetesName]; exists && slices.Contains(candidates, target.PreferredIdentity) {
			return target.PreferredIdentity, candidates, nil
		}
	}
	if slices.Contains(candidates, project.PreferredIdentity) {
		return project.PreferredIdentity, candidates, nil
	}
	if len(candidates) == 1 {
		return candidates[0], candidates, nil
	}
	return "", candidates, nil
}

func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" || len(path) > 1 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		path = filepath.Join(home, path[2:])
	}
	return filepath.Clean(path), nil
}

func KubernetesTargetIdentity(target config.Kubernetes, project *config.Project) string {
	if target.Type == "gke" && target.Location != "" && target.Cluster != "" {
		projectID := target.Project
		if project != nil && project.ProjectID != "" {
			projectID = project.ProjectID
		}
		return "gke\x00" + projectID + "\x00" + target.Location + "\x00" + target.Cluster
	}
	return "kubeconfig\x00" + target.Kubeconfig + "\x00" + target.Context
}

func addDisplayTags(resolved *Resolved, cfg config.Config, labels config.LabelSet) {
	if resolved.DisplayTags == nil {
		resolved.DisplayTags = map[string]string{}
	}
	for _, name := range labels.TagNames() {
		color := cfg.Tags[name].Color
		if !config.ValidTagColor(color) {
			color = config.DefaultTagColor
		}
		resolved.DisplayTags[name] = color
	}
}
