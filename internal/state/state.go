package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/infurio/contexthop/internal/kubetarget"
	"gopkg.in/yaml.v3"
)

const SessionFileEnv = "CONTEXTHOP_SESSION_FILE"

type Component struct {
	Identity   string `json:"identity,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Project    string `json:"project,omitempty"`
	Kubernetes string `json:"kubernetes,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Docker     string `json:"docker,omitempty"`
	ADC        string `json:"adc,omitempty"`
}

type Manifest struct {
	PromptColors                      map[string]string `json:"promptColors,omitempty"`
	Production                        bool              `json:"production,omitempty"`
	Previous                          *Selection        `json:"previous,omitempty"`
	Version                           int               `json:"version"`
	SessionID                         string            `json:"sessionId"`
	Destination                       string            `json:"destination,omitempty"`
	WorkspaceName                     string            `json:"workspaceName,omitempty"`
	IdentityName                      string            `json:"identityName,omitempty"`
	ProjectName                       string            `json:"projectName,omitempty"`
	KubernetesName                    string            `json:"kubernetesName,omitempty"`
	DockerName                        string            `json:"dockerName,omitempty"`
	KubernetesLabel                   string            `json:"kubernetesLabel,omitempty"`
	Risk                              string            `json:"risk,omitempty"`
	Expected                          Component         `json:"expected"`
	CloudSDKConfig                    string            `json:"cloudSdkConfig,omitempty"`
	KubernetesTargetIdentity          string            `json:"kubernetesTargetIdentity,omitempty"`
	KubernetesSourceRevision          string            `json:"kubernetesSourceRevision,omitempty"`
	KubernetesControlPlaneFingerprint string            `json:"kubernetesControlPlaneFingerprint,omitempty"`
	ADCMode                           string            `json:"adcMode,omitempty"`
}

// Selection stores references, never credentials or session-directory paths.
// Previous lives in the current shell's manifest and is not global history.
type Selection struct {
	Name, Workspace, Identity, Project, Kubernetes, Docker, ADC, Namespace string
	Account, ProjectID, TargetIdentity, DockerContext                      string
}

func (m Manifest) Selection() Selection {
	return Selection{Name: m.Destination, Workspace: m.WorkspaceName, Identity: m.IdentityName, Project: m.ProjectName, Kubernetes: m.KubernetesName, Docker: m.DockerName, ADC: m.ADCMode, Namespace: m.Expected.Namespace, Account: m.Expected.Identity, ProjectID: m.Expected.Project, TargetIdentity: m.KubernetesTargetIdentity, DockerContext: m.Expected.Docker}
}

type Snapshot struct {
	SharedConfig                  *Manifest
	SharedConfigError             string
	SharedConfigChecked           bool
	Scope                         string
	Managed                       bool
	SessionID                     string
	Destination                   string
	Risk                          string
	Expected                      Component
	Observed                      Component
	LocalStatus                   string
	Message                       string
	ExpectedKubernetesFingerprint string
	ObservedKubernetesFingerprint string
}

func InspectLocal() Snapshot {
	kubernetes, namespace := readKubeState()
	snapshot := Snapshot{
		Scope: os.Getenv("CONTEXTHOP_SCOPE"),
		Observed: Component{
			Provider:   detectProvider(),
			Project:    firstNonEmpty(os.Getenv("CLOUDSDK_CORE_PROJECT"), readGcloudProperty("project")),
			Identity:   firstNonEmpty(os.Getenv("CLOUDSDK_CORE_ACCOUNT"), readGcloudProperty("account")),
			Kubernetes: kubernetes,
			Namespace:  namespace,
			Docker:     firstNonEmpty(os.Getenv("DOCKER_CONTEXT"), readDockerCurrentContext()),
			ADC:        os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
		},
		LocalStatus: "UNMANAGED",
	}

	manifestPath := os.Getenv(SessionFileEnv)
	if manifestPath == "" {
		snapshot.Message = "Not in an isolated ContextHop session"
		return snapshot
	}
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		snapshot.LocalStatus = "CONTEXT-DRIFT"
		snapshot.Message = fmt.Sprintf("Cannot read session manifest: %v", err)
		return snapshot
	}

	snapshot.Managed = true
	snapshot.SessionID = manifest.SessionID
	snapshot.Destination = manifest.Destination
	snapshot.Expected = manifest.Expected
	snapshot.ExpectedKubernetesFingerprint = manifest.KubernetesControlPlaneFingerprint
	if manifest.KubernetesControlPlaneFingerprint != "" && snapshot.Observed.Kubernetes != "" {
		if fingerprint, err := kubetarget.FingerprintFile(os.Getenv("KUBECONFIG")); err == nil {
			snapshot.ObservedKubernetesFingerprint = fingerprint.Digest
		}
	}
	if mismatches := snapshot.Mismatches(); len(mismatches) > 0 {
		snapshot.LocalStatus = "CONTEXT-DRIFT"
		snapshot.Message = strings.Join(mismatches, "; ")
	} else {
		snapshot.LocalStatus = "LOCAL MATCH"
		snapshot.Message = "Expected and locally observed state agree"
	}
	return snapshot
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.Version != 1 {
		return Manifest{}, fmt.Errorf("unsupported manifest version %d", manifest.Version)
	}
	if manifest.SessionID == "" {
		return Manifest{}, errors.New("session manifest has no sessionId")
	}
	return manifest, nil
}

func (s Snapshot) Mismatches() []string {
	if !s.Managed {
		return nil
	}
	var result []string
	compare := func(label, expected, observed string) {
		if expected != observed {
			result = append(result, fmt.Sprintf("%s expected %q, observed %q", label, displayNone(expected), displayNone(observed)))
		}
	}
	compare("identity", s.Expected.Identity, s.Observed.Identity)
	compare("project", s.Expected.Project, s.Observed.Project)
	compare("kubernetes", s.Expected.Kubernetes, s.Observed.Kubernetes)
	compare("namespace", s.Expected.Namespace, s.Observed.Namespace)
	compare("docker", s.Expected.Docker, s.Observed.Docker)
	compare("ADC", s.Expected.ADC, s.Observed.ADC)
	if s.ExpectedKubernetesFingerprint != "" {
		compare("Kubernetes control plane", s.ExpectedKubernetesFingerprint, s.ObservedKubernetesFingerprint)
	}
	return result
}

func detectProvider() string {
	if os.Getenv("CONTEXTHOP_CONTEXT") != "" {
		if os.Getenv("CLOUDSDK_CORE_ACCOUNT") != "" {
			return "gcp"
		}
		if profile := os.Getenv("AWS_PROFILE"); profile != "" && profile != "contexthop-none" {
			return "aws"
		}
		return ""
	}
	if os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_CONFIG_FILE") != "" {
		return "aws"
	}
	if os.Getenv("CLOUDSDK_CONFIG") != "" || os.Getenv("CLOUDSDK_CORE_PROJECT") != "" || os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		return "gcp"
	}
	return ""
}

func readGcloudProperty(property string) string {
	configDir := os.Getenv("CLOUDSDK_CONFIG")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(home, ".config", "gcloud")
	}
	active := os.Getenv("CLOUDSDK_ACTIVE_CONFIG_NAME")
	if active == "" {
		data, err := os.ReadFile(filepath.Join(configDir, "active_config"))
		if err != nil {
			return ""
		}
		active = strings.TrimSpace(string(data))
	}
	data, err := os.ReadFile(filepath.Join(configDir, "configurations", "config_"+active))
	if err != nil {
		return ""
	}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			continue
		}
		if section != "core" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == property {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func readDockerCurrentContext() string {
	configDir := os.Getenv("DOCKER_CONFIG")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(home, ".docker")
	}
	data, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	if err != nil {
		return ""
	}
	var document struct {
		CurrentContext string `json:"currentContext"`
	}
	if json.Unmarshal(data, &document) != nil {
		return ""
	}
	return document.CurrentContext
}

func readKubeState() (string, string) {
	paths := os.Getenv("KUBECONFIG")
	if paths == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		paths = filepath.Join(home, ".kube", "config")
	}
	type kubeDocument struct {
		CurrentContext string `yaml:"current-context"`
		Contexts       []struct {
			Name    string `yaml:"name"`
			Context struct {
				Namespace string `yaml:"namespace"`
			} `yaml:"context"`
		} `yaml:"contexts"`
	}
	var currentContext string
	var documents []kubeDocument
	for _, path := range filepath.SplitList(paths) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var document kubeDocument
		if yaml.Unmarshal(data, &document) != nil {
			continue
		}
		documents = append(documents, document)
		if currentContext == "" && document.CurrentContext != "" {
			currentContext = document.CurrentContext
		}
	}
	if currentContext == "" {
		return "", ""
	}
	for _, document := range documents {
		for _, context := range document.Contexts {
			if context.Name == currentContext {
				namespace := context.Context.Namespace
				if namespace == "" {
					namespace = "default"
				}
				return currentContext, namespace
			}
		}
	}
	return currentContext, "default"
}

// KubernetesState reads the current context and effective namespace directly
// from KUBECONFIG without invoking kubectl or contacting the cluster.
func KubernetesState() (string, string) {
	return readKubeState()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" && value != "(unset)" {
			return value
		}
	}
	return ""
}

func displayNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
