package selection

type Resource string

const (
	Identity   Resource = "identity"
	Project    Resource = "project"
	Kubernetes Resource = "kubernetes"
	Docker     Resource = "docker"
	Workspace  Resource = "workspace"
)

func (r Request) Resource(kind Resource) string {
	switch kind {
	case Identity:
		return r.Identity
	case Project:
		return r.Project
	case Kubernetes:
		return r.Kubernetes
	case Docker:
		return r.Docker
	case Workspace:
		return r.Workspace
	}
	return ""
}

func (r *Request) ClearDependents(kind Resource) {
	switch kind {
	case Workspace:
		r.Identity, r.Project, r.Kubernetes, r.Docker, r.Workspace, r.Source = "", "", "", "", "", ""
	case Identity:
		r.Project, r.Kubernetes = "", ""
	case Project:
		r.Kubernetes = ""
	}
}

func (r *Request) clearResource(kind Resource) {
	switch kind {
	case Identity:
		r.Identity = ""
	case Project:
		r.Project = ""
	case Kubernetes:
		r.Kubernetes = ""
	case Docker:
		r.Docker = ""
	case Workspace:
		r.Workspace = ""
	}
	r.ClearDependents(kind)
}

// Unstage is an explicit Space removal. A modified preset is no longer staged.
func (r *Request) Unstage(kind Resource) {
	r.clearResource(kind)
	r.Workspace, r.Source, r.ADCOverride = "", "", ADCDefault
}

// UnstageNext preserves workspace provenance until the last component is
// removed, so Escape can progressively unwind a prepared combination.
func (r *Request) UnstageNext(includeLocal bool) bool {
	kinds := []Resource{Kubernetes, Project, Identity}
	if includeLocal {
		kinds = append(kinds, Docker, Workspace)
	}
	for _, kind := range kinds {
		if r.Resource(kind) == "" {
			continue
		}
		if includeLocal {
			r.Workspace, r.ADCOverride = "", ADCDefault
		}
		r.clearResource(kind)
		if !r.HasResources() {
			r.Source = ""
		}
		return true
	}
	return false
}
