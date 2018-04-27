package updatestrategy

import (
	"context"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"k8s.io/client-go/pkg/api/v1"
)

// UpdateStrategy defines an interface for performing cluster node updates.
type UpdateStrategy interface {
	Update(ctx context.Context, nodePool *api.NodePool) error
}

// ProviderNodePoolsBackend is an interface for describing a node pools
// provider backend e.g. AWS Auto Scaling Groups.
type ProviderNodePoolsBackend interface {
	Get(nodePool *api.NodePool) (*NodePool, error)
	Scale(nodePool *api.NodePool, replicas int) error
	SuspendAutoscaling(nodePool *api.NodePool) error
	Terminate(node *Node, decrementDesired bool) error
}

// NodePool defines a node pool including all nodes.
type NodePool struct {
	Min        int
	Desired    int
	Current    int
	Max        int
	Generation int
	Nodes      []*Node
}

// ReadyNodes returns a list of nodes which are marked as ready.
func (n *NodePool) ReadyNodes() []*Node {
	nodes := make([]*Node, 0, len(n.Nodes))
	for _, node := range n.Nodes {
		if node.Ready {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// Node is an abstract node object which combines the node information from the
// node pool backend along with the corresponding Kubernetes node object.
type Node struct {
	Name            string
	Labels          map[string]string
	Taints          []v1.Taint
	Cordoned        bool
	ProviderID      string
	FailureDomain   string
	Generation      int
	VolumesAttached bool
	Ready           bool
}
