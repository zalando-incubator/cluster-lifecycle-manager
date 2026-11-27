package provisioner

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/nebius/gosdk"
	"github.com/nebius/gosdk/auth"
	mk8s "github.com/nebius/gosdk/proto/nebius/mk8s/v1"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"k8s.io/client-go/rest"
)

// nebiusAPI encapsulates the SDK so discovery and token refresh can be tested independently.
type nebiusAPI interface {
	cluster(context.Context, string) (*mk8s.Cluster, error)
	BearerToken(context.Context) (auth.BearerToken, error)
	Close() error
}

type nebiusSDK struct{ *gosdk.SDK }

func (s nebiusSDK) cluster(ctx context.Context, id string) (*mk8s.Cluster, error) {
	return s.Services().MK8S().V1().Cluster().Get(ctx, &mk8s.GetClusterRequest{Id: id})
}

func newNebiusSDK(ctx context.Context, credentialsFile string) (nebiusAPI, error) {
	sdk, err := gosdk.New(ctx,
		gosdk.WithUserAgentPrefix("cluster-lifecycle-manager"),
		gosdk.WithExplicitInit(true),
		gosdk.WithCredentials(gosdk.ServiceAccountReader(auth.NewServiceAccountCredentialsFileParser(nil, credentialsFile))),
	)
	if err != nil {
		return nil, fmt.Errorf("initialize Nebius SDK: %w", err)
	}
	if err := sdk.Init(ctx); err != nil {
		_ = sdk.Close()
		return nil, fmt.Errorf("initialize Nebius SDK: %w", err)
	}
	return nebiusSDK{sdk}, nil
}

// A configured but inaccessible file must not fall back to an unrelated context.
func validateNebiusCredentialsFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("access Nebius credentials file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("nebius credentials file is not a regular file")
	}
	return nil
}

func newNebiusKubernetesClient(ctx context.Context, cluster *api.Cluster, path string) (kubernetesApplyClient, error) {
	if path == "" {
		return newCurrentContextKubernetesClient(ctx, cluster)
	}
	if err := validateNebiusCredentialsFile(path); err != nil {
		return nil, err
	}
	return newNebiusKubernetesClientWithSDK(ctx, cluster, path, newNebiusSDK)
}

func newNebiusKubernetesClientWithSDK(ctx context.Context, cluster *api.Cluster, path string, newSDK func(context.Context, string) (nebiusAPI, error)) (kubernetesApplyClient, error) {
	if cluster.ID == "" {
		return nil, fmt.Errorf("nebius cluster ID is required")
	}
	sdk, err := newSDK(ctx, path)
	if err != nil {
		return nil, err
	}
	config, err := nebiusRESTConfig(ctx, sdk, cluster.ID)
	if err != nil {
		_ = sdk.Close()
		return nil, err
	}
	client, err := newKubectlClient(config)
	if err != nil {
		_ = sdk.Close()
		return nil, fmt.Errorf("create Nebius Kubernetes client: %w", err)
	}
	return &nebiusKubernetesClient{kubectlClient: client, sdk: sdk}, nil
}

type nebiusKubernetesClient struct {
	*kubectlClient
	sdk nebiusAPI
}

func (c *nebiusKubernetesClient) Close() error { return c.sdk.Close() }

func nebiusRESTConfig(ctx context.Context, sdk nebiusAPI, id string) (*rest.Config, error) {
	cluster, err := sdk.cluster(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("discover Nebius cluster %s: %w", id, err)
	}
	if cluster.GetMetadata().GetId() != id {
		return nil, fmt.Errorf("nebius cluster discovery returned a different or missing ID for %s", id)
	}
	controlPlane := cluster.GetStatus().GetControlPlane()
	endpoint := controlPlane.GetEndpoints().GetPublicEndpoint()
	if endpoint == "" {
		return nil, fmt.Errorf("nebius cluster %s has no public Kubernetes endpoint", id)
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("nebius cluster %s has an invalid public Kubernetes endpoint", id)
	}
	ca := []byte(controlPlane.GetAuth().GetClusterCaCertificate())
	if !x509.NewCertPool().AppendCertsFromPEM(ca) {
		return nil, fmt.Errorf("nebius cluster %s has no valid Kubernetes CA certificate", id)
	}
	config := &rest.Config{Host: u.String(), TLSClientConfig: rest.TLSClientConfig{CAData: ca}}
	config.Wrap(func(base http.RoundTripper) http.RoundTripper {
		return &nebiusTokenTransport{base: base, sdk: sdk, host: u.Host}
	})
	return config, nil
}

type nebiusTokenTransport struct {
	base http.RoundTripper
	sdk  nebiusAPI
	host string
}

func (t *nebiusTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Never forward the IAM token to another host, including on redirects.
	if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Host, t.host) || req.URL.User != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, fmt.Errorf("refusing Nebius Kubernetes request to a different endpoint")
	}
	token, err := t.sdk.BearerToken(req.Context())
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, fmt.Errorf("get Nebius IAM token: %w", err)
	}
	if token.Token == "" {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, fmt.Errorf("nebius IAM token is empty")
	}
	clonedRequest := req.Clone(req.Context())
	clonedRequest.Header.Set("Authorization", "Bearer "+token.Token)
	return t.base.RoundTrip(clonedRequest)
}
