package channel

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

func createTempDir(t *testing.T) string {
	res, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	return res
}

func setupExampleConfig(t *testing.T, baseDir string, mainStack string) {
	setupConfig(
		t, baseDir,
		map[string]string{
			"cluster/manifests/example1/main.yaml":   "example1-main",
			"cluster/manifests/example1/unknown.swp": "ignored",
			"cluster/manifests/example2/config.yaml": "example2-config",
			"cluster/manifests/example2/main.yaml":   "example2-main",
			"cluster/manifests/deletions.yaml":       "deletions",
			"cluster/node-pools/example/main.yaml":   "node-pool",
			"cluster/config-defaults.yaml":           "defaults",
			"cluster/stack.yaml":                     mainStack,
		})
}

func setupConfig(t *testing.T, baseDir string, manifests map[string]string) {
	for manifestPath, contents := range manifests {
		fullpath := path.Join(baseDir, manifestPath)

		err := os.MkdirAll(path.Dir(fullpath), 0755)
		require.NoError(t, err)

		err = os.WriteFile(fullpath, []byte(contents), 0644)
		require.NoError(t, err)
	}
}

func expectedManifest(sourceName, manifestPath string, contents string) Manifest {
	return Manifest{
		Path:     path.Join(sourceName, manifestPath),
		Contents: []byte(contents),
	}
}

func verifyExampleConfig(t *testing.T, config Config, sourceName string, mainStack string) {
	stack, err := config.StackManifest("stack.yaml")
	require.NoError(t, err)
	require.Equal(t, expectedManifest(sourceName, "cluster/stack.yaml", mainStack), stack)

	pool, err := config.NodePoolManifest("example", "main.yaml")
	require.NoError(t, err)
	require.Equal(t, expectedManifest(sourceName, "cluster/node-pools/example/main.yaml", "node-pool"), pool)

	defaults, err := config.DefaultsManifests()
	require.NoError(t, err)
	require.Equal(t, []Manifest{expectedManifest(sourceName, "cluster/config-defaults.yaml", "defaults")}, defaults)

	deletions, err := config.DeletionsManifests()
	require.NoError(t, err)
	require.Equal(t, []Manifest{expectedManifest(sourceName, "cluster/manifests/deletions.yaml", "deletions")}, deletions)

	manifests, err := config.Components()
	require.NoError(t, err)
	expected := []Component{
		{
			Name: "example1",
			Manifests: []Manifest{
				expectedManifest(sourceName, "cluster/manifests/example1/main.yaml", "example1-main"),
			},
		},
		{
			Name: "example2",
			Manifests: []Manifest{
				expectedManifest(sourceName, "cluster/manifests/example2/config.yaml", "example2-config"),
				expectedManifest(sourceName, "cluster/manifests/example2/main.yaml", "example2-main"),
			},
		},
	}
	require.Equal(t, expected, manifests)
}
