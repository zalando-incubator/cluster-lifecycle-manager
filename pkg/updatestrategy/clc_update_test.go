package updatestrategy

import (
	"context"
	"fmt"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

type nodeState int

const (
	nodeStateReady nodeState = iota
	nodeStateDecommissionPending
	nodeStateDecommissioning
	nodeStateTerminated
)

type mockNodeCLC struct {
	name       string
	generation int
	state      nodeState
}

type mockNodePoolManagerCLC struct {
	generation            int
	nodes                 []*mockNodeCLC
	abortAfterTerminating string
	abortFunc             func()
}

func (m *mockNodePoolManagerCLC) MarkPoolForDecommission(_ *api.NodePool) error {
	return nil
}

func (m *mockNodePoolManagerCLC) DisableReplacementNodeProvisioning(_ context.Context, _ *Node) error {
	return nil
}

func (m *mockNodePoolManagerCLC) advance() {
	for _, node := range m.nodes {
		switch node.state {
		case nodeStateDecommissionPending:
			log.Infof("decommissioning node %s", node.name)
			node.state = nodeStateDecommissioning
			return
		case nodeStateDecommissioning:
			log.Infof("decommissioned node %s", node.name)
			node.state = nodeStateTerminated
			if node.name == m.abortAfterTerminating {
				m.abortFunc()
			}
			return
		case nodeStateTerminated:
			if node.name == m.abortAfterTerminating {
				return
			}
		}
	}
}

func (m *mockNodePoolManagerCLC) GetPool(_ context.Context, _ *api.NodePool) (*NodePool, error) {
	m.advance()

	result := &NodePool{
		Min:        0,
		Desired:    0,
		Current:    0,
		Max:        10,
		Generation: m.generation,
	}

	for _, node := range m.nodes {
		if node.state != nodeStateTerminated {
			var lifecycleStatus string
			switch node.state {
			case nodeStateReady:
				lifecycleStatus = "ready"
			case nodeStateDecommissionPending:
				lifecycleStatus = "decommission-pending"
			case nodeStateDecommissioning:
				lifecycleStatus = "decommissioning"
			default:
				panic("invalid lifecycle status")
			}

			result.Nodes = append(result.Nodes, &Node{
				Name:       node.name,
				Labels:     map[string]string{"lifecycle-status": lifecycleStatus},
				Generation: node.generation,
			})
		}
	}

	return result, nil
}

func (m *mockNodePoolManagerCLC) findNode(nodeName string) (*mockNodeCLC, error) {
	for _, n := range m.nodes {
		if n.name == nodeName {
			return n, nil
		}
	}
	return nil, fmt.Errorf("unknown node: %s", nodeName)
}

func (m *mockNodePoolManagerCLC) MarkNodeForDecommission(_ context.Context, node *Node) error {
	n, err := m.findNode(node.Name)
	if err != nil {
		return err
	}

	if n.generation == m.generation {
		return fmt.Errorf("decommission attempted on an up-to-date node %s", node.Name)
	}

	if n.state == nodeStateReady {
		n.state = nodeStateDecommissionPending
	}
	return nil
}

func (m *mockNodePoolManagerCLC) AbortNodeDecommissioning(_ context.Context, node *Node) error {
	n, err := m.findNode(node.Name)
	if err != nil {
		return err
	}

	if n.state == nodeStateDecommissionPending {
		n.state = nodeStateReady
	}
	return nil
}

func (m *mockNodePoolManagerCLC) ScalePool(_ context.Context, _ *api.NodePool, _ int) error {
	panic("should not be called")
}

func (m *mockNodePoolManagerCLC) TerminateNode(_ context.Context, _ *Node, _ bool) error {
	panic("should not be called")
}

func (m *mockNodePoolManagerCLC) CordonNode(_ context.Context, _ *Node) error {
	panic("should not be called")
}

func TestCLCUpdateStrategy(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		nodePoolGeneration    int
		nodes                 []*mockNodeCLC
		abortAfterTerminating string
		expectTerminated      []string
		expectNotTerminated   []string
	}{
		{
			name:               "old nodes are decommissioned, new nodes are left intact",
			nodePoolGeneration: 3,
			nodes: []*mockNodeCLC{
				{name: "node-1", generation: 1},
				{name: "node-2", generation: 3},
				{name: "node-3", generation: 3},
				{name: "node-4", generation: 2},
				{name: "node-5", generation: 10},
			},
			expectTerminated:    []string{"node-1", "node-4", "node-5"},
			expectNotTerminated: []string{"node-2", "node-3"},
		},
		{
			name:               "works fine if there are no old nodes",
			nodePoolGeneration: 3,
			nodes: []*mockNodeCLC{
				{name: "node-1", generation: 3},
				{name: "node-2", generation: 3},
			},
			expectNotTerminated: []string{"node-1", "node-2"},
		},
		{
			name:               "correctly aborts",
			nodePoolGeneration: 3,
			nodes: []*mockNodeCLC{
				{name: "node-1", generation: 1},
				{name: "node-2", generation: 3},
				{name: "node-3", generation: 2},
				{name: "node-4", generation: 4},
				{name: "node-5", generation: 3},
				{name: "node-6", generation: 10},
			},
			abortAfterTerminating: "node-3",
			expectTerminated:      []string{"node-1", "node-3"},
			expectNotTerminated:   []string{"node-2", "node-4", "node-5", "node-6"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.WithField("test", true)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			nodePoolManager := &mockNodePoolManagerCLC{
				generation:            tc.nodePoolGeneration,
				nodes:                 tc.nodes,
				abortAfterTerminating: tc.abortAfterTerminating,
				abortFunc:             cancel,
			}
			strategy := NewCLCUpdateStrategy(logger, nodePoolManager, 50*time.Millisecond)
			nodePool := &api.NodePool{
				DiscountStrategy: "none",
				InstanceType:     "m4.large",
				Name:             "test-node-pool",
				Profile:          "test-profile",
				MinSize:          0,
				MaxSize:          10,
			}

			err := strategy.Update(ctx, nodePool)
			if tc.abortAfterTerminating == "" {
				require.NoError(t, err)
			} else {
				require.EqualValues(t, context.Canceled, err)
			}

			for _, nodeName := range tc.expectTerminated {
				node, err := nodePoolManager.findNode(nodeName)
				require.NoError(t, err)
				require.Equal(t, nodeStateTerminated, node.state, "expected node %s to be terminated", nodeName)
			}
			for _, nodeName := range tc.expectNotTerminated {
				node, err := nodePoolManager.findNode(nodeName)
				require.NoError(t, err)
				require.Equal(t, nodeStateReady, node.state, "expected node %s to be ready", nodeName)
			}
		})
	}
}
