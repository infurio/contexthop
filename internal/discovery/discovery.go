package discovery

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	cloudconfig "github.com/infurio/contexthop/internal/config"
)

type Result struct {
	Config   cloudconfig.Config
	Warnings []string
}

type gcloudIdentity struct {
	Account string `json:"account"`
}

type gcloudConfiguration struct {
	Name       string `json:"name"`
	IsActive   bool   `json:"is_active"`
	Properties struct {
		Core struct {
			Account string `json:"account"`
			Project string `json:"project"`
		} `json:"core"`
	} `json:"properties"`
}

type dockerContext struct {
	Name    string `json:"Name"`
	Current bool   `json:"Current"`
}

type kubeConfigView struct {
	Contexts []struct {
		Name    string `json:"name"`
		Context struct {
			Cluster   string `json:"cluster"`
			User      string `json:"user"`
			Namespace string `json:"namespace"`
		} `json:"context"`
	} `json:"contexts"`
	Clusters []struct {
		Name    string          `json:"name"`
		Cluster json.RawMessage `json:"cluster"`
	} `json:"clusters"`
	Users []struct {
		Name string          `json:"name"`
		User json.RawMessage `json:"user"`
	} `json:"users"`
}

type kubeSourceView struct {
	Path string
	View kubeConfigView
}

type resolvedKubeSource struct {
	Source kubeSourceView
	Paths  []string
}

type SourceRevision struct {
	Value string
	Err   error
}

func Discover(ctx context.Context) (Result, error) {
	observations, err := ObserveLocal(ctx)
	if err != nil {
		return Result{}, err
	}
	return Normalize(observations)
}

func discoverKubernetesSources(ctx context.Context, pathList string) ([]kubeSourceView, []string) {
	var sources []kubeSourceView
	var warnings []string
	for _, path := range filepath.SplitList(pathList) {
		if path == "" {
			continue
		}
		view, err := loadKubeView(ctx, path)
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return nil, append(warnings, "kubectl is not installed")
			}
			warnings = append(warnings, fmt.Sprintf("could not inspect kubeconfig %s", path))
			continue
		}
		sources = append(sources, kubeSourceView{Path: path, View: view})
	}
	return sources, warnings
}

func importKubernetesSources(cfg *cloudconfig.Config, projectNames map[string]string, sources []kubeSourceView) []string {
	if cfg.KubernetesAliases == nil {
		cfg.KubernetesAliases = map[string][]cloudconfig.KubernetesAlias{}
	}
	result := Result{Config: *cfg}
	normalizeKubernetes(&result, observeKubernetesContexts(sources, time.Now().UTC().Format(time.RFC3339)), projectNames)
	*cfg = result.Config
	return result.Warnings
}

func loadKubeView(ctx context.Context, path string) (kubeConfigView, error) {
	command := exec.CommandContext(ctx, "kubectl", "config", "view", "-o", "json")
	command.Env = environmentWithValue(os.Environ(), "KUBECONFIG", path)
	output, err := command.Output()
	if err != nil {
		return kubeConfigView{}, err
	}
	var view kubeConfigView
	if err := json.Unmarshal(output, &view); err != nil {
		return kubeConfigView{}, err
	}
	return view, nil
}

func environmentWithValue(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

func kubeContextSignature(view kubeConfigView, contextName string) string {
	for _, context := range view.Contexts {
		if context.Name != contextName {
			continue
		}
		// Namespace is a context/session preset, not part of the credentialed
		// access path to a control plane. Contexts that differ only by namespace
		// share an access profile and retain their namespace on their aliases.
		// Keep the historical empty-namespace slot so existing profiles that
		// already used no namespace retain their AccessID. Only namespace-bearing
		// legacy IDs need reconciliation.
		parts := []string{""}
		clusterDefinition := "name:" + context.Context.Cluster
		for _, cluster := range view.Clusters {
			if cluster.Name == context.Context.Cluster {
				clusterDefinition = string(cluster.Cluster)
				break
			}
		}
		parts = append(parts, clusterDefinition)
		userDefinition := "name:" + context.Context.User
		for _, user := range view.Users {
			if user.Name == context.Context.User {
				userDefinition = string(user.User)
				break
			}
		}
		parts = append(parts, userDefinition)
		return strings.Join(parts, "\x00")
	}
	return ""
}

// ValidateKubernetesSource refuses an ambiguous merged context. The user can
// resolve it by setting the target's kubeconfig to one specific source file.
func ValidateKubernetesSource(ctx context.Context, target cloudconfig.Kubernetes) error {
	if target.Context == "" || target.Kubeconfig == "" {
		return nil
	}
	_, err := resolveKubernetesSource(ctx, target)
	return err
}

// resolveKubernetesSource repairs older imports that recorded the entire
// KUBECONFIG path list for every context. Missing unrelated files are ignored
// when exactly one readable source still contains the requested definition.
func resolveKubernetesSource(ctx context.Context, target cloudconfig.Kubernetes) (resolvedKubeSource, error) {
	return resolveKubernetesSourceCached(ctx, target, nil)
}

type cachedKubeView struct {
	view kubeConfigView
	err  error
}

func resolveKubernetesSourceCached(ctx context.Context, target cloudconfig.Kubernetes, cache map[string]cachedKubeView) (resolvedKubeSource, error) {
	var sources []kubeSourceView
	var missing []string
	for _, path := range filepath.SplitList(target.Kubeconfig) {
		if path == "" {
			continue
		}
		expanded, err := expandHome(path)
		if err != nil {
			return resolvedKubeSource{}, err
		}
		if _, err := os.Stat(expanded); err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, path)
				continue
			}
			return resolvedKubeSource{}, fmt.Errorf("inspect kubeconfig source %s: %w", path, err)
		}
		loaded, ok := cache[expanded]
		if !ok {
			loaded.view, loaded.err = loadKubeView(ctx, expanded)
			if cache != nil {
				cache[expanded] = loaded
			}
		}
		if loaded.err != nil {
			return resolvedKubeSource{}, fmt.Errorf("inspect kubeconfig %s: %w", path, loaded.err)
		}
		sources = append(sources, kubeSourceView{Path: path, View: loaded.view})
	}
	resolved, err := selectKubernetesSource(target.Context, sources)
	if err != nil {
		if len(missing) > 0 {
			return resolvedKubeSource{}, fmt.Errorf("%w; missing configured sources: %s; restore the missing source or run `chop catalog edit` to update the Kubernetes target", err, strings.Join(missing, ", "))
		}
		return resolvedKubeSource{}, err
	}
	return resolved, nil
}

func selectKubernetesSource(contextName string, sources []kubeSourceView) (resolvedKubeSource, error) {
	definitions := map[string][]kubeSourceView{}
	for _, source := range sources {
		if signature := kubeContextSignature(source.View, contextName); signature != "" {
			definitions[signature] = append(definitions[signature], source)
		}
	}
	if len(definitions) == 0 {
		return resolvedKubeSource{}, fmt.Errorf("kubernetes context %q no longer exists in its configured source", contextName)
	}
	if len(definitions) > 1 {
		var paths []string
		for _, definitionSources := range definitions {
			for _, source := range definitionSources {
				paths = append(paths, source.Path)
			}
		}
		sort.Strings(paths)
		return resolvedKubeSource{}, fmt.Errorf("kubernetes context %q has conflicting definitions in %s; set its kubeconfig to one specific source file", contextName, strings.Join(paths, ", "))
	}
	for _, definitionSources := range definitions {
		paths := make([]string, 0, len(definitionSources))
		for _, source := range definitionSources {
			paths = append(paths, source.Path)
		}
		sort.Strings(paths)
		return resolvedKubeSource{Source: definitionSources[0], Paths: paths}, nil
	}
	panic("unreachable")
}

// CaptureKubernetesSourceRevisions records the local source state displayed by
// a picker. Values are kept in memory only and contain no kubeconfig contents.
func CaptureKubernetesSourceRevisions(cfg cloudconfig.Config) map[string]SourceRevision {
	result := make(map[string]SourceRevision, len(cfg.Kubernetes))
	cache := map[string]SourceRevision{}
	for name, target := range cfg.Kubernetes {
		if target.Kubeconfig == "" {
			continue
		}
		revision, ok := cache[target.Kubeconfig]
		if !ok {
			revision.Value, revision.Err = kubeconfigSourceRevision(target.Kubeconfig)
			cache[target.Kubeconfig] = revision
		}
		result[name] = revision
	}
	return result
}

func ValidateKubernetesSourceRevision(target cloudconfig.Kubernetes, expected SourceRevision) error {
	if expected.Err != nil {
		return expected.Err
	}
	observed, err := kubeconfigSourceRevision(target.Kubeconfig)
	if err != nil {
		return err
	}
	if observed != expected.Value {
		return fmt.Errorf("kubeconfig source changed while %q was being selected; refresh and select it again", target.Context)
	}
	return nil
}

func KubernetesSourceRevision(target cloudconfig.Kubernetes) (string, error) {
	if target.Kubeconfig == "" {
		return "", nil
	}
	return kubeconfigSourceRevision(target.Kubeconfig)
}

func kubeconfigSourceRevision(pathList string) (string, error) {
	hash := sha256.New()
	for _, path := range filepath.SplitList(pathList) {
		expanded, err := expandHome(path)
		if err != nil {
			return "", err
		}
		contents, err := os.ReadFile(expanded)
		if err != nil {
			if os.IsNotExist(err) {
				// KUBECONFIG path lists routinely retain removed optional files.
				// Record their absence in the revision so a later appearance is
				// still detected, then let resolveKubernetesSource decide whether
				// the selected context exists in one of the readable sources.
				hash.Write([]byte(path))
				hash.Write([]byte("\x00missing\x00"))
				continue
			}
			return "", fmt.Errorf("read kubeconfig source %s: %w", path, err)
		}
		hash.Write([]byte(path))
		hash.Write([]byte{0})
		hash.Write(contents)
		hash.Write([]byte{0})
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// EnrichKubernetesDependencies repairs imported aliases whose context name is
// friendly but whose underlying cluster name carries GKE dependency metadata.
func EnrichKubernetesDependencies(ctx context.Context, cfg *cloudconfig.Config, projectFilter string) error {
	views := map[string]cachedKubeView{}
	for name, target := range cfg.Kubernetes {
		if target.Kubeconfig == "" {
			continue
		}
		resolvedSource, err := resolveKubernetesSourceCached(ctx, target, views)
		if err != nil {
			// A stale unrelated target must not prevent another target from
			// being displayed or selected. The selected target is checked by
			// ValidateKubernetesSource before any authentication or mutation.
			continue
		}
		target.Kubeconfig = resolvedSource.Source.Path
		view := resolvedSource.Source.View
		if target.AccessID == "" {
			target.AccessID = shortHash(kubeContextSignature(view, target.Context))
		}
		if target.Namespace == "" {
			target.Namespace = namespaceForContext(view, target.Context)
		}
		projectID, location, cluster, isGKE := gkeMetadataForContext(view, target.Context)
		if !isGKE {
			// A plain kubeconfig context name is often a friendly alias. Retain
			// its underlying cluster reference as the best available physical
			// name without treating that display value as canonical identity.
			// Explicit managed targets already carry authoritative provider
			// metadata and must not be replaced by a kubeconfig-local alias.
			if target.Type != "gke" {
				target.Cluster = clusterForContext(view, target.Context)
			}
			if target.ProviderID == "" {
				target.ProviderID = "kube:" + shortHash(kubeClusterSignature(view, target.Cluster))
			}
			cfg.Kubernetes[name] = target
			continue
		}
		// The encoded GKE cluster reference is provider metadata, so expose
		// the provider cluster and location even when project mapping still
		// needs to be resolved. Never replace it with the context alias.
		target.Cluster = cluster
		target.Location = location
		target.ProviderID = "gke:" + projectID + "/" + location + "/" + cluster
		cfg.Kubernetes[name] = target
		for projectName, project := range cfg.Projects {
			if project.Provider == "gcp" && project.ProjectID == projectID {
				if projectFilter != "" && projectName != projectFilter {
					break
				}
				if target.Project != "" && target.Project != projectName {
					break
				}
				target.Type = "gke"
				target.Project = projectName
				target.Cluster = cluster
				target.Location = location
				cfg.Kubernetes[name] = target
				break
			}
		}
	}
	return nil
}

func namespaceForContext(view kubeConfigView, contextName string) string {
	for _, context := range view.Contexts {
		if context.Name == contextName {
			return context.Context.Namespace
		}
	}
	return ""
}

func clusterForContext(view kubeConfigView, contextName string) string {
	for _, context := range view.Contexts {
		if context.Name == contextName {
			return context.Context.Cluster
		}
	}
	return ""
}

func gkeMetadataForContext(view kubeConfigView, contextName string) (project, location, cluster string, ok bool) {
	for _, context := range view.Contexts {
		if context.Name == contextName {
			return parseGKEContext(context.Context.Cluster)
		}
	}
	return "", "", "", false
}

func run(parent context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

func ensureProject(cfg *cloudconfig.Config, names map[string]string, projectID string) string {
	if existing := names[projectID]; existing != "" {
		return existing
	}
	name := uniqueName(slug(projectID), cfg.Projects)
	names[projectID] = name
	cfg.Projects[name] = cloudconfig.Project{Provider: "gcp", ProjectID: projectID}
	return name
}

func parseGKEContext(value string) (project, location, cluster string, ok bool) {
	if !strings.HasPrefix(value, "gke_") {
		return "", "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(value, "gke_"), "_", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func currentKubeconfigPath() string {
	if value := os.Getenv("KUBECONFIG"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.kube/config"
	}
	return filepath.Join(home, ".kube", "config")
}

func identityName(account string) string {
	local, domain, found := strings.Cut(account, "@")
	if !found {
		return slug(account)
	}
	organization := strings.Split(domain, ".")[0]
	if organization == "gmail" || organization == "googlemail" || organization == "" {
		return slug(local + "-" + organization)
	}
	return slug(organization)
}

func isServiceAccount(account string) bool {
	return strings.HasSuffix(account, ".iam.gserviceaccount.com") || strings.HasSuffix(account, "@developer.gserviceaccount.com")
}

func inferRisk(value string) string {
	value = strings.ToLower(value)
	if strings.Contains(value, "prod") {
		return "production"
	}
	if strings.Contains(value, "sandbox") {
		return "sandbox"
	}
	return ""
}

func slug(value string) string {
	value = strings.ToLower(value)
	var result strings.Builder
	lastDash := false
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			result.WriteRune(character)
			lastDash = false
		} else if result.Len() > 0 && !lastDash {
			result.WriteByte('-')
			lastDash = true
		}
	}
	name := strings.Trim(result.String(), "-")
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		name = "context-" + name
	}
	return name
}

func uniqueName[T any](base string, existing map[string]T) string {
	if _, found := existing[base]; !found {
		return base
	}
	for number := 2; ; number++ {
		candidate := fmt.Sprintf("%s-%d", base, number)
		if _, found := existing[candidate]; !found {
			return candidate
		}
	}
}

func nonEmptyLines(data []byte) []string {
	var result []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			result = append(result, line)
		}
	}
	return result
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
