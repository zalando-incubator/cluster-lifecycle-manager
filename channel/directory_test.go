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
