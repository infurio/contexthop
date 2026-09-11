package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	cloudconfig "github.com/infurio/contexthop/internal/config"
)

// Observations are read-only facts reported by external configuration and
// provider tools. They deliberately do not contain ContextHop workspaces or user
// preferences; those belong to the reconciled catalog.
type Observations struct {
	Identities         []IdentityObservation
	GCloudConfigs      []GCloudConfigurationObservation
	KubernetesContexts []KubernetesContextObservation
	DockerContexts     []DockerContextObservation
	Warnings           []string
}

type IdentityObservation struct {
	Provider   string
	Kind       string
	Account    string
	Source     string
	ObservedAt string
}

type GCloudConfigurationObservation struct {
	Name       string
	Account    string
	ProjectID  string
	ObservedAt string
}

type KubernetesContextObservation struct {
	Source           string
	Context          string
	ClusterReference string
	UserReference    string
	Namespace        string
	AccessSignature  string
	ClusterSignature string
	GKEProjectID     string
	GKELocation      string
	GKECluster       string
	ObservedAt       string
}

type DockerContextObservation struct {
	Context    string
	ObservedAt string
}

// ObserveLocal scans host configuration without modifying it. Remote project
// and cluster enumeration remains an explicit, identity-scoped operation.
func ObserveLocal(ctx context.Context) (Observations, error) {
	if err := RequireUnmanagedShell(); err != nil {
		return Observations{}, err
	}
	observedAt := time.Now().UTC().Format(time.RFC3339)
	observations := Observations{}

	if output, err := run(ctx, "gcloud", "auth", "list", "--format=json", "--quiet"); err == nil {
		var identities []gcloudIdentity
		if err := json.Unmarshal(output, &identities); err != nil {
			observations.Warnings = append(observations.Warnings, "could not decode gcloud identities")
		} else {
			for _, discovered := range identities {
				if discovered.Account == "" {
					continue
				}
				kind := "user"
				if isServiceAccount(discovered.Account) {
					kind = "service-account"
				}
				observations.Identities = append(observations.Identities, IdentityObservation{
					Provider: "gcp", Kind: kind, Account: discovered.Account,
					Source: "gcloud-auth", ObservedAt: observedAt,
				})
			}
		}
	} else if !errors.Is(err, exec.ErrNotFound) {
		observations.Warnings = append(observations.Warnings, "gcloud identity discovery failed")
	}

	if output, err := run(ctx, "gcloud", "config", "configurations", "list", "--format=json", "--quiet"); err == nil {
		var configurations []gcloudConfiguration
		if err := json.Unmarshal(output, &configurations); err != nil {
			observations.Warnings = append(observations.Warnings, "could not decode gcloud configurations")
		} else {
			for _, discovered := range configurations {
				account := strings.TrimSpace(discovered.Properties.Core.Account)
				if account != "" {
					kind := "user"
					if isServiceAccount(account) {
						kind = "service-account"
					}
					observations.Identities = append(observations.Identities, IdentityObservation{
						Provider: "gcp", Kind: kind, Account: account,
						Source: "gcloud-configuration:" + discovered.Name, ObservedAt: observedAt,
					})
				}
				if account != "" || discovered.Properties.Core.Project != "" {
					observations.GCloudConfigs = append(observations.GCloudConfigs, GCloudConfigurationObservation{
						Name: discovered.Name, Account: account,
						ProjectID: discovered.Properties.Core.Project, ObservedAt: observedAt,
					})
				}
			}
		}
	} else if !errors.Is(err, exec.ErrNotFound) {
		observations.Warnings = append(observations.Warnings, "gcloud configuration discovery failed")
	}

	sources, warnings := discoverKubernetesSources(ctx, currentKubeconfigPath())
	observations.Warnings = append(observations.Warnings, warnings...)
	observations.KubernetesContexts = observeKubernetesContexts(sources, observedAt)

	if output, err := run(ctx, "docker", "context", "ls", "--format", "{{json .}}"); err == nil {
		for _, line := range nonEmptyLines(output) {
			var discovered dockerContext
			if json.Unmarshal([]byte(line), &discovered) != nil || discovered.Name == "" {
				observations.Warnings = append(observations.Warnings, "could not decode a Docker context")
				continue
			}
			observations.DockerContexts = append(observations.DockerContexts, DockerContextObservation{Context: discovered.Name, ObservedAt: observedAt})
		}
	} else if !errors.Is(err, exec.ErrNotFound) {
		observations.Warnings = append(observations.Warnings, "Docker context discovery failed")
	}

	sortObservations(&observations)
	return observations, nil
}

func observeKubernetesContexts(sources []kubeSourceView, observedAt string) []KubernetesContextObservation {
	var observations []KubernetesContextObservation
	for _, source := range sources {
		for _, contextItem := range source.View.Contexts {
			projectID, location, cluster, isGKE := parseGKEContext(contextItem.Context.Cluster)
			if !isGKE {
				projectID, location, cluster, isGKE = parseGKEContext(contextItem.Name)
			}
			observation := KubernetesContextObservation{
				Source: source.Path, Context: contextItem.Name,
				ClusterReference: contextItem.Context.Cluster, UserReference: contextItem.Context.User,
				Namespace:        contextItem.Context.Namespace,
				AccessSignature:  kubeContextSignature(source.View, contextItem.Name),
				ClusterSignature: kubeClusterSignature(source.View, contextItem.Context.Cluster),
				ObservedAt:       observedAt,
			}
			if isGKE {
				observation.GKEProjectID, observation.GKELocation, observation.GKECluster = projectID, location, cluster
			}
			observations = append(observations, observation)
		}
	}
	return observations
}

func kubeClusterSignature(view kubeConfigView, clusterName string) string {
	for _, cluster := range view.Clusters {
		if cluster.Name == clusterName {
			return string(cluster.Cluster)
		}
	}
	return clusterName
}

// Normalize converts observations into canonical resources and access-profile
// aliases. Exact access definitions are merged; matching names alone never are.
func Normalize(observations Observations) (Result, error) {
	result := Result{Config: cloudconfig.New(), Warnings: append([]string(nil), observations.Warnings...)}
	identityByKey := map[string]string{}
	for _, observed := range observations.Identities {
		if observed.Provider == "" || observed.Account == "" {
			continue
		}
		key := observed.Provider + "\x00" + observed.Kind + "\x00" + strings.ToLower(observed.Account)
		if _, exists := identityByKey[key]; exists {
			continue
		}
		name := uniqueName(identityName(observed.Account), result.Config.Identities)
		identityByKey[key] = name
		result.Config.Identities[name] = cloudconfig.Identity{
			Provider: observed.Provider, Kind: observed.Kind, Account: observed.Account,
			CloudSDKConfig: "~/.config/contexthop/gcloud/" + name,
			ADC:            "~/.config/contexthop/gcloud/" + name + "/application_default_credentials.json",
			Provenance:     "gcp", VerifiedBy: "gcp", ObservedAt: observed.ObservedAt,
		}
	}

	projectNameByID := map[string]string{}
	for _, observed := range observations.GCloudConfigs {
		if observed.ProjectID == "" {
			continue
		}
		name := ensureProject(&result.Config, projectNameByID, observed.ProjectID)
		project := result.Config.Projects[name]
		project.Provenance, project.VerifiedBy, project.ObservedAt = "gcp", "gcp", observed.ObservedAt
		identityKeyPrefix := "gcp\x00"
		for key, identityName := range identityByKey {
			if strings.HasPrefix(key, identityKeyPrefix) && strings.HasSuffix(key, "\x00"+strings.ToLower(observed.Account)) && !contains(project.Identities, identityName) {
				project.Identities = append(project.Identities, identityName)
			}
		}
		sort.Strings(project.Identities)
		result.Config.Projects[name] = project
	}

	normalizeKubernetes(&result, observations.KubernetesContexts, projectNameByID)
	for _, observed := range observations.DockerContexts {
		if observed.Context == "" {
			continue
		}
		name := uniqueName(slug(observed.Context), result.Config.Docker)
		result.Config.Docker[name] = cloudconfig.Docker{
			Context:    observed.Context,
			Provenance: "docker", VerifiedBy: "docker", ObservedAt: observed.ObservedAt,
		}
	}

	return result, result.Config.Validate()
}

func normalizeKubernetes(result *Result, observations []KubernetesContextObservation, projectNames map[string]string) {
	byAccess := map[string][]KubernetesContextObservation{}
	byContext := map[string]map[string][]string{}
	for _, observed := range observations {
		access := observed.AccessSignature
		if access == "" {
			access = observed.Source + "\x00" + observed.Context
		}
		byAccess[access] = append(byAccess[access], observed)
		if byContext[observed.Context] == nil {
			byContext[observed.Context] = map[string][]string{}
		}
		byContext[observed.Context][access] = append(byContext[observed.Context][access], observed.Source)
	}
	for contextName, definitions := range byContext {
		if len(definitions) < 2 {
			continue
		}
		var paths []string
		for _, sources := range definitions {
			paths = append(paths, sources...)
		}
		sort.Strings(paths)
		result.Warnings = append(result.Warnings, fmt.Sprintf("Kubernetes context %q has conflicting definitions in %s; retained as separate access profiles", contextName, strings.Join(paths, ", ")))
	}

	accessKeys := make([]string, 0, len(byAccess))
	for key := range byAccess {
		accessKeys = append(accessKeys, key)
	}
	sort.Strings(accessKeys)
	for _, accessKey := range accessKeys {
		group := byAccess[accessKey]
		sort.Slice(group, func(i, j int) bool { return preferContextObservation(group[i], group[j]) })
		preferred := group[0]
		nameBase := preferred.Context
		if len(byContext[preferred.Context]) > 1 {
			nameBase += "-" + filepath.Base(preferred.Source)
		}
		name := uniqueName(slug(nameBase), result.Config.Kubernetes)
		target := cloudconfig.Kubernetes{
			Type: "kubeconfig", Cluster: preferred.ClusterReference,
			AccessID: shortHash(accessKey), Kubeconfig: preferred.Source,
			Context: preferred.Context, Namespace: preferred.Namespace,
			Provenance: "kubeconfig",
			VerifiedBy: "kubeconfig", ObservedAt: preferred.ObservedAt,
		}
		for _, observed := range group {
			if observed.GKEProjectID != "" {
				projectName := ensureProject(&result.Config, projectNames, observed.GKEProjectID)
				project := result.Config.Projects[projectName]
				if project.Provenance == "" {
					project.Provenance, project.VerifiedBy, project.ObservedAt = "kubeconfig", "kubeconfig", observed.ObservedAt
					result.Config.Projects[projectName] = project
				}
				target.Type, target.Project = "gke", projectName
				target.Cluster, target.Location = observed.GKECluster, observed.GKELocation
				target.ProviderID = "gke:" + observed.GKEProjectID + "/" + observed.GKELocation + "/" + observed.GKECluster
				break
			}
		}
		if target.ProviderID == "" {
			target.ProviderID = "kube:" + shortHash(preferred.ClusterSignature)
		}
		result.Config.Kubernetes[name] = target
		aliases := make([]cloudconfig.KubernetesAlias, 0, len(group))
		seen := map[string]bool{}
		for _, observed := range group {
			key := observed.Source + "\x00" + observed.Context
			if seen[key] {
				continue
			}
			seen[key] = true
			aliases = append(aliases, cloudconfig.KubernetesAlias{
				Context: observed.Context, Kubeconfig: observed.Source,
				Namespace: observed.Namespace, Provenance: "kubeconfig", ObservedAt: observed.ObservedAt,
			})
		}
		sort.Slice(aliases, func(i, j int) bool {
			if aliases[i].Context != aliases[j].Context {
				return aliases[i].Context < aliases[j].Context
			}
			return aliases[i].Kubeconfig < aliases[j].Kubeconfig
		})
		result.Config.KubernetesAliases[name] = aliases
	}
	sort.Strings(result.Warnings)
}

func preferContextObservation(left, right KubernetesContextObservation) bool {
	leftGenerated := left.GKEProjectID != "" && left.Context == "gke_"+left.GKEProjectID+"_"+left.GKELocation+"_"+left.GKECluster
	rightGenerated := right.GKEProjectID != "" && right.Context == "gke_"+right.GKEProjectID+"_"+right.GKELocation+"_"+right.GKECluster
	if leftGenerated != rightGenerated {
		return !leftGenerated
	}
	if len(left.Context) != len(right.Context) {
		return len(left.Context) < len(right.Context)
	}
	if left.Context != right.Context {
		return left.Context < right.Context
	}
	return left.Source < right.Source
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum[:12])
}

func sortObservations(observations *Observations) {
	sort.Slice(observations.Identities, func(i, j int) bool {
		left, right := observations.Identities[i], observations.Identities[j]
		return left.Provider+"\x00"+left.Kind+"\x00"+left.Account+"\x00"+left.Source < right.Provider+"\x00"+right.Kind+"\x00"+right.Account+"\x00"+right.Source
	})
	sort.Slice(observations.GCloudConfigs, func(i, j int) bool { return observations.GCloudConfigs[i].Name < observations.GCloudConfigs[j].Name })
	sort.Slice(observations.KubernetesContexts, func(i, j int) bool {
		left, right := observations.KubernetesContexts[i], observations.KubernetesContexts[j]
		return left.Context+"\x00"+left.Source < right.Context+"\x00"+right.Source
	})
	sort.Slice(observations.DockerContexts, func(i, j int) bool {
		return observations.DockerContexts[i].Context < observations.DockerContexts[j].Context
	})
}
