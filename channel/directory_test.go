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

	d, err := NewDirectory("testsrc", tempdir)
	require.NoError(t, err)

	err = d.Update(context.Background(), logger)
	require.NoError(t, err)

	anyVersion, err := d.Version([]string{"foobar"})
	require.NoError(t, err)

	config, err := anyVersion.Get(context.Background(), logger)
	require.NoError(t, err)

	verifyExampleConfig(t, config, "testsrc", "main")

	require.NoError(t, config.Delete())
	_, err = os.Stat(tempdir)
	require.NoError(t, err)
}
