package provisioner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
)

type recordingApplier struct {
	manifests []*kubernetes.ResourceManifest
	err       error
	events    *[]string
}

type failingDecrypter struct{ err error }

func (d failingDecrypter) Decrypt(context.Context, string) (string, error) { return "", d.err }

func testApplyProvisioner(secretDecrypter decrypter.Decrypter) *kubernetesApplyProvisioner {
	return newKubernetesApplyProvisioner(api.ZalandoNebiusProvider, nil, secretDecrypter, false)
}

func (a *recordingApplier) Apply(_ context.Context, manifest *kubernetes.ResourceManifest) error {
	a.manifests = append(a.manifests, manifest)
	if a.events != nil {
		*a.events = append(*a.events, "apply:"+manifest.Name)
	}
	return a.err
}

type recordingDeleter struct {
	resources []*kubernetes.Resource
	events    *[]string
	err       error
	failName  string
}

type recordingClient struct {
	*recordingApplier
	*recordingDeleter
}

func (d *recordingDeleter) DeleteResource(_ context.Context, _ *log.Entry, resource *kubernetes.Resource) error {
	d.resources = append(d.resources, resource)
	*d.events = append(*d.events, "delete:"+resource.Name)
	if d.failName == "" || d.failName == resource.Name {
		return d.err
	}
	return nil
}

func applyTestConfig(t *testing.T, deletions ...string) *channel.SimpleConfig {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"cluster/config-defaults.yaml":            "name: default\noverride: default\n",
		"cluster/manifests/01-first/config.yaml":  "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: '{{.Cluster.ConfigItems.name}}'\n  namespace: default\n",
		"cluster/manifests/02-second/config.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: '{{.Cluster.ConfigItems.override}}'\n  namespace: default\n",
		// CloudFormation deletions must not be parsed or executed.
		"cluster/cf-manifests/cf-deletions.yaml": "not: [valid",
	}
	if len(deletions) > 0 {
		files["cluster/manifests/deletions.yaml"] = deletions[0]
	}
	for name, contents := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0644))
	}
	config, err := channel.NewSimpleConfig("test", dir, false)
	require.NoError(t, err)
	return config
}

const testApplyDeletions = `
pre_apply:
- name: '{{.Cluster.ConfigItems.name}}-pre'
  namespace: default
  kind: ConfigMap
post_apply:
- name: '{{.Cluster.ConfigItems.override}}-post'
  namespace: default
  kind: ConfigMap
`

func TestKubernetesApplyDeletions(t *testing.T) {
	config := applyTestConfig(t, testApplyDeletions)
	logger := log.NewEntry(log.New())
	cluster := func() *api.Cluster { return &api.Cluster{Provider: api.ZalandoNebiusProvider} }
	p := testApplyProvisioner(nil)
	var events []string
	a := &recordingApplier{events: &events}
	d := &recordingDeleter{events: &events}
	p.newClient = func(*api.Cluster) (kubernetesApplyClient, error) { return &recordingClient{a, d}, nil }
	require.NoError(t, p.Provision(context.Background(), logger, cluster(), config))
	require.Equal(t, []string{"delete:default-pre", "apply:default", "apply:default", "delete:default-post"}, events)
	require.Equal(t, "default", d.resources[0].Namespace)

	failure := errors.New("delete failed")
	events = nil
	d.err = failure
	require.ErrorIs(t, p.Provision(context.Background(), logger, cluster(), config), failure)
	require.Equal(t, []string{"delete:default-pre"}, events)

	events = nil
	d.failName = "default-post"
	require.ErrorIs(t, p.Provision(context.Background(), logger, cluster(), config), failure)
	require.Equal(t, []string{"delete:default-pre", "apply:default", "apply:default", "delete:default-post"}, events)

	events = nil
	d.err = nil
	a.err = errors.New("apply failed")
	require.ErrorIs(t, p.Provision(context.Background(), logger, cluster(), config), a.err)
	require.Equal(t, []string{"delete:default-pre", "apply:default"}, events)

	p.dryRun = true
	p.newClient = func(*api.Cluster) (kubernetesApplyClient, error) {
		t.Fatal("created client in dry run")
		return nil, nil
	}
	require.NoError(t, p.Provision(context.Background(), logger, cluster(), config))
}

func TestKubernetesApplyInvalidDeletions(t *testing.T) {
	config := applyTestConfig(t, "pre_apply: [invalid")
	p := testApplyProvisioner(nil)
	p.newClient = func(*api.Cluster) (kubernetesApplyClient, error) {
		t.Fatal("created client before parsing deletions")
		return nil, nil
	}
	require.ErrorContains(t, p.Provision(context.Background(), log.NewEntry(log.New()), &api.Cluster{Provider: api.ZalandoNebiusProvider}, config), "deletions.yaml")
}

func TestKubernetesApplyProvisioner(t *testing.T) {
	config := applyTestConfig(t)
	cluster := &api.Cluster{
		Provider: api.ZalandoNebiusProvider, InfrastructureAccount: "other:account",
		APIServerURL: "https://example.com", ConfigItems: map[string]string{"override": "configured"},
		NodePools: []*api.NodePool{{ConfigItems: map[string]string{"name": "pool"}}},
	}
	p := testApplyProvisioner(decrypter.SecretDecrypter{})
	a := &recordingApplier{}
	p.newClient = func(c *api.Cluster) (kubernetesApplyClient, error) {
		require.Same(t, cluster, c)
		return &recordingClient{recordingApplier: a}, nil
	}
	require.NoError(t, p.Provision(context.Background(), log.NewEntry(log.New()), cluster, config))
	require.Len(t, a.manifests, 2)
	require.Equal(t, "default", a.manifests[0].Name)
	require.Equal(t, "configured", a.manifests[1].Name)
	require.Equal(t, "pool", cluster.NodePools[0].ConfigItems["name"])
	require.Equal(t, "configured", cluster.NodePools[0].ConfigItems["override"])
}

func TestKubernetesApplyErrorsAndDryRun(t *testing.T) {
	config := applyTestConfig(t)
	logger := log.NewEntry(log.New())
	newCluster := func() *api.Cluster { return &api.Cluster{Provider: api.ZalandoNebiusProvider} }
	p := testApplyProvisioner(decrypter.SecretDecrypter{})
	other := &api.Cluster{Provider: api.ZalandoAWSProvider}
	require.False(t, p.Supports(other))
	require.ErrorIs(t, p.Provision(context.Background(), logger, other, config), ErrProviderNotSupported)
	require.ErrorIs(t, p.Decommission(context.Background(), logger, other), ErrProviderNotSupported)
	require.ErrorContains(t, p.Decommission(context.Background(), logger, newCluster()), "does not support decommissioning")

	failure := errors.New("apply error")
	a := &recordingApplier{err: failure}
	p.newClient = func(*api.Cluster) (kubernetesApplyClient, error) { return &recordingClient{recordingApplier: a}, nil }
	err := p.Provision(context.Background(), logger, newCluster(), config)
	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, "01-first/config.yaml")
	require.Len(t, a.manifests, 1)

	p.dryRun = true
	p.newClient = func(*api.Cluster) (kubernetesApplyClient, error) {
		t.Fatal("created client in dry run")
		return nil, nil
	}
	require.NoError(t, p.Provision(context.Background(), logger, newCluster(), config))
}

func TestKubernetesApplyPreparationErrors(t *testing.T) {
	config := applyTestConfig(t)
	logger := log.NewEntry(log.New())
	failure := errors.New("failed")
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider}
	p := testApplyProvisioner(failingDecrypter{failure})
	p.newClient = func(*api.Cluster) (kubernetesApplyClient, error) {
		t.Fatal("created client before decrypting")
		return nil, nil
	}
	require.ErrorIs(t, p.Provision(context.Background(), logger, cluster, config), failure)

	p.secretDecrypter = decrypter.SecretDecrypter{}
	p.newClient = func(*api.Cluster) (kubernetesApplyClient, error) { return nil, failure }
	require.ErrorIs(t, p.Provision(context.Background(), logger, cluster, config), failure)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &recordingApplier{}
	p.newClient = func(*api.Cluster) (kubernetesApplyClient, error) { return &recordingClient{recordingApplier: a}, nil }
	require.ErrorIs(t, p.Provision(ctx, logger, cluster, config), context.Canceled)
	require.Empty(t, a.manifests)
}

func TestKubernetesApplyClientFactoryErrors(t *testing.T) {
	config := applyTestConfig(t)
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider}
	logger := log.NewEntry(log.New())
	p := testApplyProvisioner(nil)
	require.ErrorContains(t, p.Provision(context.Background(), logger, cluster, config), "no Kubernetes client factory")
	p.newClient = func(*api.Cluster) (kubernetesApplyClient, error) { return nil, nil }
	require.ErrorContains(t, p.Provision(context.Background(), logger, cluster, config), "client factory returned nil")
}

func TestKubernetesApplyWithAnotherProvider(t *testing.T) {
	cluster := &api.Cluster{Provider: api.ProviderID("future-provider")}
	var events []string
	p := newKubernetesApplyProvisioner(cluster.Provider, func(got *api.Cluster) (kubernetesApplyClient, error) {
		require.Same(t, cluster, got)
		return &recordingClient{&recordingApplier{events: &events}, &recordingDeleter{events: &events}}, nil
	}, nil, false)
	require.NoError(t, p.Provision(context.Background(), log.NewEntry(log.New()), cluster, applyTestConfig(t, testApplyDeletions)))
	require.Equal(t, []string{"delete:default-pre", "apply:default", "apply:default", "delete:default-post"}, events)
}

func TestNebiusProvisioner(t *testing.T) {
	logger := log.NewEntry(log.New())
	config := applyTestConfig(t, testApplyDeletions)
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "nebius-test"}
	other := &api.Cluster{Provider: api.ProviderID("kubernetes-apply")}
	p := NewNebiusProvisioner(nil, false).(*NebiusProvisioner)
	require.True(t, p.Supports(cluster))
	require.False(t, p.Supports(other))
	require.ErrorIs(t, p.Provision(context.Background(), logger, other, config), ErrProviderNotSupported)
	require.ErrorIs(t, p.Decommission(context.Background(), logger, other), ErrProviderNotSupported)
	require.ErrorContains(t, p.Decommission(context.Background(), logger, cluster), "zalando-nebius does not support decommissioning")

	var events []string
	calls := 0
	p.apply.newClient = func(got *api.Cluster) (kubernetesApplyClient, error) {
		calls++
		require.Same(t, cluster, got)
		require.Equal(t, api.ZalandoNebiusProvider, got.Provider)
		return &recordingClient{&recordingApplier{events: &events}, &recordingDeleter{events: &events}}, nil
	}
	require.NoError(t, p.Provision(context.Background(), logger, cluster, config))
	require.Equal(t, 1, calls)
	require.Equal(t, []string{"delete:default-pre", "apply:default", "apply:default", "delete:default-post"}, events)

	p.apply.dryRun = true
	require.NoError(t, p.Provision(context.Background(), logger, cluster, config))
	require.Equal(t, 1, calls)
}

func writeTestKubeconfig(t *testing.T, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(file, []byte(content), 0600))
	t.Setenv("KUBECONFIG", file)
	return file
}

func TestKubernetesApplyCurrentContext(t *testing.T) {
	writeTestKubeconfig(t, `apiVersion: v1
kind: Config
current-context: selected
clusters:
- name: first
  cluster:
    server: https://wrong.example.com
- name: second
  cluster:
    server: https://selected.example.com
    insecure-skip-tls-verify: true
users:
- name: second-user
  user:
    token: context-token
contexts:
- name: selected
  context:
    cluster: second
    user: second-user
`)
	cfg, name, err := currentKubeConfig()
	require.NoError(t, err)
	require.Equal(t, "selected", name)
	require.Equal(t, "https://selected.example.com", cfg.Host)
	require.Equal(t, "context-token", cfg.BearerToken)
	require.True(t, cfg.Insecure)
	p := NewNebiusProvisioner(nil, false).(*NebiusProvisioner)
	var events []string
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, APIServerURL: "https://wrong.example.com"}
	p.apply.newClient = func(c *api.Cluster) (kubernetesApplyClient, error) {
		require.Same(t, cluster, c)
		return &recordingClient{&recordingApplier{events: &events}, &recordingDeleter{events: &events}}, nil
	}
	require.NoError(t, p.Provision(context.Background(), log.NewEntry(log.New()), cluster, applyTestConfig(t, testApplyDeletions)))
	require.Equal(t, []string{"delete:default-pre", "apply:default", "apply:default", "delete:default-post"}, events)
}

func TestKubernetesApplyKubeconfigErrors(t *testing.T) {
	for _, tc := range []struct{ name, config, want string }{
		{"empty", "apiVersion: v1\nkind: Config\n", "no current context"},
		{"missing context", "apiVersion: v1\nkind: Config\ncurrent-context: missing\n", "current context \"missing\" not found"},
		{"missing server", "apiVersion: v1\nkind: Config\ncurrent-context: ctx\ncontexts:\n- name: ctx\n  context:\n    cluster: missing\n", "has no cluster server"},
		{"invalid", "invalid: [", "load kubeconfig"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeTestKubeconfig(t, tc.config)
			p := NewNebiusProvisioner(nil, false)
			err := p.Provision(context.Background(), log.NewEntry(log.New()), &api.Cluster{Provider: api.ZalandoNebiusProvider}, applyTestConfig(t))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestKubernetesApplyMergedKubeconfig(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ca.pem"), []byte("test CA"), 0600))
	require.NoError(t, os.WriteFile(first, []byte(`apiVersion: v1
kind: Config
current-context: merged
contexts:
- name: merged
  context:
    cluster: shared
    user: shared-user
`), 0600))
	require.NoError(t, os.WriteFile(second, []byte(`apiVersion: v1
kind: Config
clusters:
- name: shared
  cluster:
    server: https://merged.example.com
    certificate-authority: ca.pem
users:
- name: shared-user
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: credential-plugin
      interactiveMode: Never
`), 0600))
	t.Setenv("KUBECONFIG", first+string(os.PathListSeparator)+second)
	cfg, name, err := currentKubeConfig()
	require.NoError(t, err)
	require.Equal(t, "merged", name)
	require.Equal(t, "https://merged.example.com", cfg.Host)
	require.Equal(t, filepath.Join(dir, "ca.pem"), cfg.CAFile)
	require.NotNil(t, cfg.ExecProvider)
	require.Equal(t, "credential-plugin", cfg.ExecProvider.Command)
}

func TestKubernetesApplyRealClientsUseContext(t *testing.T) {
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
	writeTestKubeconfig(t, fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: test
clusters:
- name: test
  cluster:
    server: %s
    insecure-skip-tls-verify: true
users:
- name: test
  user:
    token: context-token
contexts:
- name: test
  context:
    cluster: test
    user: test
`, server.URL))
	client, err := newCurrentContextKubernetesClient(&api.Cluster{Provider: api.ZalandoNebiusProvider})
	require.NoError(t, err)
	manifests, err := kubernetes.ParseResourceManifest("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n", "test.yaml")
	require.NoError(t, err)
	require.Error(t, client.Apply(context.Background(), manifests[0]))
	require.NoError(t, client.DeleteResource(context.Background(), log.NewEntry(log.New()), &kubernetes.Resource{Kind: "ConfigMap", Namespace: "default", Name: "old"}))
	require.Contains(t, requests, "GET /openapi/v2 Bearer context-token")
	require.Contains(t, requests, "DELETE /api/v1/namespaces/default/configmaps/old Bearer context-token")
}
