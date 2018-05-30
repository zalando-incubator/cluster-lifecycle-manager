package provisioner

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func exampleCluster(pools []*api.NodePool) *api.Cluster {
	return &api.Cluster{
		ConfigItems: map[string]string{
			"autoscaling_buffer_pools":           "worker",
			"autoscaling_buffer_cpu_scale":       "0.75",
			"autoscaling_buffer_memory_scale":    "0.75",
			"autoscaling_buffer_cpu_reserved":    "1200m",
			"autoscaling_buffer_memory_reserved": "3500Mi",
		},
		NodePools: pools,
	}
}

func render(t *testing.T, templates map[string]string, templateName string, data interface{}) (string, error) {
	basedir, err := ioutil.TempDir(os.TempDir(), t.Name())
	require.NoError(t, err, "unable to create temp dir")

	defer os.RemoveAll(basedir)

	for name, content := range templates {
		fullPath := path.Join(basedir, name)
		parentDir := path.Dir(fullPath)
		err := os.MkdirAll(parentDir, 0755)
		require.NoError(t, err, "error while creating %s", parentDir)
		err = ioutil.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err, "error while writing %s", fullPath)
	}

	context := newTemplateContext(basedir)
	return renderTemplate(context, path.Join(basedir, templateName), data)
}

func TestTemplating(t *testing.T) {
	result, err := render(
		t,
		map[string]string{"dir/foo.yaml": "foo {{ . }}"},
		"dir/foo.yaml",
		"1")

	require.NoError(t, err)
	require.EqualValues(t, "foo 1", result)
}

func TestBase64(t *testing.T) {
	result, err := render(
		t,
		map[string]string{"dir/foo.yaml": "{{ . | base64 }}"},
		"dir/foo.yaml",
		"abc123")

	require.NoError(t, err)
	require.EqualValues(t, "YWJjMTIz", result)
}

func TestManifestHash(t *testing.T) {
	result, err := render(
		t,
		map[string]string{
			"dir/config.yaml": "foo {{ . }}",
			"dir/foo.yaml":    `{{ manifestHash "config.yaml" }}`,
		},
		"dir/foo.yaml",
		"abc123")

	require.NoError(t, err)
	require.EqualValues(t, "82b883f3662dfed3357ba6c497a77684b1d84468c6aa49bf89c4f209889ddc77", result)
}

func TestManifestHashMissingFile(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"dir/foo.yaml": `{{ manifestHash "missing.yaml" }}`,
		},
		"dir/foo.yaml",
		"abc123")

	require.Error(t, err)
}

func TestManifestHashRecursiveInclude(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"dir/config.yaml": `{{ manifestHash "foo.yaml" }}`,
			"dir/foo.yaml":    `{{ manifestHash "config.yaml" }}`,
		},
		"dir/foo.yaml",
		"abc123")

	require.Error(t, err)
}

func renderAutoscaling(t *testing.T, cluster *api.Cluster) (string, error) {
	return render(
		t,
		map[string]string{
			"foo.yaml": `{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
		},
		"foo.yaml",
		cluster)
}

func TestAutoscalingBufferExplicit(t *testing.T) {
	cluster := exampleCluster([]*api.NodePool{})
	cluster.ConfigItems["autoscaling_buffer_cpu"] = "111m"
	cluster.ConfigItems["autoscaling_buffer_memory"] = "1500Mi"

	result, err := renderAutoscaling(t, cluster)

	require.NoError(t, err)
	require.EqualValues(t, "111m 1500Mi", result)
}

func TestAutoscalingBufferExplicitOnlyOne(t *testing.T) {
	cluster := exampleCluster([]*api.NodePool{})
	cluster.ConfigItems["autoscaling_buffer_cpu"] = "111m"

	_, err := renderAutoscaling(t, cluster)
	require.Error(t, err)

	delete(cluster.ConfigItems, "autoscaling_buffer_cpu")
	cluster.ConfigItems["autoscaling_buffer_memory"] = "1500Mi"

	_, err = renderAutoscaling(t, cluster)
	require.Error(t, err)
}

func TestAutoscalingBufferPoolBasedScale(t *testing.T) {
	result, err := render(
		t,
		map[string]string{
			"foo.yaml": `{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
		},
		"foo.yaml",
		exampleCluster([]*api.NodePool{
			{
				InstanceType: "m4.xlarge",
				Name:         "master-default",
			},
			{
				InstanceType: "t2.nano",
				Name:         "worker-small",
			},
			{
				// 2 vcpu / 8gb
				InstanceType: "m4.large",
				Name:         "worker-default",
			},
		}))

	require.NoError(t, err)
	require.EqualValues(t, "800m 4692Mi", result)
}

func TestAutoscalingBufferPoolBasedReserved(t *testing.T) {
	result, err := render(
		t,
		map[string]string{
			"foo.yaml": `{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
		},
		"foo.yaml",
		exampleCluster([]*api.NodePool{
			{
				// 8 vcpu / 32gb
				InstanceType: "m4.2xlarge",
				Name:         "worker-default",
			},
		}))

	require.NoError(t, err)
	require.EqualValues(t, "6 24Gi", result)
}

func TestAutoscalingBufferPoolBasedNoPools(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"foo.yaml": `{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
		},
		"foo.yaml",
		exampleCluster([]*api.NodePool{
			{
				InstanceType: "m4.xlarge",
				Name:         "master-default",
			},
			{
				InstanceType: "m4.large",
				Name:         "testing-default",
			},
		}))

	require.Error(t, err)
}

func TestAutoscalingBufferPoolBasedMismatchingType(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"foo.yaml": `{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
		},
		"foo.yaml",
		exampleCluster([]*api.NodePool{
			{
				InstanceType: "r4.large",
				Name:         "worker-one",
			},
			{
				InstanceType: "c4.xlarge",
				Name:         "worker-two",
			},
		}))

	require.Error(t, err)
}

func TestAutoscalingBufferPoolBasedInvalidSettings(t *testing.T) {
	configSets := []map[string]string{
		// missing
		{"autoscaling_buffer_cpu_scale": "0.8", "autoscaling_buffer_memory_scale": "0.8"},
		{"autoscaling_buffer_pools": "worker", "autoscaling_buffer_memory_scale": "0.8"},
		{"autoscaling_buffer_pools": "worker", "autoscaling_buffer_cpu_scale": "0.8"},
		// invalid
		{"autoscaling_buffer_pools": "[(", "autoscaling_buffer_cpu_scale": "0.8", "autoscaling_buffer_memory_scale": "0.8"},
		{"autoscaling_buffer_pools": "worker", "autoscaling_buffer_cpu_scale": "sdfsdfsdf", "autoscaling_buffer_memory_scale": "0.8"},
		{"autoscaling_buffer_pools": "worker", "autoscaling_buffer_cpu_scale": "0.8", "autoscaling_buffer_memory_scale": "fgdfgdfg"},
	}

	for _, configItems := range configSets {
		cluster := exampleCluster([]*api.NodePool{
			{
				InstanceType: "m4.large",
				Name:         "worker",
			},
		})
		cluster.ConfigItems = configItems

		_, err := render(
			t,
			map[string]string{
				"foo.yaml": `{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
			},
			"foo.yaml",
			cluster)

		assert.Error(t, err, "configItems: %s", configItems)
	}
}
