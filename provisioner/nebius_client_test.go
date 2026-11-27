package provisioner

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nebius/gosdk/auth"
	common "github.com/nebius/gosdk/proto/nebius/common/v1"
	mk8s "github.com/nebius/gosdk/proto/nebius/mk8s/v1"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	"k8s.io/client-go/rest"
)

type fakeNebiusAPI struct {
	mu       sync.Mutex
	clusters map[string]*mk8s.Cluster
	requests []string
	tokens   []string
	err      error
	closed   int
}

func (f *fakeNebiusAPI) cluster(ctx context.Context, id string) (*mk8s.Cluster, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.requests = append(f.requests, id)
	return f.clusters[id], f.err
}

func (f *fakeNebiusAPI) BearerToken(ctx context.Context) (auth.BearerToken, error) {
	if err := ctx.Err(); err != nil {
		return auth.BearerToken{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return auth.BearerToken{}, f.err
	}
	if len(f.tokens) == 0 {
		return auth.BearerToken{}, nil
	}
	token := f.tokens[0]
	if len(f.tokens) > 1 {
		f.tokens = f.tokens[1:]
	}
	return auth.BearerToken{Token: token}, nil
}

func (f *fakeNebiusAPI) Close() error { f.closed++; return nil }

func testNebiusCluster(id, endpoint, ca string) *mk8s.Cluster {
	return &mk8s.Cluster{
		Metadata: &common.ResourceMetadata{Id: id},
		Status: &mk8s.ClusterStatus{ControlPlane: &mk8s.ControlPlaneStatus{
			Endpoints: &mk8s.ControlPlaneStatusEndpoints{PublicEndpoint: endpoint},
			Auth:      &mk8s.ControlPlaneStatusAuth{ClusterCaCertificate: ca},
		}},
	}
}

func TestNebiusRESTConfigDiscoversClusterAndRefreshesToken(t *testing.T) {
	var headers []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	sdk := &fakeNebiusAPI{clusters: map[string]*mk8s.Cluster{
		"cluster-a": testNebiusCluster("cluster-a", strings.TrimPrefix(server.URL, "https://"), string(ca)),
		"cluster-b": testNebiusCluster("cluster-b", server.URL, string(ca)),
	}, tokens: []string{"first-token", "refreshed-token"}}
	config, err := nebiusRESTConfig(context.Background(), sdk, "cluster-a")
	require.NoError(t, err)
	require.Equal(t, server.URL, config.Host)
	require.Equal(t, ca, config.CAData)
	require.False(t, config.Insecure)
	client, err := rest.HTTPClientFor(config)
	require.NoError(t, err)
	for range 2 {
		resp, err := client.Get(server.URL)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}
	require.Equal(t, []string{"Bearer first-token", "Bearer refreshed-token"}, headers)
	_, err = nebiusRESTConfig(context.Background(), sdk, "cluster-b")
	require.NoError(t, err)
	require.Equal(t, []string{"cluster-a", "cluster-b"}, sdk.requests)
}

func TestNebiusRESTConfigRejectsInvalidDiscovery(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	for _, tc := range []struct {
		name    string
		cluster *mk8s.Cluster
		message string
	}{
		{"missing cluster", nil, "different or missing ID"},
		{"wrong cluster", testNebiusCluster("other", server.URL, ca), "different or missing ID"},
		{"missing endpoint", testNebiusCluster("id", "", ca), "no public Kubernetes endpoint"},
		{"http endpoint", testNebiusCluster("id", "http://example.com", ca), "invalid public Kubernetes endpoint"},
		{"endpoint credentials", testNebiusCluster("id", "https://user:password@example.com", ca), "invalid public Kubernetes endpoint"},
		{"endpoint path", testNebiusCluster("id", "https://example.com/path", ca), "invalid public Kubernetes endpoint"},
		{"missing CA", testNebiusCluster("id", server.URL, ""), "no valid Kubernetes CA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sdk := &fakeNebiusAPI{clusters: map[string]*mk8s.Cluster{"id": tc.cluster}}
			_, err := nebiusRESTConfig(context.Background(), sdk, "id")
			require.ErrorContains(t, err, tc.message)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := nebiusRESTConfig(ctx, &fakeNebiusAPI{}, "id")
	require.ErrorIs(t, err, context.Canceled)
}

func TestNebiusCredentialsFileSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	err := validateNebiusCredentialsFile(path)
	require.ErrorContains(t, err, "credentials file")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0600))
	require.NoError(t, validateNebiusCredentialsFile(path))
	err = validateNebiusCredentialsFile(t.TempDir())
	require.ErrorContains(t, err, "not a regular file")
}

func TestNebiusClientFallbackAndMissingExplicitCredentials(t *testing.T) {
	writeTestKubeconfig(t, `apiVersion: v1
kind: Config
current-context: test
clusters:
- name: test
  cluster:
    server: https://example.com
contexts:
- name: test
  context:
    cluster: test
`)
	cluster := &api.Cluster{ID: "cluster-id"}
	path := filepath.Join(t.TempDir(), "missing.json")
	p := NewNebiusProvisioner(nil, "", nil, path, false).(*NebiusProvisioner)
	_, err := p.apply.newClient(context.Background(), cluster)
	require.ErrorContains(t, err, "credentials file")

	p = NewNebiusProvisioner(nil, "", nil, "", false).(*NebiusProvisioner)
	client, err := p.apply.newClient(context.Background(), cluster)
	require.NoError(t, err)
	_, ok := client.(*kubectlClient)
	require.True(t, ok)
}

func TestNebiusClientClosesSDK(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	sdk := &fakeNebiusAPI{clusters: map[string]*mk8s.Cluster{"id": testNebiusCluster("id", server.URL, ca)}}
	factory := func(_ context.Context, path string) (nebiusAPI, error) {
		require.Equal(t, "credentials.json", path)
		return sdk, nil
	}
	client, err := newNebiusKubernetesClientWithSDK(context.Background(), &api.Cluster{ID: "id"}, "credentials.json", factory)
	require.NoError(t, err)
	require.NoError(t, client.(io.Closer).Close())
	require.Equal(t, 1, sdk.closed)

	sdk.closed = 0
	_, err = newNebiusKubernetesClientWithSDK(context.Background(), &api.Cluster{ID: "unknown"}, "credentials.json", factory)
	require.Error(t, err)
	require.Equal(t, 1, sdk.closed)
}

func TestNebiusKubernetesClientUsesDiscoveredServer(t *testing.T) {
	var requests []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			fmt.Fprint(w, `{"kind":"APIVersions","apiVersion":"v1","versions":["v1"]}`)
		case "/apis":
			fmt.Fprint(w, `{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`)
		case "/api/v1":
			fmt.Fprint(w, `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[{"name":"configmaps","singularName":"configmap","namespaced":true,"kind":"ConfigMap","verbs":["get","delete"]}]}`)
		case "/api/v1/namespaces/default/configmaps/old":
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Success"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	sdk := &fakeNebiusAPI{clusters: map[string]*mk8s.Cluster{"cluster-id": testNebiusCluster("cluster-id", server.URL, ca)}, tokens: []string{"iam-token"}}
	client, err := newNebiusKubernetesClientWithSDK(context.Background(), &api.Cluster{ID: "cluster-id", APIServerURL: "https://wrong.example.com"}, "credentials.json", func(context.Context, string) (nebiusAPI, error) { return sdk, nil })
	require.NoError(t, err)
	defer client.(io.Closer).Close()
	manifests, err := kubernetes.ParseResourceManifest("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n", "test.yaml")
	require.NoError(t, err)
	require.Error(t, client.Apply(context.Background(), manifests[0]))
	require.NoError(t, client.DeleteResource(context.Background(), log.NewEntry(log.New()), &kubernetes.Resource{Kind: "ConfigMap", Namespace: "default", Name: "old"}))
	require.Contains(t, requests, "GET /openapi/v2 Bearer iam-token")
	require.Contains(t, requests, "DELETE /api/v1/namespaces/default/configmaps/old Bearer iam-token")
}

type closeTrackingBody struct{ closed bool }

func (b *closeTrackingBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *closeTrackingBody) Close() error             { b.closed = true; return nil }

func TestNebiusTokenTransportErrorsCloseBody(t *testing.T) {
	for _, failure := range []error{errors.New("token unavailable"), nil} {
		body := &closeTrackingBody{}
		req, err := http.NewRequest(http.MethodPost, "https://example.org", body)
		require.NoError(t, err)
		transport := &nebiusTokenTransport{base: http.DefaultTransport, sdk: &fakeNebiusAPI{err: failure}, host: "example.org"}
		_, err = transport.RoundTrip(req)
		require.Error(t, err)
		require.True(t, body.closed, fmt.Sprint(failure))
	}
}

func TestNebiusTokenTransportRejectsOtherEndpoints(t *testing.T) {
	for _, endpoint := range []string{"https://other.example.org/", "http://example.org/", "https://user@example.org/"} {
		t.Run(endpoint, func(t *testing.T) {
			body := &closeTrackingBody{}
			req, err := http.NewRequest(http.MethodPost, endpoint, body)
			require.NoError(t, err)
			transport := &nebiusTokenTransport{base: http.DefaultTransport, sdk: &fakeNebiusAPI{tokens: []string{"token"}}, host: "example.org"}
			_, err = transport.RoundTrip(req)
			require.ErrorContains(t, err, "different endpoint")
			require.True(t, body.closed)
		})
	}
}
