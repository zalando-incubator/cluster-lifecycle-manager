package reconciler

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// memClient is an in-memory stand-in for the API server. It merges applied
// objects over live ones, like server-side apply does for fields owned by
// other managers, and uses the real comparison to tell what changed.
type memClient struct {
	objs   map[string]*unstructured.Unstructured
	failOn string
}

func newMemClient() *memClient { return &memClient{objs: map[string]*unstructured.Unstructured{}} }

func key(gvr schema.GroupVersionResource, ns, name string) string {
	return gvr.Resource + "/" + ns + "/" + name
}

func (m *memClient) Get(_ context.Context, gvr schema.GroupVersionResource, ns, name string) (*unstructured.Unstructured, error) {
	if o, ok := m.objs[key(gvr, ns, name)]; ok {
		return o.DeepCopy(), nil
	}
	return nil, nil
}

func merge(dst, src map[string]interface{}) {
	for k, v := range src {
		if sm, ok := v.(map[string]interface{}); ok {
			if dm, ok := dst[k].(map[string]interface{}); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

func (m *memClient) Apply(ctx context.Context, obj *unstructured.Unstructured, dryRun bool) (ApplyResult, error) {
	gvr, err := gvrFor(obj)
	if err != nil {
		return ApplyResult{}, err
	}
	live, _ := m.Get(ctx, gvr, obj.GetNamespace(), obj.GetName())
	result := obj.DeepCopy()
	if live != nil {
		merged := live.DeepCopy()
		merge(merged.Object, result.Object)
		result = merged
	}
	if !dryRun {
		m.objs[key(gvr, obj.GetNamespace(), obj.GetName())] = result
	}
	return compare(live, result, dryRun), nil
}

func (m *memClient) put(gvr schema.GroupVersionResource, ns, name string, data map[string]string) {
	enc := map[string]interface{}{}
	for k, v := range data {
		enc[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	m.objs[key(gvr, ns, name)] = &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]interface{}{"name": name, "namespace": ns},
		"data":     enc,
	}}
}

func (m *memClient) has(gvr schema.GroupVersionResource, ns, name string) bool {
	_, ok := m.objs[key(gvr, ns, name)]
	return ok
}

var testRenderConfig = RenderConfig{
	Namespace:                  "flux-system",
	Name:                       "reconciler",
	RunnerServiceAccount:       "tofu-runner",
	RunnerCredentialsSecret:    "provider-credentials-secret",
	RunnerCredentialsEnv:       "PROVIDER_CREDENTIALS_FILE",
	RunnerCredentialsMountPath: "/var/run/secrets/provider",
	RunnerCredentialsFile:      "credentials.json",
	ClusterToken:               SecretRef{Namespace: "default", Name: "cluster-token", Key: "token"},
	RunnerPodApplication:       "kubernetes",
	RunnerPodComponent:         "reconciler",
}

const (
	testCA    = "-----BEGIN CERTIFICATE-----\nZmFrZQ==\n-----END CERTIFICATE-----"
	testItems = "node_count: 2\nname: poc3"
)

func testSpec(t *testing.T, extra map[string]string) *ClusterSpec {
	t.Helper()
	items := map[string]string{ConfigItemProvisionVars: testItems}
	for k, v := range extra {
		items[k] = v
	}
	c := cluster(items)
	c.Alias = "poc3"
	spec, err := ParseSpec(c)
	require.NoError(t, err)
	return spec
}

func (m *memClient) withOutputs() {
	m.put(gvrSecret, "flux-system", "tfout-poc3", map[string]string{
		OutputClusterID: "cluster-1", OutputEndpoint: "pu.example.org:443", OutputCACertificate: testCA, OutputOIDCIssuer: "https://oidc.example.org/1",
	})
}

func (m *memClient) withToken(token string) {
	m.put(gvrSecret, "default", "cluster-token", map[string]string{"token": token})
}

func actions(res *Result) map[string]Action {
	out := map[string]Action{}
	for _, o := range res.Objects {
		out[o.Kind+"/"+o.Name] = o.Action
	}
	return out
}

func TestReconcileWaitsForTheCluster(t *testing.T) {
	m := newMemClient()
	m.withToken("token-1")
	r := &Reconciler{Client: m, Config: testRenderConfig}

	res, err := r.Reconcile(context.Background(), testSpec(t, nil))
	require.NoError(t, err)
	require.Equal(t, StageProvisioning, res.Stage)
	require.Contains(t, res.Waiting, "tfout-poc3")
	require.Equal(t, map[string]Action{
		"OCIRepository/provision-poc3": ActionCreated,
		"Terraform/cluster-poc3":       ActionCreated,
		"ConfigMap/status-poc3":        ActionCreated,
	}, actions(res))

	// nothing is delivered to a cluster that does not run yet
	require.False(t, m.has(gvrSecret, "flux-system", "kubeconfig-poc3"))
	require.False(t, m.has(gvrHelmRepository, "flux-system", "delivery-poc3"))
	require.False(t, m.has(gvrHelmRelease, "flux-system", "example-chart-poc3"))

	// outputs exist, but are not complete yet
	m.put(gvrSecret, "flux-system", "tfout-poc3", map[string]string{OutputClusterID: "cluster-1"})
	res, err = r.Reconcile(context.Background(), testSpec(t, nil))
	require.NoError(t, err)
	require.Equal(t, StageProvisioning, res.Stage)
	require.Contains(t, res.Waiting, OutputEndpoint)
	require.False(t, m.has(gvrHelmRelease, "flux-system", "example-chart-poc3"))
}

func TestReconcileDeliversOnceTheClusterIsReady(t *testing.T) {
	m := newMemClient()
	m.withOutputs()
	m.withToken("token-1")
	r := &Reconciler{Client: m, Config: testRenderConfig}

	res, err := r.Reconcile(context.Background(), testSpec(t, nil))
	require.NoError(t, err)
	require.Equal(t, StageDelivering, res.Stage)
	require.Empty(t, res.Waiting)
	require.Equal(t, ActionCreated, actions(res)["Secret/kubeconfig-poc3"])
	require.Equal(t, ActionCreated, actions(res)["HelmRepository/delivery-poc3"])
	require.Equal(t, ActionCreated, actions(res)["HelmRelease/example-chart-poc3"])

	// the order is: infrastructure, kubeconfig, delivery
	var order []string
	for _, o := range res.Objects {
		order = append(order, o.Kind)
	}
	require.Equal(t, []string{"OCIRepository", "Terraform", "ConfigMap", "Secret", "HelmRepository", "HelmRelease"}, order)

	kc, _ := secretValue(m.objs[key(gvrSecret, "flux-system", "kubeconfig-poc3")], KubeconfigKey)
	require.Contains(t, kc, "server: https://pu.example.org:443")
	require.Contains(t, kc, "token: token-1")
}

func TestReconcileIsIdempotent(t *testing.T) {
	m := newMemClient()
	m.withOutputs()
	m.withToken("token-1")
	r := &Reconciler{Client: m, Config: testRenderConfig}
	spec := testSpec(t, nil)

	_, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)

	res, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, StageDelivering, res.Stage)
	for name, a := range actions(res) {
		require.Equal(t, ActionUnchanged, a, name)
	}
}

func TestReconcileChangesOnlyWhatChanged(t *testing.T) {
	m := newMemClient()
	m.withOutputs()
	m.withToken("token-1")
	r := &Reconciler{Client: m, Config: testRenderConfig}
	_, err := r.Reconcile(context.Background(), testSpec(t, nil))
	require.NoError(t, err)

	// a changed Terraform variable changes only the Terraform resource
	res, err := r.Reconcile(context.Background(), testSpec(t, map[string]string{ConfigItemProvisionVars: "node_count: 3\nname: poc3"}))
	require.NoError(t, err)
	for name, a := range actions(res) {
		if name == "Terraform/cluster-poc3" {
			require.Equal(t, ActionUpdated, a, name)
		} else {
			require.Equal(t, ActionUnchanged, a, name)
		}
	}

	// a changed Helm value changes only the HelmRelease
	res, err = r.Reconcile(context.Background(), testSpec(t, map[string]string{
		ConfigItemProvisionVars: "node_count: 3\nname: poc3", ConfigItemDeliveryValues: "replicas: 2"}))
	require.NoError(t, err)
	for name, a := range actions(res) {
		if name == "HelmRelease/example-chart-poc3" {
			require.Equal(t, ActionUpdated, a, name)
		} else {
			require.Equal(t, ActionUnchanged, a, name)
		}
	}
}

func TestReconcileFollowsTokenRenewal(t *testing.T) {
	m := newMemClient()
	m.withOutputs()
	m.withToken("token-1")
	r := &Reconciler{Client: m, Config: testRenderConfig}
	spec := testSpec(t, nil)
	_, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)

	m.withToken("token-2")
	res, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	for name, a := range actions(res) {
		if name == "Secret/kubeconfig-poc3" {
			require.Equal(t, ActionUpdated, a, name)
		} else {
			require.Equal(t, ActionUnchanged, a, name)
		}
	}
	kc, _ := secretValue(m.objs[key(gvrSecret, "flux-system", "kubeconfig-poc3")], KubeconfigKey)
	require.Contains(t, kc, "token: token-2")
}

func TestReconcileKeepsStatusData(t *testing.T) {
	m := newMemClient()
	r := &Reconciler{Client: m, Config: testRenderConfig}
	spec := testSpec(t, nil)
	_, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)

	// the status reporting owns the data of the ConfigMap
	cm := m.objs[key(gvrConfigMap, "flux-system", "status-poc3")]
	cm.Object["data"] = map[string]interface{}{"phase": "Planning"}

	res, err := r.reconcileResources(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, ActionUnchanged, actions(res)["ConfigMap/status-poc3"])
	require.Equal(t, "Planning", m.objs[key(gvrConfigMap, "flux-system", "status-poc3")].Object["data"].(map[string]interface{})["phase"])
}

func TestReconcileMissingToken(t *testing.T) {
	m := newMemClient()
	m.withOutputs()
	r := &Reconciler{Client: m, Config: testRenderConfig}

	res, err := r.Reconcile(context.Background(), testSpec(t, nil))
	require.ErrorContains(t, err, "token Secret default/cluster-token does not exist")
	require.Equal(t, StageProvisioning, res.Stage)
	require.False(t, m.has(gvrHelmRelease, "flux-system", "example-chart-poc3"))
	require.False(t, m.has(gvrSecret, "flux-system", "kubeconfig-poc3"))
}

func TestDryRunChangesNothingAndShowsDiff(t *testing.T) {
	m := newMemClient()
	m.withOutputs()
	m.withToken("token-1")
	r := &Reconciler{Client: m, Config: testRenderConfig, DryRun: true}
	spec := testSpec(t, nil)

	before := len(m.objs)
	res, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, before, len(m.objs), "dry run must not create anything")
	require.Equal(t, ActionCreated, actions(res)["Terraform/cluster-poc3"])
	for _, o := range res.Objects {
		require.NotEmpty(t, o.Diff, o.Name)
	}

	// an update shows the changed value; secret values never appear
	r.DryRun = false
	_, err = r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	r.DryRun = true
	m.withToken("token-2")
	res, err = r.Reconcile(context.Background(), testSpec(t, map[string]string{ConfigItemProvisionVars: "node_count: 3\nname: poc3"}))
	require.NoError(t, err)

	var all strings.Builder
	for _, o := range res.Objects {
		all.WriteString(o.Diff)
		if o.Kind == "Terraform" {
			require.Equal(t, ActionUpdated, o.Action)
			require.Contains(t, o.Diff, "-    value: 2")
			require.Contains(t, o.Diff, "+    value: 3")
		}
		if o.Kind == "Secret" {
			require.Equal(t, ActionUpdated, o.Action)
			require.Contains(t, o.Diff, "-  value: '<redacted: old value>'")
			require.Contains(t, o.Diff, "+  value: '<redacted: new value>'")
		}
	}
	for _, secret := range []string{"token-1", "token-2", base64.StdEncoding.EncodeToString([]byte("token-1"))} {
		require.NotContains(t, all.String(), secret)
	}
	kcLive, _ := secretValue(m.objs[key(gvrSecret, "flux-system", "kubeconfig-poc3")], KubeconfigKey)
	require.Contains(t, kcLive, "token: token-1", "dry run must not update the kubeconfig")
}

func TestNameValidation(t *testing.T) {
	r := &Reconciler{Client: newMemClient(), Config: testRenderConfig}

	long := testSpec(t, nil)
	long.Alias = strings.Repeat("a", 50)
	_, err := r.Reconcile(context.Background(), long)
	require.ErrorContains(t, err, "longer than 53 characters")

	bad := testSpec(t, nil)
	bad.Alias = "Not_Valid!"
	_, err = r.Reconcile(context.Background(), bad)
	require.ErrorContains(t, err, "alias")
}

func TestRenderedObjectsGolden(t *testing.T) {
	for _, approve := range []string{"auto", "manual"} {
		t.Run(approve, func(t *testing.T) {
			spec := testSpec(t, map[string]string{
				ConfigItemProvisionApprove:    approve,
				ConfigItemProvisionModulePath: "modules/cluster",
				ConfigItemDeliveryRelease:     "stackset",
				ConfigItemDeliveryValues:      "replicas: 1\nargs: [--interval=10s]",
			})
			c := testRenderConfig
			c.OCIPullSecret = "pull-secret"
			c.OCIProvider = "aws"
			kc, err := BuildKubeconfig(spec.Alias, "pu.example.org:443", testCA, "test-token")
			require.NoError(t, err)

			var out strings.Builder
			for _, o := range []*unstructured.Unstructured{
				c.ProvisionSource(spec), c.Terraform(spec), c.Status(spec), c.Kubeconfig(spec, kc), c.DeliverySource(spec), c.Release(spec),
			} {
				b, err := yaml.Marshal(o.Object)
				require.NoError(t, err)
				out.WriteString("---\n" + string(b))

				// every object is labelled for cleanup and carries no foreign namespace
				require.Equal(t, "reconciler", o.GetLabels()["app.kubernetes.io/managed-by"], o.GetKind())
				require.Equal(t, "poc3", o.GetLabels()["reconciler/cluster"], o.GetKind())
				require.Equal(t, "flux-system", o.GetNamespace(), o.GetKind())
			}

			path := filepath.Join("testdata", "golden", "objects-"+approve+".yaml")
			if *update {
				require.NoError(t, os.WriteFile(path, []byte(out.String()), 0o644))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, string(want), out.String())

			tf := c.Terraform(spec)
			require.Equal(t, false, tf.Object["spec"].(map[string]interface{})["destroyResourcesOnDeletion"])
			_, hasApprove := tf.Object["spec"].(map[string]interface{})["approvePlan"]
			require.Equal(t, approve == "auto", hasApprove)
		})
	}
}

func TestEncryptedValuesAreRejected(t *testing.T) {
	_, err := ParseSpec(cluster(map[string]string{
		ConfigItemDeliveryValues: "token: x",
		"some_secret":            "aws:kms:AQICAHdoNOTREAL",
	}))
	require.ErrorContains(t, err, `config item "some_secret" is encrypted (aws:kms:)`)
	require.NotContains(t, err.Error(), "AQICAHdoNOTREAL", "the value must not be echoed")
}

func TestBuildKubeconfig(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte(testCA))
	for _, tc := range []struct{ name, endpoint, ca string }{
		{"no scheme, PEM", "pu.example.org:443", testCA},
		{"scheme, base64", "https://pu.example.org:443", b64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := BuildKubeconfig("poc3", tc.endpoint, tc.ca, " token \n")
			require.NoError(t, err)
			require.Contains(t, string(b), "server: https://pu.example.org:443")
			require.Contains(t, string(b), "token: token\n")
			require.Contains(t, string(b), "certificate-authority-data: "+b64)
		})
	}
	_, err := BuildKubeconfig("poc3", "", testCA, "t")
	require.Error(t, err)
	_, err = BuildKubeconfig("poc3", "x", "not a cert", "t")
	require.ErrorContains(t, err, "CA certificate")
	_, err = BuildKubeconfig("poc3", "x", testCA, " ")
	require.ErrorContains(t, err, "token")
}
