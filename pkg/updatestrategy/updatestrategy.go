package updatestrategy

import (
	"context"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	v1 "k8s.io/api/core/v1"
)

// UpdateStrategy defines an interface for performing cluster node updates.
type UpdateStrategy interface {
	Update(ctx context.Context, nodePool *api.NodePool) error
	PrepareForRemoval(ctx context.Context, nodePool *api.NodePool) error
}

// ProviderNodePoolsBackend is an interface for describing a node pools
// provider backend e.g. AWS Auto Scaling Groups.
type ProviderNodePoolsBackend interface {
	Get(ctx context.Context, nodePool *api.NodePool) (*NodePool, error)
	Scale(ctx context.Context, nodePool *api.NodePool, replicas int) error
	MarkForDecommission(ctx context.Context, nodePool *api.NodePool) error
	Terminate(ctx context.Context, nodePool *api.NodePool, node *Node, decrementDesired bool) error
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
	Annotations     map[string]string
	Labels          map[string]string
	Taints          []v1.Taint
	Cordoned        bool
	ProviderID      string
	FailureDomain   string
	Generation      int
	VolumesAttached bool
	Ready           bool
	Master          bool
}

// ProfileNodePoolProvisioner is a NodePoolProvisioner which selects the
// backend provisioner based on the node pool profile. It has a default
// provisioner and a mapping of profile to provisioner for those profiles which
// can't use the default provisioner.
type ProfileNodePoolProvisioner struct {
	defaultProvisioner ProviderNodePoolsBackend
	profileMapping     map[string]ProviderNodePoolsBackend
}

// NewProfileNodePoolsBackend initializes a new ProfileNodePoolProvisioner.
func NewProfileNodePoolsBackend(defaultProvisioner ProviderNodePoolsBackend, profileMapping map[string]ProviderNodePoolsBackend) *ProfileNodePoolProvisioner {
	return &ProfileNodePoolProvisioner{
		defaultProvisioner: defaultProvisioner,
		profileMapping:     profileMapping,
	}
}

// Get the specified node pool using the right node pool provisioner for the
// profile.
func (n *ProfileNodePoolProvisioner) Get(ctx context.Context, nodePool *api.NodePool) (*NodePool, error) {
	if provisioner, ok := n.profileMapping[nodePool.Profile]; ok {
		return provisioner.Get(ctx, nodePool)
	}

	return n.defaultProvisioner.Get(ctx, nodePool)
}

// MarkForDecommission marks a node pool for decommissioning using the right
// node pool provisioner for the profile.
func (n *ProfileNodePoolProvisioner) MarkForDecommission(ctx context.Context, nodePool *api.NodePool) error {
	if provisioner, ok := n.profileMapping[nodePool.Profile]; ok {
		return provisioner.MarkForDecommission(ctx, nodePool)
	}

	return n.defaultProvisioner.MarkForDecommission(ctx, nodePool)
}

// Scale scales a node pool  using the right node pool provisioner for the
// profile.
func (n *ProfileNodePoolProvisioner) Scale(ctx context.Context, nodePool *api.NodePool, replicas int) error {
	if provisioner, ok := n.profileMapping[nodePool.Profile]; ok {
		return provisioner.Scale(ctx, nodePool, replicas)
	}

	return n.defaultProvisioner.Scale(ctx, nodePool, replicas)
}

// Terminate terminates a node pool using the default provisioner.
func (n *ProfileNodePoolProvisioner) Terminate(ctx context.Context, nodePool *api.NodePool, node *Node, decrementDesired bool) error {
	if provisioner, ok := n.profileMapping[nodePool.Profile]; ok {
		return provisioner.Terminate(ctx, nodePool, node, decrementDesired)
	}

	return n.defaultProvisioner.Terminate(ctx, nodePool, node, decrementDesired)
}
