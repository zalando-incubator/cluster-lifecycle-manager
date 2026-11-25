package reconciler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

func loadFixture(t *testing.T) (selected []string, skipped map[string]Skipped) {
	t.Helper()
	clusters, err := registry.NewFileRegistry("testdata/clusters.yaml").ListClusters(registry.Filter{})
	require.NoError(t, err)

	sel, skp := Select(clusters, []string{"zalando-nebius"}, "reconciler")
	for _, c := range sel {
		selected = append(selected, c.Alias)
	}
	skipped = map[string]Skipped{}
	for _, s := range skp {
		skipped[s.Alias] = s
	}
	return selected, skipped
}

func TestSelect(t *testing.T) {
	selected, skipped := loadFixture(t)

	require.Equal(t, []string{"new-cluster", "broken-cluster"}, selected)

	require.Len(t, skipped, 3)
	// a cluster of another provider is skipped quietly, even if enabled
	require.True(t, skipped["aws-cluster"].OtherProvider)
	// a cluster of a handled provider without the item is skipped and reported
	require.False(t, skipped["existing-poc"].OtherProvider)
	require.Contains(t, skipped["existing-poc"].Reason, `"managed_by"`)
	// a cluster managed by another component is skipped
	require.False(t, skipped["clm-managed-cluster"].OtherProvider)
}

func TestSelectOnlyClustersManagedByThisComponent(t *testing.T) {
	clusters, err := registry.NewFileRegistry("testdata/clusters.yaml").ListClusters(registry.Filter{})
	require.NoError(t, err)
	for _, c := range clusters {
		if c.Alias == "existing-poc" {
			for _, value := range []string{"", "clm", "Reconciler", "reconciler-2"} {
				c.ConfigItems[api.ManagedByConfigItem] = value
				sel, _ := Select(clusters, []string{"zalando-nebius"}, "reconciler")
				for _, s := range sel {
					require.NotEqual(t, "existing-poc", s.Alias, "value %q", value)
				}
			}
		}
	}
}

func TestParseSpec(t *testing.T) {
	clusters, err := registry.NewFileRegistry("testdata/clusters.yaml").ListClusters(registry.Filter{})
	require.NoError(t, err)

	byAlias := map[string]int{}
	for i, c := range clusters {
		byAlias[c.Alias] = i
	}

	spec, err := ParseSpec(clusters[byAlias["new-cluster"]])
	require.NoError(t, err)
	require.Equal(t, &ClusterSpec{
		Alias:                   "new-cluster",
		ID:                      "nebius:new-cluster",
		ProvisionModuleRepo:     "oci://registry.example.org/modules/cluster",
		ProvisionModuleRef:      "v1",
		ProvisionModulePath:     "modules/cluster",
		ProvisionApprove:        "auto",
		ProvisionVars:           map[string]interface{}{"cluster_name": "new-cluster", "node_count": float64(2)},
		DeliveryRepoURL:         "oci://registry.example.org/charts",
		DeliveryRepoType:        "oci",
		DeliveryChart:           "example-chart",
		DeliveryChartVersion:    "1.2.3",
		DeliveryRelease:         "example-chart",
		DeliveryTargetNamespace: "example",
		DeliveryValues:          map[string]interface{}{"replicas": float64(1)},
	}, spec)

	_, err = ParseSpec(clusters[byAlias["broken-cluster"]])
	require.Error(t, err)
	// all problems are reported at once
	for _, want := range []string{
		`missing required config item "provision_module_repo"`,
		`missing required config item "delivery_chart"`,
		`config item "provision_vars"`,
	} {
		require.Contains(t, err.Error(), want)
	}
}
