package channel

import (
	"context"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestCombinedSourceOverrides(t *testing.T) {
	main := &mockSource{
		name:          "main",
		validChannels: []string{"master"},
	}
	secondary := &mockSource{
		name:          "secondary",
		validChannels: []string{"alternate"},
	}

	combined, err := NewCombinedSource([]ConfigSource{main, secondary})
	require.NoError(t, err)

	version, err := combined.Version("master", map[string]string{"secondary": "alternate"})
	require.NoError(t, err)
	require.Equal(t, "main=master;secondary=alternate", version.ID())

	_, err = combined.Version("missing", map[string]string{"secondary": "alternate"})
	require.Error(t, err)

	_, err = combined.Version("master", map[string]string{"secondary": "missing"})
	require.Error(t, err)
}

func TestCombinedSource(t *testing.T) {
	logger := log.StandardLogger().WithFields(map[string]interface{}{})

	mainDir := createTempDir(t)
	defer os.RemoveAll(mainDir)

	secondaryDir := createTempDir(t)
	defer os.RemoveAll(secondaryDir)

	mainSrc, err := NewDirectory("main", mainDir)
	require.NoError(t, err)

	secondarySrc, err := NewDirectory("secondary", secondaryDir)
	require.NoError(t, err)

	setupConfig(
		t, mainDir,
		map[string]string{
			"cluster/manifests/example1/config.yaml":     "example1-config-main",
			"cluster/manifests/example1/deployment.yaml": "example1-deployment-main",
			"cluster/manifests/example1/unknown.swp":     "ignored",
			"cluster/manifests/example2/config.yaml":     "example2-config-main",
			"cluster/manifests/example2/deployment.yaml": "example2-deployment-main",
			"cluster/manifests/deletions.yaml":           "deletions",
			"cluster/node-pools/example/main.yaml":       "node-pool",
			"cluster/etcd/stack.yaml":       			  "etcd",
			"cluster/config-defaults.yaml":               "defaults",
			"cluster/stack.yaml":                         "stack",
		})

	setupConfig(
		t, secondaryDir,
		map[string]string{
			"cluster/manifests/example1/deployment.yaml": "example1-deployment-secondary",
			"cluster/manifests/example3/deployment.yaml": "example3-deployment-secondary",
			"cluster/manifests/deletions.yaml":           "secondary-deletions",
			"cluster/node-pools/example/deployment.yaml": "secondary-node-pool",
			"cluster/config-defaults.yaml":               "secondary-defaults",
			"cluster/stack.yaml":                         "secondary-stack",
		})

	combined, err := NewCombinedSource([]ConfigSource{mainSrc, secondarySrc})
	require.NoError(t, err)

	anyVersion, err := combined.Version("foobar", nil)
	require.NoError(t, err)

	config, err := anyVersion.Get(context.Background(), logger)
	require.NoError(t, err)

	stack, err := config.StackManifest("stack.yaml")
	require.NoError(t, err)
	require.Equal(t, expectedManifest("main", "cluster/stack.yaml", "stack"), stack)

	etcdStack, err := config.EtcdManifest("stack.yaml")
	require.NoError(t, err)
	require.Equal(t, expectedManifest("main", "cluster/etcd/stack.yaml", "etcd"), etcdStack)

	pool, err := config.NodePoolManifest("example", "main.yaml")
	require.NoError(t, err)
	require.Equal(t, expectedManifest("main", "cluster/node-pools/example/main.yaml", "node-pool"), pool)

	defaults, err := config.DefaultsManifests()
	require.NoError(t, err)
	require.Equal(t, []Manifest{
		expectedManifest("main", "cluster/config-defaults.yaml", "defaults"),
		expectedManifest("secondary", "cluster/config-defaults.yaml", "secondary-defaults"),
	}, defaults)

	deletions, err := config.DeletionsManifests()
	require.NoError(t, err)
	require.Equal(t, []Manifest{
		expectedManifest("main", "cluster/manifests/deletions.yaml", "deletions"),
		expectedManifest("secondary", "cluster/manifests/deletions.yaml", "secondary-deletions"),
	}, deletions)

	manifests, err := config.Components()
	require.NoError(t, err)
	expected := []Component{
		// From main
		{
			Name: "main/example1",
			Manifests: []Manifest{
				expectedManifest("main", "cluster/manifests/example1/config.yaml", "example1-config-main"),
				expectedManifest("main", "cluster/manifests/example1/deployment.yaml", "example1-deployment-main"),
			},
		},
		// From main
		{
			Name: "main/example2",
			Manifests: []Manifest{
				expectedManifest("main", "cluster/manifests/example2/config.yaml", "example2-config-main"),
				expectedManifest("main", "cluster/manifests/example2/deployment.yaml", "example2-deployment-main"),
			},
		},
		// From secondary, same name as in main
		{
			Name: "secondary/example1",
			Manifests: []Manifest{
				expectedManifest("secondary", "cluster/manifests/example1/deployment.yaml", "example1-deployment-secondary"),
			},
		},
		// From secondary
		{
			Name: "secondary/example3",
			Manifests: []Manifest{
				expectedManifest("secondary", "cluster/manifests/example3/deployment.yaml", "example3-deployment-secondary"),
			},
		},
	}

	require.Equal(t, expected, manifests)

	require.NoError(t, config.Delete())
}
