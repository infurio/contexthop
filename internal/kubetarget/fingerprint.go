// Package kubetarget identifies the Kubernetes control plane described by a
// materialized kubeconfig.
package kubetarget

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	FingerprintVersion = 1
	fingerprintPrefix  = "k8s-control-plane:v1:"
)

const (
	TrustCertificateAuthority = "certificate-authority"
	TrustSystemRoots          = "system-roots"
	TrustInsecure             = "insecure"
)

// Trust describes how the Kubernetes client authenticates the control plane.
// CertificateSHA256 contains sorted, deduplicated SHA-256 hashes of certificate
// DER. It is populated only for TrustCertificateAuthority.
type Trust struct {
	Mode              string   `json:"mode"`
	CertificateSHA256 []string `json:"certificateSha256,omitempty"`
}

// Fingerprint is the canonical identity of a materialized control plane.
type Fingerprint struct {
	Version  int    `json:"version"`
	Endpoint string `json:"endpoint"`
	Trust    Trust  `json:"trust"`
	Digest   string `json:"digest"`
}

type kubeconfig struct {
	CurrentContext string `yaml:"current-context"`
	Contexts       []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Clusters []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthority     string `yaml:"certificate-authority"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
}

// FingerprintFile fingerprints the current context in a flattened, minified
// kubeconfig file.
func FingerprintFile(path string) (Fingerprint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("read kubeconfig: %w", err)
	}
	fingerprint, err := FingerprintBytes(data)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("fingerprint kubeconfig %s: %w", path, err)
	}
	return fingerprint, nil
}

// FingerprintBytes fingerprints the current context in a flattened, minified
// kubeconfig document.
func FingerprintBytes(data []byte) (Fingerprint, error) {
	var config kubeconfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return Fingerprint{}, fmt.Errorf("decode kubeconfig: %w", err)
	}
	if config.CurrentContext == "" {
		return Fingerprint{}, fmt.Errorf("kubeconfig has no current context")
	}

	clusterName := ""
	contextMatches := 0
	for _, candidate := range config.Contexts {
		if candidate.Name == config.CurrentContext {
			contextMatches++
			clusterName = candidate.Context.Cluster
		}
	}
	if contextMatches == 0 {
		return Fingerprint{}, fmt.Errorf("current context %q does not exist", config.CurrentContext)
	}
	if contextMatches > 1 {
		return Fingerprint{}, fmt.Errorf("current context %q has duplicate definitions", config.CurrentContext)
	}
	if clusterName == "" {
		return Fingerprint{}, fmt.Errorf("current context %q has no cluster", config.CurrentContext)
	}

	clusterMatches := 0
	var selected struct {
		Server                   string `yaml:"server"`
		CertificateAuthority     string `yaml:"certificate-authority"`
		CertificateAuthorityData string `yaml:"certificate-authority-data"`
		InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
	}
	for _, candidate := range config.Clusters {
		if candidate.Name == clusterName {
			clusterMatches++
			selected = candidate.Cluster
		}
	}
	if clusterMatches == 0 {
		return Fingerprint{}, fmt.Errorf("cluster %q referenced by current context does not exist", clusterName)
	}
	if clusterMatches > 1 {
		return Fingerprint{}, fmt.Errorf("cluster %q has duplicate definitions", clusterName)
	}
	if selected.CertificateAuthority != "" {
		return Fingerprint{}, fmt.Errorf("cluster %q is not flattened: certificate-authority is a file reference", clusterName)
	}

	var caData []byte
	if selected.CertificateAuthorityData != "" {
		encoded := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, selected.CertificateAuthorityData)
		var err error
		caData, err = base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return Fingerprint{}, fmt.Errorf("cluster %q has invalid certificate-authority-data: %w", clusterName, err)
		}
		if len(caData) == 0 {
			return Fingerprint{}, fmt.Errorf("cluster %q has empty certificate-authority-data", clusterName)
		}
	}

	fingerprint, err := FingerprintCluster(selected.Server, caData, selected.InsecureSkipTLSVerify)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("cluster %q: %w", clusterName, err)
	}
	return fingerprint, nil
}

// FingerprintCluster fingerprints a server and decoded CA certificate data.
// caData may contain PEM certificates or concatenated DER certificates.
func FingerprintCluster(server string, caData []byte, insecure bool) (Fingerprint, error) {
	endpoint, err := CanonicalEndpoint(server)
	if err != nil {
		return Fingerprint{}, err
	}
	trust, err := canonicalTrust(caData, insecure)
	if err != nil {
		return Fingerprint{}, err
	}
	canonical := struct {
		Version  int      `json:"version"`
		Endpoint string   `json:"endpoint"`
		Mode     string   `json:"trustMode"`
		CAs      []string `json:"certificateSha256,omitempty"`
	}{Version: FingerprintVersion, Endpoint: endpoint, Mode: trust.Mode, CAs: trust.CertificateSHA256}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("encode canonical fingerprint: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return Fingerprint{Version: FingerprintVersion, Endpoint: endpoint, Trust: trust, Digest: fingerprintPrefix + hex.EncodeToString(digest[:])}, nil
}

// CanonicalEndpoint returns a stable spelling of a Kubernetes API server URL.
func CanonicalEndpoint(server string) (string, error) {
	parsed, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("invalid server URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("server URL must use http or https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("server URL has no host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("server URL must not contain user information, a query, or a fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" || parsed.RawPath != "" && parsed.RawPath != "/" {
		return "", fmt.Errorf("server URL must not contain a non-root path")
	}

	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if hostname == "" {
		return "", fmt.Errorf("server URL has no host")
	}
	port := parsed.Port()
	if port == "443" && parsed.Scheme == "https" || port == "80" && parsed.Scheme == "http" {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	return parsed.Scheme + "://" + host, nil
}

func canonicalTrust(caData []byte, insecure bool) (Trust, error) {
	if insecure && len(caData) != 0 {
		return Trust{}, fmt.Errorf("cluster specifies both a certificate authority and insecure TLS")
	}
	if insecure {
		return Trust{Mode: TrustInsecure}, nil
	}
	if len(caData) == 0 {
		return Trust{Mode: TrustSystemRoots}, nil
	}

	certificates, err := parseCertificates(caData)
	if err != nil {
		return Trust{}, fmt.Errorf("invalid certificate authority: %w", err)
	}
	hashes := make([]string, 0, len(certificates))
	seen := make(map[string]bool, len(certificates))
	for _, certificate := range certificates {
		digest := sha256.Sum256(certificate.Raw)
		value := hex.EncodeToString(digest[:])
		if !seen[value] {
			seen[value] = true
			hashes = append(hashes, value)
		}
	}
	sort.Strings(hashes)
	return Trust{Mode: TrustCertificateAuthority, CertificateSHA256: hashes}, nil
}

func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	remainder := data
	var certificates []*x509.Certificate
	for {
		block, rest := pem.Decode(remainder)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("unexpected PEM block %q", block.Type)
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certificates = append(certificates, certificate)
		remainder = rest
	}
	if len(certificates) > 0 {
		if strings.TrimSpace(string(remainder)) != "" {
			return nil, fmt.Errorf("data remains after PEM certificates")
		}
		return certificates, nil
	}
	certificates, err := x509.ParseCertificates(data)
	if err != nil {
		return nil, err
	}
	if len(certificates) == 0 {
		return nil, fmt.Errorf("no certificates")
	}
	return certificates, nil
}
