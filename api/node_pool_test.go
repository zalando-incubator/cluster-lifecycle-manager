package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSpotIO(t *testing.T) {
	pool := NodePool{Profile: "worker-spotio"}
	require.True(t, pool.IsSpotIO())
}
