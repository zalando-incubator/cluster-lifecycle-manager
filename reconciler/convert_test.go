package reconciler

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

var update = flag.Bool("update", false, "update golden files")

func cluster(items map[string]string) *api.Cluster {
	base := map[string]string{
		ConfigItemProvisionModuleRepo:     "oci://registry.example.org/modules/cluster",
		ConfigItemProvisionModuleRef:      "v1",
		ConfigItemDeliveryRepoURL:         "oci://registry.example.org/charts",
		ConfigItemDeliveryChart:           "example-chart",
		ConfigItemDeliveryChartVersion:    "1.2.3",
		ConfigItemDeliveryTargetNamespace: "example",
	}
	for k, v := range items {
		base[k] = v
	}
	return &api.Cluster{Alias: "test-cluster", ID: "test:id", ConfigItems: base}
}

type rendered struct {
	Spec          *ClusterSpec           `json:"spec"`
	TerraformVars []interface{}          `json:"terraformVars"`
	HelmValues    map[string]interface{} `json:"helmValues"`
}

func TestConvertGolden(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items map[string]string
	}{
		{
			name:  "defaults",
			items: nil,
		},
		{
			name: "typed-vars-and-values",
			items: map[string]string{
				ConfigItemProvisionModulePath: "modules/cluster",
				ConfigItemProvisionApprove:    "manual",
				ConfigItemProvisionVars: `
node_count: 2
enabled: true
kubernetes_version: "1.30"
cluster_name: poc3
zones: [a, b]
labels: {team: teapot, tier: 1}
`,
				ConfigItemDeliveryRelease: "stackset",
				ConfigItemDeliveryValues: `
replicas: 1
args:
  - --interval=10s
resources:
  limits: {cpu: 100m}
`,
			},
		},
		{
			name:  "empty-yaml-items",
			items: map[string]string{ConfigItemProvisionVars: "  ", ConfigItemDeliveryValues: "{}"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := ParseSpec(cluster(tc.items))
			require.NoError(t, err)

			got, err := json.MarshalIndent(rendered{spec, spec.TerraformVars(), spec.HelmValues()}, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')

			path := filepath.Join("testdata", "golden", tc.name+".json")
			if *update {
				require.NoError(t, os.WriteFile(path, got, 0o644))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, string(want), string(got))
		})
	}
}

func TestConvertIsIdempotent(t *testing.T) {
	c := cluster(map[string]string{ConfigItemProvisionVars: "b: 1\na: 2\nc: 3"})
	first, err := ParseSpec(c)
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		again, err := ParseSpec(c)
		require.NoError(t, err)
		require.Equal(t, first.TerraformVars(), again.TerraformVars())
	}
	// sorted by name
	names := []string{}
	for _, v := range first.TerraformVars() {
		names = append(names, v.(map[string]interface{})["name"].(string))
	}
	require.Equal(t, []string{"a", "b", "c"}, names)
}

func TestParseSpecValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items map[string]string
		want  string
	}{
		{"invalid approve", map[string]string{ConfigItemProvisionApprove: "yes"}, `"provision_approve" must be "auto" or "manual", got "yes"`},
		{"invalid repo type", map[string]string{ConfigItemDeliveryRepoType: "https"}, `"delivery_repo_type" must be "oci", got "https"`},
		{"invalid var name", map[string]string{ConfigItemProvisionVars: "bad name: 1"}, `"bad name" is not a valid variable name`},
		{"vars not a map", map[string]string{ConfigItemProvisionVars: "- a\n- b"}, `config item "provision_vars"`},
		{"values not yaml", map[string]string{ConfigItemDeliveryValues: "a: [unclosed"}, `config item "delivery_values"`},
		{"duplicate key", map[string]string{ConfigItemProvisionVars: "a: 1\na: 2"}, `config item "provision_vars"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSpec(cluster(tc.items))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			require.Contains(t, err.Error(), "test-cluster")
		})
	}
}
