package kubetarget

import (
	"crypto/ed25519"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFingerprintBytesSelectsCurrentContextAndCluster(t *testing.T) {
	ca := testCertificate(t, 1, "selected")
	config := `apiVersion: v1
kind: Config
current-context: selected
contexts:
- name: selected
  context:
    cluster: selected-cluster
- name: ignored
  context:
    cluster: ignored-cluster
clusters:
- name: selected-cluster
  cluster:
    server: https://API.Example.COM.:443/
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(ca) + `
- name: ignored-cluster
  cluster:
    server: https://wrong.example:6443
    insecure-skip-tls-verify: true
`

	fingerprint, err := FingerprintBytes([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint.Endpoint != "https://api.example.com" {
		t.Fatalf("endpoint = %q", fingerprint.Endpoint)
	}
	if fingerprint.Version != FingerprintVersion {
		t.Fatalf("version = %d", fingerprint.Version)
	}
	if fingerprint.Trust.Mode != TrustCertificateAuthority || len(fingerprint.Trust.CertificateSHA256) != 1 {
		t.Fatalf("trust = %#v", fingerprint.Trust)
	}
	if !strings.HasPrefix(fingerprint.Digest, fingerprintPrefix) {
		t.Fatalf("digest = %q", fingerprint.Digest)
	}
}

func TestCertificatePEMOrderAndWhitespaceDoNotAffectFingerprint(t *testing.T) {
	first := testCertificate(t, 1, "first")
	second := testCertificate(t, 2, "second")
	one := append(append([]byte("\n"), first...), second...)
	two := append(append([]byte{}, second...), first...)
	two = append(two, first...)

	left, err := FingerprintCluster("https://example.test:443", one, false)
	if err != nil {
		t.Fatal(err)
	}
	right, err := FingerprintCluster("https://EXAMPLE.TEST./", two, false)
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("equivalent bundles differ: %q != %q", left.Digest, right.Digest)
	}
	if len(left.Trust.CertificateSHA256) != 2 {
		t.Fatalf("certificate hashes = %v", left.Trust.CertificateSHA256)
	}
}

func TestFingerprintDistinguishesTrustModesAndChanges(t *testing.T) {
	ca := testCertificate(t, 1, "first")
	otherCA := testCertificate(t, 2, "second")
	values := make(map[string]bool)
	for _, input := range []struct {
		server   string
		ca       []byte
		insecure bool
	}{
		{"https://example.test", nil, false},
		{"https://example.test", nil, true},
		{"https://example.test", ca, false},
		{"https://example.test", otherCA, false},
		{"https://other.test", ca, false},
	} {
		fingerprint, err := FingerprintCluster(input.server, input.ca, input.insecure)
		if err != nil {
			t.Fatal(err)
		}
		if values[fingerprint.Digest] {
			t.Fatalf("duplicate digest for %#v", input)
		}
		values[fingerprint.Digest] = true
	}
}

func TestFingerprintFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(kubeconfigDocument("work", "https://127.0.0.1:6443", nil, false)), 0o600); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := FingerprintFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint.Endpoint != "https://127.0.0.1:6443" || fingerprint.Trust.Mode != TrustSystemRoots {
		t.Fatalf("fingerprint = %#v", fingerprint)
	}
}

func TestCanonicalEndpoint(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://API.Example.COM.:443/", "https://api.example.com"},
		{"http://Example.COM:80", "http://example.com"},
		{"https://[2001:db8::1]:6443/", "https://[2001:db8::1]:6443"},
		{"https://[2001:db8::1]:443", "https://[2001:db8::1]"},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := CanonicalEndpoint(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("endpoint = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRejectsInvalidKubeconfigs(t *testing.T) {
	validCA := base64.StdEncoding.EncodeToString(testCertificate(t, 1, "ca"))
	tests := []struct {
		name string
		body string
		want string
	}{
		{"no current context", "apiVersion: v1", "no current context"},
		{"missing context", "current-context: absent", "does not exist"},
		{"duplicate context", `current-context: work
contexts:
- {name: work, context: {cluster: one}}
- {name: work, context: {cluster: two}}
`, "duplicate definitions"},
		{"missing cluster name", `current-context: work
contexts:
- {name: work, context: {}}
`, "has no cluster"},
		{"missing cluster", `current-context: work
contexts:
- {name: work, context: {cluster: absent}}
`, "does not exist"},
		{"duplicate cluster", `current-context: work
contexts:
- {name: work, context: {cluster: one}}
clusters:
- {name: one, cluster: {server: https://one.test}}
- {name: one, cluster: {server: https://two.test}}
`, "duplicate definitions"},
		{"file CA", `current-context: work
contexts:
- {name: work, context: {cluster: one}}
clusters:
- {name: one, cluster: {server: https://one.test, certificate-authority: ca.pem}}
`, "not flattened"},
		{"bad base64", kubeconfigDocumentWithCAValue(`"%%%"`), "invalid certificate-authority-data"},
		{"empty CA", kubeconfigDocumentWithCAValue(`" "`), "empty certificate-authority-data"},
		{"bad certificate", kubeconfigDocumentWithCAValue(base64.StdEncoding.EncodeToString([]byte("not a certificate"))), "invalid certificate authority"},
		{"CA and insecure", `current-context: work
contexts:
- {name: work, context: {cluster: one}}
clusters:
- name: one
  cluster:
    server: https://one.test
    certificate-authority-data: ` + validCA + `
    insecure-skip-tls-verify: true
`, "both a certificate authority and insecure TLS"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := FingerprintBytes([]byte(test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestRejectsInvalidEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"example.test:6443",
		"ftp://example.test",
		"https:///missing-host",
		"https://user@example.test",
		"https://example.test/path",
		"https://example.test?query=yes",
		"https://example.test#fragment",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := CanonicalEndpoint(endpoint); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func kubeconfigDocument(context, server string, ca []byte, insecure bool) string {
	trust := ""
	if len(ca) != 0 {
		trust = "    certificate-authority-data: " + base64.StdEncoding.EncodeToString(ca) + "\n"
	}
	if insecure {
		trust += "    insecure-skip-tls-verify: true\n"
	}
	return `apiVersion: v1
kind: Config
current-context: ` + context + `
contexts:
- name: ` + context + `
  context:
    cluster: cluster
clusters:
- name: cluster
  cluster:
    server: ` + server + "\n" + trust
}

func kubeconfigDocumentWithCAValue(value string) string {
	return `current-context: work
contexts:
- {name: work, context: {cluster: one}}
clusters:
- name: one
  cluster:
    server: https://one.test
    certificate-authority-data: ` + value + "\n"
}

func testCertificate(t *testing.T, serial int64, commonName string) []byte {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = byte(serial)
	privateKey := ed25519.NewKeyFromSeed(seed)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Unix(0, 0),
		NotAfter:              time.Unix(4102444800, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(strings.NewReader(strings.Repeat("x", 64)), template, template, privateKey.Public(), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
