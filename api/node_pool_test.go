package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaints(t *testing.T) {
	pool := NodePool{ConfigItems: map[string]string{
		"taints": "dedicated=test:NoSchedule,example:NoSchedule",
	}}
	require.Equal(t, []Taint{
		{Key: "dedicated", Value: "test", Effect: "NoSchedule"},
		{Key: "example", Value: "", Effect: "NoSchedule"},
	}, pool.Taints())
}
