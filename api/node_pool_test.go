package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsKarpenter(t *testing.T) {
	pool := NodePool{Profile: "worker-karpenter"}
	require.True(t, pool.IsKarpenter())
}
