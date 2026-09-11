package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const Version = 1

// ValidateName accepts human-readable catalog names; control characters cannot
// be displayed safely in the terminal or passed through the shell environment.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("must not be empty")
	}
	if !utf8.ValidString(name) {
		return errors.New("must be valid UTF-8")
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return errors.New("must not contain control characters or line breaks")
		}
	}
	return nil
}

type Config struct {
	Tags              map[string]Tag               `yaml:"tags,omitempty"`
	Discovery         map[string]IdentityDiscovery `yaml:"discovery,omitempty"`
	Version           int                          `yaml:"version"`
	Identities        map[string]Identity          `yaml:"identities,omitempty"`
	Projects          map[string]Project           `yaml:"projects,omitempty"`
	Kubernetes        map[string]Kubernetes        `yaml:"kubernetes,omitempty"`
	KubernetesAliases map[string][]KubernetesAlias `yaml:"kubernetesAliases,omitempty"`
	Docker            map[string]Docker            `yaml:"docker,omitempty"`
	Destinations      map[string]Destination       `yaml:"workspaces,omitempty"`
	// LegacyDestinations accepts version 1 files written before the user-facing
	// term was changed to workspace. It is never emitted.
	LegacyDestinations map[string]Destination `yaml:"destinations,omitempty"`
}

type Identity struct {
	Pinned         bool           `yaml:"pinned,omitempty"`
	Browser        BrowserProfile `yaml:"browser,omitempty"`
	LabelSet       `yaml:",inline"`
	Provider       string `yaml:"provider"`
	Kind           string `yaml:"kind,omitempty"`
	Account        string `yaml:"account"`
	CloudSDKConfig string `yaml:"cloudSdkConfig,omitempty"`
	ADC            string `yaml:"adc,omitempty"`
	Provenance     string `yaml:"provenance,omitempty"`
	VerifiedBy     string `yaml:"verifiedBy,omitempty"`
	ObservedAt     string `yaml:"observedAt,omitempty"`
	Hidden         bool   `yaml:"hidden,omitempty"`
}

type Project struct {
	Pinned            bool `yaml:"pinned,omitempty"`
	LabelSet          `yaml:",inline"`
	ManualIdentities  []string `yaml:"manualIdentities,omitempty"`
	Provider          string   `yaml:"provider"`
	ProjectID         string   `yaml:"projectId"`
	ProjectNumber     string   `yaml:"projectNumber,omitempty"`
	Identities        []string `yaml:"identities,omitempty"`
	PreferredIdentity string   `yaml:"preferredIdentity,omitempty"`
	Risk              string   `yaml:"risk,omitempty"`
	Provenance        string   `yaml:"provenance,omitempty"`
	VerifiedBy        string   `yaml:"verifiedBy,omitempty"`
	ObservedAt        string   `yaml:"observedAt,omitempty"`
	Hidden            bool     `yaml:"hidden,omitempty"`
}

type Kubernetes struct {
	Pinned         bool `yaml:"pinned,omitempty"`
	LabelSet       `yaml:",inline"`
	ManualIdentity string `yaml:"manualIdentity,omitempty"`
	Type           string `yaml:"type"`
	Project        string `yaml:"project,omitempty"`
	Cluster        string `yaml:"cluster,omitempty"`
	Location       string `yaml:"location,omitempty"`
	ProviderID     string `yaml:"providerId,omitempty"`
	// AccessID identifies endpoint, TLS, and credential semantics. Namespace is
	// a context/session preset and is intentionally excluded.
	AccessID          string `yaml:"accessId,omitempty"`
	Endpoint          string `yaml:"endpoint,omitempty"`
	PreferredIdentity string `yaml:"preferredIdentity,omitempty"`
	Kubeconfig        string `yaml:"kubeconfig,omitempty"`
	Context           string `yaml:"context,omitempty"`
	Namespace         string `yaml:"namespace,omitempty"`
	Risk              string `yaml:"risk,omitempty"`
	Provenance        string `yaml:"provenance,omitempty"`
	VerifiedBy        string `yaml:"verifiedBy,omitempty"`
	ObservedAt        string `yaml:"observedAt,omitempty"`
	Hidden            bool   `yaml:"hidden,omitempty"`
}

// KubernetesAlias records a discovered name and source for an access profile.
// Aliases are relationships, not independently selectable physical clusters.
type KubernetesAlias struct {
	Context    string `yaml:"context"`
	Kubeconfig string `yaml:"kubeconfig,omitempty"`
	Namespace  string `yaml:"namespace,omitempty"`
	Provenance string `yaml:"provenance,omitempty"`
	ObservedAt string `yaml:"observedAt,omitempty"`
}

type Docker struct {
	Pinned     bool `yaml:"pinned,omitempty"`
	LabelSet   `yaml:",inline"`
	Context    string `yaml:"context"`
	Risk       string `yaml:"risk,omitempty"`
	Provenance string `yaml:"provenance,omitempty"`
	VerifiedBy string `yaml:"verifiedBy,omitempty"`
	ObservedAt string `yaml:"observedAt,omitempty"`
	Hidden     bool   `yaml:"hidden,omitempty"`
}

type Destination struct {
	Pinned     bool `yaml:"pinned,omitempty"`
	LabelSet   `yaml:",inline"`
	Identity   string `yaml:"identity,omitempty"`
	Project    string `yaml:"project,omitempty"`
	Kubernetes string `yaml:"kubernetes,omitempty"`
	Docker     string `yaml:"docker,omitempty"`
	ADC        string `yaml:"adc,omitempty"`
	Risk       string `yaml:"risk,omitempty"`
	Provenance string `yaml:"provenance,omitempty"`
	VerifiedBy string `yaml:"verifiedBy,omitempty"`
	ObservedAt string `yaml:"observedAt,omitempty"`
	Hidden     bool   `yaml:"hidden,omitempty"`
}

func New() Config {
	return Config{
		Version:           Version,
		Tags:              map[string]Tag{},
		Discovery:         map[string]IdentityDiscovery{},
		Identities:        map[string]Identity{},
		Projects:          map[string]Project{},
		Kubernetes:        map[string]Kubernetes{},
		KubernetesAliases: map[string][]KubernetesAlias{},
		Docker:            map[string]Docker{},
		Destinations:      map[string]Destination{},
	}
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(dir, "contexthop", "config.yaml"), nil
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	cfg, err := Decode(f)
	if err != nil {
		return Config{}, fmt.Errorf("load %s: %w", path, err)
	}
	return cfg, nil
}

func Decode(r io.Reader) (Config, error) {
	cfg := New()
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, err
	}
	for name, workspace := range cfg.LegacyDestinations {
		if _, exists := cfg.Destinations[name]; exists {
			return Config{}, fmt.Errorf("workspace %q is defined in both workspaces and legacy destinations", name)
		}
		cfg.Destinations[name] = workspace
	}
	cfg.LegacyDestinations = nil
	cfg.MigrateLabels()
	cfg.MigrateTags()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Encode(w io.Writer, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(cfg)
}

func (c Config) Validate() error {
	var problems []string
	for name, identity := range c.Identities {
		if err := identity.Browser.Validate(); err != nil {
			problems = append(problems, fmt.Sprintf("identity %q browser: %v", name, err))
		}
	}
	for name, tag := range c.Tags {
		if err := ValidateName(name); err != nil {
			problems = append(problems, fmt.Sprintf("tag %q: %v", name, err))
		}
		if !ValidTagColor(tag.Color) {
			problems = append(problems, fmt.Sprintf("tag %q has invalid colour %q", name, tag.Color))
		}
	}
	if c.Version != Version {
		problems = append(problems, fmt.Sprintf("version must be %d", Version))
	}

	validateNames := func(kind string, names []string) {
		for _, name := range names {
			if err := ValidateName(name); err != nil {
				problems = append(problems, fmt.Sprintf("%s %q %s", kind, name, err))
			}
		}
	}
	validateNames("identity", keys(c.Identities))
	validateNames("project", keys(c.Projects))
	validateNames("kubernetes target", keys(c.Kubernetes))
	validateNames("docker target", keys(c.Docker))
	validateNames("workspace", keys(c.Destinations))

	for name, identity := range c.Identities {
		for _, tag := range identity.Tags {
			if err := ValidateName(tag); err != nil {
				problems = append(problems, fmt.Sprintf("%s tag: %v", name, err))
			}
		}
		for k, v := range identity.LabelSet.Effective("") {
			if err := ValidateName(k); err != nil {
				problems = append(problems, fmt.Sprintf("%s label key: %v", name, err))
			}
			if v != "" {
				if err := ValidateName(v); err != nil {
					problems = append(problems, fmt.Sprintf("%s label value: %v", name, err))
				}
			}
		}
		validateProvenance("identity", name, identity.Provenance, identity.VerifiedBy, identity.ObservedAt, &problems)
		if identity.Provider == "" {
			problems = append(problems, fmt.Sprintf("identity %q has no provider", name))
		}
		if identity.Account == "" {
			problems = append(problems, fmt.Sprintf("identity %q has no account", name))
		}
		switch identity.Kind {
		case "", "user", "service-account", "external-account":
		default:
			problems = append(problems, fmt.Sprintf("identity %q has unsupported kind %q", name, identity.Kind))
		}
	}

	for name, project := range c.Projects {
		for _, tag := range project.Tags {
			if err := ValidateName(tag); err != nil {
				problems = append(problems, fmt.Sprintf("%s tag: %v", name, err))
			}
		}
		for k, v := range project.LabelSet.Effective("") {
			if err := ValidateName(k); err != nil {
				problems = append(problems, fmt.Sprintf("%s label key: %v", name, err))
			}
			if v != "" {
				if err := ValidateName(v); err != nil {
					problems = append(problems, fmt.Sprintf("%s label value: %v", name, err))
				}
			}
		}
		validateProvenance("project", name, project.Provenance, project.VerifiedBy, project.ObservedAt, &problems)
		if project.Provider == "" {
			problems = append(problems, fmt.Sprintf("project %q has no provider", name))
		}
		if project.ProjectID == "" {
			problems = append(problems, fmt.Sprintf("project %q has no projectId", name))
		}
		validateRisk("project", name, project.Risk, &problems)
		for _, identityName := range project.ManualIdentities {
			if !contains(project.Identities, identityName) {
				problems = append(problems, fmt.Sprintf("project %q manual identity %q is not associated with the project", name, identityName))
			}
		}
		for _, identityName := range project.Identities {
			identity, ok := c.Identities[identityName]
			if !ok {
				problems = append(problems, fmt.Sprintf("project %q references unknown identity %q", name, identityName))
				continue
			}
			if identity.Provider != project.Provider {
				problems = append(problems, fmt.Sprintf("project %q provider %q is incompatible with identity %q provider %q", name, project.Provider, identityName, identity.Provider))
			}
		}
		if project.PreferredIdentity != "" && !contains(project.Identities, project.PreferredIdentity) {
			problems = append(problems, fmt.Sprintf("project %q prefers identity %q, but that identity is not mapped to the project", name, project.PreferredIdentity))
		}
	}

	for name, target := range c.Kubernetes {
		for _, tag := range target.Tags {
			if err := ValidateName(tag); err != nil {
				problems = append(problems, fmt.Sprintf("%s tag: %v", name, err))
			}
		}
		for k, v := range target.LabelSet.Effective("") {
			if err := ValidateName(k); err != nil {
				problems = append(problems, fmt.Sprintf("%s label key: %v", name, err))
			}
			if v != "" {
				if err := ValidateName(v); err != nil {
					problems = append(problems, fmt.Sprintf("%s label value: %v", name, err))
				}
			}
		}
		validateProvenance("kubernetes target", name, target.Provenance, target.VerifiedBy, target.ObservedAt, &problems)
		validateRisk("kubernetes target", name, target.Risk, &problems)
		switch target.Type {
		case "gke":
			if target.Project == "" || target.Cluster == "" || target.Location == "" {
				problems = append(problems, fmt.Sprintf("GKE target %q requires project, cluster, and location", name))
			}
			project, ok := c.Projects[target.Project]
			if target.Project != "" && !ok {
				problems = append(problems, fmt.Sprintf("GKE target %q references unknown project %q", name, target.Project))
			} else if ok && project.Provider != "gcp" {
				problems = append(problems, fmt.Sprintf("GKE target %q requires a GCP project", name))
			}
			if target.Endpoint != "" && target.Endpoint != "dns" && target.Endpoint != "internal-ip" {
				problems = append(problems, fmt.Sprintf("GKE target %q has unsupported endpoint %q", name, target.Endpoint))
			}
		case "kubeconfig":
			if target.Project != "" {
				if _, ok := c.Projects[target.Project]; !ok {
					problems = append(problems, fmt.Sprintf("kubeconfig target %q references unknown project %q", name, target.Project))
				}
			}
			if target.Kubeconfig == "" {
				problems = append(problems, fmt.Sprintf("kubeconfig target %q requires kubeconfig", name))
			}
			if target.Endpoint != "" {
				problems = append(problems, fmt.Sprintf("kubeconfig target %q cannot select a GKE endpoint", name))
			}
		case "":
			problems = append(problems, fmt.Sprintf("kubernetes target %q has no type", name))
		default:
			problems = append(problems, fmt.Sprintf("kubernetes target %q has unsupported type %q", name, target.Type))
		}
		if target.ManualIdentity != "" {
			project, ok := c.Projects[target.Project]
			if !ok || !contains(project.Identities, target.ManualIdentity) {
				problems = append(problems, fmt.Sprintf("kubernetes target %q manual identity %q is not associated with its project", name, target.ManualIdentity))
			}
		}
		if target.PreferredIdentity != "" {
			project, ok := c.Projects[target.Project]
			if !ok || !contains(project.Identities, target.PreferredIdentity) {
				problems = append(problems, fmt.Sprintf("kubernetes target %q prefers identity %q, but that identity is not mapped to its project", name, target.PreferredIdentity))
			}
		}
	}

	for targetName, aliases := range c.KubernetesAliases {
		if _, ok := c.Kubernetes[targetName]; !ok {
			problems = append(problems, fmt.Sprintf("kubernetes aliases reference unknown target %q", targetName))
			continue
		}
		seen := map[string]bool{}
		for _, alias := range aliases {
			if alias.Context == "" {
				problems = append(problems, fmt.Sprintf("kubernetes target %q has an alias with no context", targetName))
				continue
			}
			key := alias.Kubeconfig + "\x00" + alias.Context
			if seen[key] {
				problems = append(problems, fmt.Sprintf("kubernetes target %q repeats alias %q from %q", targetName, alias.Context, alias.Kubeconfig))
			}
			seen[key] = true
		}
	}

	for name, target := range c.Docker {
		for _, tag := range target.Tags {
			if err := ValidateName(tag); err != nil {
				problems = append(problems, fmt.Sprintf("%s tag: %v", name, err))
			}
		}
		for k, v := range target.LabelSet.Effective("") {
			if err := ValidateName(k); err != nil {
				problems = append(problems, fmt.Sprintf("%s label key: %v", name, err))
			}
			if v != "" {
				if err := ValidateName(v); err != nil {
					problems = append(problems, fmt.Sprintf("%s label value: %v", name, err))
				}
			}
		}
		validateProvenance("docker target", name, target.Provenance, target.VerifiedBy, target.ObservedAt, &problems)
		if target.Context == "" {
			problems = append(problems, fmt.Sprintf("docker target %q has no context", name))
		}
		validateRisk("docker target", name, target.Risk, &problems)
	}

	for name, destination := range c.Destinations {
		for _, tag := range destination.Tags {
			if err := ValidateName(tag); err != nil {
				problems = append(problems, fmt.Sprintf("%s tag: %v", name, err))
			}
		}
		for k, v := range destination.LabelSet.Effective("") {
			if err := ValidateName(k); err != nil {
				problems = append(problems, fmt.Sprintf("%s label key: %v", name, err))
			}
			if v != "" {
				if err := ValidateName(v); err != nil {
					problems = append(problems, fmt.Sprintf("%s label value: %v", name, err))
				}
			}
		}
		validateProvenance("workspace", name, destination.Provenance, destination.VerifiedBy, destination.ObservedAt, &problems)
		validateRisk("workspace", name, destination.Risk, &problems)
		if destination.Identity == "" && destination.Project == "" && destination.Kubernetes == "" && destination.Docker == "" {
			problems = append(problems, fmt.Sprintf("workspace %q contains no components", name))
			continue
		}
		identity, identityOK := c.Identities[destination.Identity]
		project, projectOK := c.Projects[destination.Project]
		target, targetOK := c.Kubernetes[destination.Kubernetes]
		if destination.Identity != "" && !identityOK {
			problems = append(problems, fmt.Sprintf("workspace %q references unknown identity %q", name, destination.Identity))
		}
		if destination.Project != "" && !projectOK {
			problems = append(problems, fmt.Sprintf("workspace %q references unknown project %q", name, destination.Project))
		}
		if destination.Kubernetes != "" && !targetOK {
			problems = append(problems, fmt.Sprintf("workspace %q references unknown kubernetes target %q", name, destination.Kubernetes))
		}
		if destination.Docker != "" {
			if _, ok := c.Docker[destination.Docker]; !ok {
				problems = append(problems, fmt.Sprintf("workspace %q references unknown docker target %q", name, destination.Docker))
			}
		}
		if destination.ADC != "" && destination.ADC != "identity" {
			problems = append(problems, fmt.Sprintf("workspace %q has unsupported ADC mode %q", name, destination.ADC))
		}
		if destination.ADC == "identity" && destination.Identity == "" {
			adcProject, adcProjectOK := project, projectOK
			if !adcProjectOK && targetOK && target.Project != "" {
				adcProject, adcProjectOK = c.Projects[target.Project]
			}
			if !adcProjectOK || len(adcProject.Identities) != 1 {
				problems = append(problems, fmt.Sprintf("workspace %q requires ADC but does not resolve exactly one identity", name))
			}
		}
		if projectOK && identityOK && !contains(project.Identities, destination.Identity) {
			problems = append(problems, fmt.Sprintf("workspace %q identity %q is not mapped to project %q", name, destination.Identity, destination.Project))
		}
		if projectOK && identityOK && project.Provider != identity.Provider {
			problems = append(problems, fmt.Sprintf("workspace %q mixes providers %q and %q", name, project.Provider, identity.Provider))
		}
		// A workspace may name only a Kubernetes target and inherit its project.
		// Reject only an explicitly configured project that contradicts it.
		if targetOK && target.Project != "" && destination.Project != "" && destination.Project != target.Project {
			problems = append(problems, fmt.Sprintf("workspace %q project %q does not match kubernetes target project %q", name, destination.Project, target.Project))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func validateProvenance(kind, name, provenance, verifiedBy, observedAt string, problems *[]string) {
	allowed := func(value string) bool {
		switch value {
		case "", "manual", "gcp", "kubeconfig", "docker":
			return true
		default:
			return false
		}
	}
	if !allowed(provenance) {
		*problems = append(*problems, fmt.Sprintf("%s %q has unsupported provenance %q", kind, name, provenance))
	}
	if !allowed(verifiedBy) || verifiedBy == "manual" {
		*problems = append(*problems, fmt.Sprintf("%s %q has unsupported verifier %q", kind, name, verifiedBy))
	}
	if observedAt != "" {
		if _, err := time.Parse(time.RFC3339, observedAt); err != nil {
			*problems = append(*problems, fmt.Sprintf("%s %q has invalid observedAt %q", kind, name, observedAt))
		}
	}
	if verifiedBy != "" && observedAt == "" {
		*problems = append(*problems, fmt.Sprintf("%s %q has verifier %q without observedAt", kind, name, verifiedBy))
	}
	if observedAt != "" && verifiedBy == "" {
		*problems = append(*problems, fmt.Sprintf("%s %q has observedAt without a verifier", kind, name))
	}
}

func validateRisk(kind, name, risk string, problems *[]string) {
	if risk != "" && risk != "sandbox" && risk != "development" && risk != "production" {
		*problems = append(*problems, fmt.Sprintf("%s %q has unsupported risk %q", kind, name, risk))
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func keys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for name := range values {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
