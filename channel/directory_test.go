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

	tempdir := createTempDir(t)
	defer os.RemoveAll(tempdir)

	setupExampleConfig(t, tempdir, "main")

	d, err := NewDirectory(tempdir)
	require.NoError(t, err)
	channels, err := d.Update(context.Background(), logger)
	require.NoError(t, err)
	require.Empty(t, channels)

	config, err := d.Get(context.Background(), logger, "channel")
	require.NoError(t, err)

	verifyExampleConfig(t, config, "main")

	require.NoError(t, config.Delete())
	_, err = os.Stat(tempdir)
	require.NoError(t, err)
}
