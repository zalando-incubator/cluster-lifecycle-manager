package updatestrategy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadyNodes(t *testing.T) {
	nodePool := &NodePool{
		Nodes: []*Node{
			{
				Ready: true,
			},
		},
	}

	nodes := nodePool.ReadyNodes()
	assert.Len(t, nodes, 1)
}
