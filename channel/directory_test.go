package channel

import (
	"context"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestDirectoryChannel(t *testing.T) {
	logger := log.StandardLogger().WithFields(map[string]interface{}{})

	tempDir := CreateTempDir(t)
	defer os.RemoveAll(tempDir)

	setupExampleConfig(t, tempDir, "main")

	d, err := NewDirectory("testsrc", tempDir)
	require.NoError(t, err)

	err = d.Update(context.Background(), logger)
	require.NoError(t, err)

	anyVersion, err := d.Version("foobar", nil)
	require.NoError(t, err)

	config, err := anyVersion.Get(context.Background(), logger)
	require.NoError(t, err)

	verifyExampleConfig(t, config, "testsrc", "main")

	require.NoError(t, config.Delete())
	_, err = os.Stat(tempDir)
	require.NoError(t, err)
}

func TestDirectoryManifestOrigin(t *testing.T) {
	dir := CreateTempDir(t)
	defer os.RemoveAll(dir)
	setupExampleConfig(t, dir, "main")

	d, err := NewDirectory("main", dir)
	require.NoError(t, err)
	version, err := d.Version("any", nil)
	require.NoError(t, err)
	config, err := version.Get(context.Background(), log.NewEntry(log.New()))
	require.NoError(t, err)

	stack, err := config.StackManifest("stack.yaml")
	require.NoError(t, err)
	require.Equal(t, Origin{Repository: "main", Ref: "local-directory", FilePath: "cluster/stack.yaml"}, stack.Origin)
}
