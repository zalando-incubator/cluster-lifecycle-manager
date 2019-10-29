package channel

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

func createTempDir(t *testing.T) string {
	res, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	return res
}

func setupExampleConfig(t *testing.T, baseDir string, mainStack string) {
	directories := []string{
		"cluster/manifests/example1",
		"cluster/manifests/example2",
		"cluster/node-pools/example",
	}
	files := map[string]string{
		"cluster/manifests/example1/main.yaml":   "example1-main",
		"cluster/manifests/example1/unknown.swp": "ignored",
		"cluster/manifests/example2/config.yaml": "example2-config",
		"cluster/manifests/example2/main.yaml":   "example2-main",
		"cluster/manifests/deletions.yaml":       "deletions",
		"cluster/node-pools/example/main.yaml":   "node-pool",
		"cluster/config-defaults.yaml":           "defaults",
		"cluster/stack.yaml":                     mainStack,
	}

	for _, directory := range directories {
		err := os.MkdirAll(path.Join(baseDir, directory), 0755)
		require.NoError(t, err)
	}

	for filename, contents := range files {
		err := ioutil.WriteFile(path.Join(baseDir, filename), []byte(contents), 0644)
		require.NoError(t, err)
	}
}

func expectedManifest(path string, contents string) Manifest {
	return Manifest{
		Path:     path,
		Contents: []byte(contents),
	}
}

func verifyExampleConfig(t *testing.T, config Config, mainStack string) {
	stack, err := config.StackManifest("stack.yaml")
	require.NoError(t, err)
	require.Equal(t, expectedManifest("cluster/stack.yaml", mainStack), stack)

	pool, err := config.NodePoolManifest("example", "main.yaml")
	require.NoError(t, err)
	require.Equal(t, expectedManifest("cluster/node-pools/example/main.yaml", "node-pool"), pool)

	defaults, err := config.DefaultsManifests()
	require.NoError(t, err)
	require.Equal(t, []Manifest{expectedManifest("cluster/config-defaults.yaml", "defaults")}, defaults)

	deletions, err := config.DeletionsManifests()
	require.NoError(t, err)
	require.Equal(t, []Manifest{expectedManifest("cluster/manifests/deletions.yaml", "deletions")}, deletions)

	manifests, err := config.Components()
	require.NoError(t, err)
	expected := []Component{
		{
			Name: "example1",
			Manifests: []Manifest{
				expectedManifest("cluster/manifests/example1/main.yaml", "example1-main"),
			},
		},
		{
			Name: "example2",
			Manifests: []Manifest{
				expectedManifest("cluster/manifests/example2/config.yaml", "example2-config"),
				expectedManifest("cluster/manifests/example2/main.yaml", "example2-main"),
			},
		},
	}
	require.Equal(t, expected, manifests)
}
