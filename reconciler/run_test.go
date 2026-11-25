package reconciler

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

func runConfig() *Config {
	return &Config{Providers: []string{"zalando-nebius"}, Name: "reconciler"}
}

func writeRegistry(t *testing.T, content string) registry.Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clusters.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return registry.NewFileRegistry(path)
}

const validCluster = `
clusters:
  - alias: poc3
    id: "nebius:poc3"
    provider: zalando-nebius
    api_server_url: https://placeholder.example.org
    lifecycle_status: requested
    config_items:
      managed_by: reconciler
      provision_module_repo: oci://registry.example.org/modules/cluster
      provision_module_ref: v1
      delivery_repo_url: oci://registry.example.org/charts
      delivery_chart: example-chart
      delivery_chart_version: 1.2.3
      delivery_target_namespace: example
`

func newRunner(m *memClient) *Reconciler {
	return &Reconciler{Client: m, Config: testRenderConfig, Diffs: io.Discard}
}

func TestRunOnceCountsFailures(t *testing.T) {
	m := newMemClient()
	m.withToken("token-1")
	r := newRunner(m)
	ctx := context.Background()

	// every entry is fine: no failure, even though the cluster is not ready yet
	require.Equal(t, 0, r.RunOnce(ctx, writeRegistry(t, validCluster), runConfig()))
	require.True(t, m.has(gvrTerraform, "flux-system", "cluster-poc3"))

	// an entry that cannot be parsed is a failure, and does not stop the others
	broken := validCluster + `
  - alias: broken
    id: "nebius:broken"
    provider: zalando-nebius
    api_server_url: https://placeholder.example.org
    config_items:
      managed_by: reconciler
      provision_vars: "[not, a, map]"
`
	m2 := newMemClient()
	m2.withToken("token-1")
	require.Equal(t, 1, newRunner(m2).RunOnce(ctx, writeRegistry(t, broken), runConfig()))
	require.True(t, m2.has(gvrTerraform, "flux-system", "cluster-poc3"), "the valid entry is still reconciled")
	require.False(t, m2.has(gvrTerraform, "flux-system", "cluster-broken"))

	// entries of other owners and providers are not failures
	other := `
clusters:
  - alias: owned-by-clm
    id: "nebius:clm"
    provider: zalando-nebius
    api_server_url: https://placeholder.example.org
    config_items: {}
`
	require.Equal(t, 0, newRunner(newMemClient()).RunOnce(ctx, writeRegistry(t, other), runConfig()))
}

func TestRunOnceRegistryFailure(t *testing.T) {
	r := newRunner(newMemClient())
	missing := registry.NewFileRegistry(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	require.Equal(t, 1, r.RunOnce(context.Background(), missing, runConfig()))
}

func TestRunOnceReconcileFailure(t *testing.T) {
	// the outputs exist but the token Secret does not: the delivery cannot be prepared
	m := newMemClient()
	m.withOutputs()
	require.Equal(t, 1, newRunner(m).RunOnce(context.Background(), writeRegistry(t, validCluster), runConfig()))
	require.False(t, m.has(gvrHelmRelease, "flux-system", "example-chart-poc3"))
}

func TestRunOnceDryRunFailureCount(t *testing.T) {
	m := newMemClient()
	r := newRunner(m)
	r.DryRun = true
	missing := registry.NewFileRegistry(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	require.Equal(t, 1, r.RunOnce(context.Background(), missing, runConfig()))
	require.Empty(t, m.objs)
}

func TestYAMLMapErrors(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"list", "[not, a, map]", `must be a YAML map, got a list`},
		{"multi-line list", "- a\n- b", `must be a YAML map, got a list`},
		{"scalar", "just text", `must be a YAML map, got a string`},
		{"number", "42", `must be a YAML map, got float64`},
		{"broken yaml", "a: [unclosed", `invalid YAML:`},
		{"duplicate key", "a: 1\na: 2", `invalid YAML:`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseYAMLMap(tc.value)
			require.ErrorContains(t, err, tc.want)
			require.NotContains(t, err.Error(), "JSON", "no library internals in the message")
			require.NotContains(t, err.Error(), "map[string]interface")
		})
	}
	m, err := parseYAMLMap("a: 1")
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"a": float64(1)}, m)
}

func TestSkippedEntriesAreLoggedOnceUntilTheyChange(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	log.SetLevel(log.InfoLevel)
	defer log.SetOutput(os.Stderr)

	r := newRunner(newMemClient())
	skip := []Skipped{{Alias: "a", Reason: "not mine"}, {Alias: "b", Reason: "other provider", OtherProvider: true}}

	r.logSkipped(skip)
	require.Equal(t, 1, strings.Count(out.String(), "Skipping cluster a"), "logged when it appears")
	require.NotContains(t, out.String(), "Skipping cluster b", "other providers are not logged at info level")

	out.Reset()
	r.logSkipped(skip)
	r.logSkipped(skip)
	require.Empty(t, out.String(), "no repeat while nothing changes")

	out.Reset()
	r.logSkipped([]Skipped{{Alias: "a", Reason: "now another reason"}})
	require.Equal(t, 1, strings.Count(out.String(), "Skipping cluster a: now another reason"), "logged when the reason changes")

	// it disappears and comes back: logged again
	r.logSkipped(nil)
	out.Reset()
	r.logSkipped(skip)
	require.Equal(t, 1, strings.Count(out.String(), "Skipping cluster a"))
}
