package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAvailableStorage(t *testing.T) {
	ebsSize := int64(10 * 1024 * 1024)

	for _, tc := range []struct {
		msg           string
		instanceTypes []string
		expected      int64
	}{
		{
			msg:           "no instance storage",
			instanceTypes: []string{"m5.xlarge"},
			expected:      ebsSize,
		},
		{
			msg:           "mixed with/without instance storage",
			instanceTypes: []string{"m5.xlarge", "m5d.xlarge"},
			expected:      ebsSize,
		},
		{
			msg:           "nodes with instance storage",
			instanceTypes: []string{"m5d.8xlarge", "m5d.24xlarge"},
			expected:      0.75 * 2 * 600 * 1024 * 1024 * 1024,
		},
	} {
		t.Run(tc.msg, func(t *testing.T) {
			pool := NodePool{InstanceTypes: tc.instanceTypes}
			size, err := pool.AvailableStorage(ebsSize, 0.75)
			require.NoError(t, err)
			require.Equal(t, tc.expected, size)
		})
	}
}

func TestIsSpotIO(t *testing.T) {
	pool := NodePool{Profile: "worker-spotio"}
	require.True(t, pool.IsSpotIO())
}
