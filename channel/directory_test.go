package channel

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirectoryChannel(t *testing.T) {
	location := "/test-dir"

	d := NewDirectory(location)
	channels, err := d.Update()
	require.NoError(t, err)
	require.NotEmpty(t, channels)

	cc, err := d.Get("channel")
	require.NoError(t, err)

	if cc.Path != location {
		t.Errorf("expected %s, got %s", location, cc.Path)
	}
}
