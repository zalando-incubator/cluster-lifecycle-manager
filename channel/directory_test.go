package channel

import (
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestDirectoryChannel(t *testing.T) {
	location := "/test-dir"

	logger := log.StandardLogger().WithFields(map[string]interface{}{})

	d := NewDirectory(location)
	channels, err := d.Update(logger)
	require.NoError(t, err)
	require.NotEmpty(t, channels)

	cc, err := d.Get(logger, "channel")
	require.NoError(t, err)

	if cc.Path != location {
		t.Errorf("expected %s, got %s", location, cc.Path)
	}
}
