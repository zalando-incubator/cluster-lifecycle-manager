package channel

import (
	"context"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestDirectoryChannel(t *testing.T) {
	location := "/test-dir"

	logger := log.StandardLogger().WithFields(map[string]interface{}{})

	d, err := NewDirectory(location)
	require.NoError(t, err)
	channels, err := d.Update(context.Background(), logger)
	require.NoError(t, err)
	require.Empty(t, channels)

	cc, err := d.Get(context.Background(), logger, "channel")
	require.NoError(t, err)

	if cc.Path != location {
		t.Errorf("expected %s, got %s", location, cc.Path)
	}
}
