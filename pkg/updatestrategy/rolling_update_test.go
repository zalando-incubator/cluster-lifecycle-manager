package updatestrategy

import (
	"context"
	"math/rand"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"

// generateString generates a random string token.
func generateString() string {
	b := make([]byte, 64)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := range b {
		b[i] = letterBytes[r.Intn(len(letterBytes))]
	}
	return string(b)
}

// mockNode mocks the basic fields of a Node object.
func mockNode(failureDomain string, generation int, cordoned, volumesAttached bool) *Node {
	return &Node{
		Cordoned:        cordoned,
		ProviderID:      generateString(),
		FailureDomain:   failureDomain,
		Generation:      generation,
		VolumesAttached: volumesAttached,
		Ready:           true,
	}
}

// mockNodePoolManager implements the NodePoolManager interface for testing. It
// works by maintaining a NodePool.
type mockNodePoolManager struct {
	nodePool *NodePool
}

func (m *mockNodePoolManager) MarkPoolForDecommission(nodePool *api.NodePool) error {
	return nil
}

func (m *mockNodePoolManager) DisableReplacementNodeProvisioning(ctx context.Context, node *Node) error {
	return nil
}

func (m *mockNodePoolManager) GetPool(ctx context.Context, nodePool *api.NodePool) (*NodePool, error) {
	return m.nodePool, nil
}

func (m *mockNodePoolManager) MarkNodeForDecommission(ctx context.Context, node *Node) error {
	return nil
}

func (m *mockNodePoolManager) AbortNodeDecommissioning(ctx context.Context, node *Node) error {
	return nil
}

func (m *mockNodePoolManager) ScalePool(ctx context.Context, nodePool *api.NodePool, replicas int) error {
	if replicas > m.nodePool.Current {
		delta := replicas - m.nodePool.Current
		for i := 0; i < delta; i++ {
			m.nodePool.Nodes = append(m.nodePool.Nodes, mockNode(getFailureDomain(m.nodePool.Nodes), m.nodePool.Generation, false, false))
		}
	} else if replicas < m.nodePool.Current {
		m.nodePool.Nodes = m.nodePool.Nodes[0:replicas]
	}

	m.nodePool.Current = replicas
	m.nodePool.Desired = replicas
	m.nodePool.Min = replicas
	m.nodePool.Max = replicas
	return nil
}

func (m *mockNodePoolManager) TerminateNode(ctx context.Context, node *Node, decrementDesired bool) error {
	newNodes := make([]*Node, 0, len(m.nodePool.Nodes))
	for _, n := range m.nodePool.Nodes {
		if n.ProviderID != node.ProviderID {
			newNodes = append(newNodes, n)
		}
	}

	replicas := m.nodePool.Current
	m.nodePool.Nodes = newNodes

	m.nodePool.Current = len(m.nodePool.Nodes)
	m.nodePool.Desired = len(m.nodePool.Nodes)
	m.nodePool.Min = len(m.nodePool.Nodes)
	m.nodePool.Max = len(m.nodePool.Nodes)

	if !decrementDesired {
		// rescale pool to replace 'terminated' nodes
		return m.ScalePool(context.Background(), nil, replicas)
	}

	return nil
}

func (m *mockNodePoolManager) CordonNode(ctx context.Context, node *Node) error {
	for _, n := range m.nodePool.Nodes {
		if n.ProviderID == node.ProviderID {
			n.Cordoned = true
		}
	}
	return nil
}

// get the failure domain used by the least amount of nodes in a nodes list.
// if two failure domains both has the least amount of nodes, then the failure
// domain strings are ordered and the first one is favoured in order to produce
// a predictable distribution in tests.
func getFailureDomain(nodes []*Node) string {
	fdCount := map[string]int{
		"a": 0,
		"b": 0,
		"c": 0,
	}

	for _, node := range nodes {
		if _, ok := fdCount[node.FailureDomain]; ok {
			fdCount[node.FailureDomain]++
		} else {
			fdCount[node.FailureDomain] = 1
		}
	}

	min := len(nodes)
	failureDomain := "a"
	for fd, num := range fdCount {
		if num < min || (num == min && fd < failureDomain) {
			min = num
			failureDomain = fd
		}
	}

	return failureDomain
}

func TestUpdate(tt *testing.T) {
	for _, tc := range []struct {
		msg             string
		nodePoolManager NodePoolManager
		surge           int
		expected        *NodePool
		success         bool
		nodePoolMaxSize int64
	}{
		{
			msg: "test that all new nodes are not updated",
			nodePoolManager: &mockNodePoolManager{
				nodePool: &NodePool{
					Min:        3,
					Max:        3,
					Current:    3,
					Desired:    3,
					Generation: 1,
					Nodes: []*Node{
						mockNode("a", 1, false, false),
						mockNode("b", 1, false, false),
						mockNode("c", 1, false, false),
					},
				},
			},
			surge:           3,
			nodePoolMaxSize: 20,
			expected: &NodePool{
				Min:        3,
				Max:        3,
				Current:    3,
				Desired:    3,
				Generation: 1,
				Nodes: []*Node{
					mockNode("a", 1, false, false),
					mockNode("b", 1, false, false),
					mockNode("c", 1, false, false),
				},
			},
			success: true,
		},
		{
			msg: "test updating all nodes (which are old), single failure domain",
			nodePoolManager: &mockNodePoolManager{
				nodePool: &NodePool{
					Min:        3,
					Max:        3,
					Current:    3,
					Desired:    3,
					Generation: 2,
					Nodes: []*Node{
						mockNode("a", 1, false, false),
						mockNode("b", 1, false, false),
						mockNode("c", 1, false, false),
					},
				},
			},
			surge:           3,
			nodePoolMaxSize: 20,
			expected: &NodePool{
				Min:        3,
				Max:        3,
				Current:    3,
				Desired:    3,
				Generation: 2,
				Nodes: []*Node{
					mockNode("a", 2, false, false),
					mockNode("b", 2, false, false),
					mockNode("c", 2, false, false),
				},
			},
			success: true,
		},
		{
			msg: "test getting nodes with matching failure domain",
			nodePoolManager: &mockNodePoolManager{
				nodePool: &NodePool{
					Min:        4,
					Max:        4,
					Current:    4,
					Desired:    4,
					Generation: 2,
					Nodes: []*Node{
						mockNode("a", 1, false, true),
						mockNode("b", 1, false, true),
						mockNode("c", 1, false, true),
						mockNode("a", 1, false, true),
					},
				},
			},
			surge:           3,
			nodePoolMaxSize: 20,
			expected: &NodePool{
				Min:        4,
				Max:        4,
				Current:    4,
				Desired:    4,
				Generation: 2,
				Nodes: []*Node{
					mockNode("b", 2, false, false),
					mockNode("c", 2, false, false),
					mockNode("a", 2, false, false),
					mockNode("a", 2, false, false),
				},
			},
			success: true,
		},
		{
			msg: "test getting expected nodes when max < surge",
			nodePoolManager: &mockNodePoolManager{
				nodePool: &NodePool{
					Min:        2,
					Max:        2,
					Current:    2,
					Desired:    2,
					Generation: 2,
					Nodes: []*Node{
						mockNode("a", 1, false, false),
						mockNode("b", 1, false, false),
					},
				},
			},
			surge:           3,
			nodePoolMaxSize: 2,
			expected: &NodePool{
				Min:        2,
				Max:        2,
				Current:    2,
				Desired:    2,
				Generation: 2,
				Nodes: []*Node{
					mockNode("c", 2, false, false),
					mockNode("a", 2, false, false),
				},
			},
			success: true,
		},
	} {
		tt.Run(tc.msg, func(t *testing.T) {
			logger := log.WithField("test", true)
			np := &api.NodePool{Name: "test", MaxSize: tc.nodePoolMaxSize}
			strategy := NewRollingUpdateStrategy(logger, tc.nodePoolManager, tc.surge)
			err := strategy.Update(context.Background(), np)
			if err != nil && tc.success {
				t.Errorf("should not fail: %v", err)
			}
			if err == nil && !tc.success {
				t.Error("expected failure")
			}

			nodePool, _ := tc.nodePoolManager.GetPool(context.Background(), np)
			if tc.success && !equalNodePool(nodePool, tc.expected) {
				t.Errorf("final node pool nodePool did not match expected nodePool")
			}
		})
	}
}

func equalNodePool(a, b *NodePool) bool {
	if a.Current != b.Current {
		return false
	}

	if a.Max != b.Max {
		return false
	}

	if a.Min != b.Min {
		return false
	}

	if a.Desired != b.Desired {
		return false
	}

	if len(a.Nodes) != len(b.Nodes) {
		return false
	}

	// compare node list
	for i, node := range a.Nodes {
		if node.FailureDomain != b.Nodes[i].FailureDomain {
			return false
		}
	}

	return true
}
