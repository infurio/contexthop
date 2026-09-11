package validation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/infurio/contexthop/internal/kubetarget"
	"github.com/infurio/contexthop/internal/resolver"
)

type gkeCluster struct {
	Name       string `json:"name"`
	Location   string `json:"location"`
	Zone       string `json:"zone"`
	SelfLink   string `json:"selfLink"`
	Endpoint   string `json:"endpoint"`
	MasterAuth struct {
		ClusterCA string `json:"clusterCaCertificate"`
	} `json:"masterAuth"`
	PrivateClusterConfig struct {
		PrivateEndpoint string `json:"privateEndpoint"`
	} `json:"privateClusterConfig"`
	ControlPlaneEndpointsConfig struct {
		DNSEndpointConfig struct {
			Endpoint string `json:"endpoint"`
		} `json:"dnsEndpointConfig"`
		IPEndpointsConfig struct {
			PublicEndpoint  string `json:"publicEndpoint"`
			PrivateEndpoint string `json:"privateEndpoint"`
		} `json:"ipEndpointsConfig"`
	} `json:"controlPlaneEndpointsConfig"`
}

func validateGKEControlPlane(ctx context.Context, resolved resolver.Resolved, environment []string, staged kubetarget.Fingerprint) error {
	if resolved.Identity == nil || resolved.Project == nil || resolved.Kubernetes == nil {
		return &Error{
			Check: CheckFingerprint, Kind: FailureTargetMismatch, Subject: "GKE control plane",
			Cause: errors.New("identity, project, and cluster coordinates are required"),
		}
	}
	target := resolved.Kubernetes
	output, err := run(ctx, environment, "gcloud", "container", "clusters", "describe", target.Cluster,
		"--project", resolved.Project.ProjectID, "--location", target.Location,
		"--account", resolved.Identity.Account, "--format=json", "--quiet")
	if err != nil {
		return classifiedError(CheckFingerprint, fmt.Sprintf("GKE cluster %q", target.Cluster), "provider identity", err)
	}
	var provider gkeCluster
	if err := json.Unmarshal([]byte(output), &provider); err != nil {
		return &Error{Check: CheckFingerprint, Kind: FailureInvalidResponse, Subject: "GKE provider response", Cause: err}
	}
	location := provider.Location
	if location == "" {
		location = provider.Zone
	}
	if provider.Name != target.Cluster || location != target.Location {
		return &Error{
			Check: CheckFingerprint, Kind: FailureTargetMismatch, Subject: "GKE provider identity",
			Cause: fmt.Errorf("expected cluster %q in %q, observed %q in %q", target.Cluster, target.Location, provider.Name, location),
		}
	}
	projectPath := "/projects/" + resolved.Project.ProjectID + "/"
	if provider.SelfLink != "" && !strings.Contains(provider.SelfLink, projectPath) {
		return &Error{
			Check: CheckFingerprint, Kind: FailureTargetMismatch, Subject: "GKE provider identity",
			Cause: fmt.Errorf("provider response does not belong to project %q", resolved.Project.ProjectID),
		}
	}
	encodedCA := strings.Join(strings.Fields(provider.MasterAuth.ClusterCA), "")
	if encodedCA == "" {
		return &Error{Check: CheckFingerprint, Kind: FailureInvalidResponse, Subject: "GKE provider response", Cause: errors.New("cluster CA is missing")}
	}
	ca, err := base64.StdEncoding.Strict().DecodeString(encodedCA)
	if err != nil {
		return &Error{Check: CheckFingerprint, Kind: FailureInvalidResponse, Subject: "GKE provider response", Cause: errors.New("cluster CA is invalid")}
	}
	candidates := gkeProviderEndpoints(provider, ca)
	endpointMatched := false
	for _, candidate := range candidates {
		endpoint := strings.TrimSpace(candidate.Endpoint)
		if endpoint == "" {
			continue
		}
		if !strings.Contains(endpoint, "://") {
			endpoint = "https://" + endpoint
		}
		providerFingerprint, fingerprintErr := kubetarget.FingerprintCluster(endpoint, candidate.CA, false)
		if fingerprintErr != nil {
			continue
		}
		if providerFingerprint.Endpoint != staged.Endpoint {
			continue
		}
		endpointMatched = true
		if providerFingerprint.Digest == staged.Digest {
			return nil
		}
	}
	if !endpointMatched {
		return &Error{
			Check: CheckFingerprint, Kind: FailureTargetMismatch, Subject: "GKE control plane",
			Cause: fmt.Errorf("staged endpoint %q does not match the provider control plane", staged.Endpoint),
		}
	}
	return &Error{
		Check: CheckFingerprint, Kind: FailureTargetMismatch, Subject: "GKE control plane",
		Cause: errors.New("staged certificate authority does not match the provider control plane; refresh the Kubernetes credentials before retrying"),
	}
}

type gkeEndpoint struct {
	Endpoint string
	CA       []byte
}

func gkeProviderEndpoints(provider gkeCluster, clusterCA []byte) []gkeEndpoint {
	return []gkeEndpoint{
		{Endpoint: provider.Endpoint, CA: clusterCA},
		{Endpoint: provider.PrivateClusterConfig.PrivateEndpoint, CA: clusterCA},
		// GKE's DNS endpoint is served through Google Front End using public
		// roots, so its kubeconfig intentionally omits the cluster CA.
		{Endpoint: provider.ControlPlaneEndpointsConfig.DNSEndpointConfig.Endpoint},
		{Endpoint: provider.ControlPlaneEndpointsConfig.IPEndpointsConfig.PublicEndpoint, CA: clusterCA},
		{Endpoint: provider.ControlPlaneEndpointsConfig.IPEndpointsConfig.PrivateEndpoint, CA: clusterCA},
	}
}
