package updatestrategy

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/pkg/api/v1"
)

const (
	lifecycleStatusLabel               = "lifecycle-status"
	lifecycleStatusReady               = "ready"
	lifecycleStatusDraining            = "draining"
	lifecycleStatusDecommissionPending = "decommission-pending"

	decommissionPendingTaintKey   = "decommission-pending"
	decommissionPendingTaintValue = "rolling-upgrade"
)

var (
	errTimeoutExceeded     = errors.New("timeout exceeded")
	operationMaxTimeout    = 15 * time.Minute
	operationCheckInterval = 15 * time.Second
)

// RollingUpdateStrategy is a cluster node update strategy which will roll the
// nodes with a specified surge.
type RollingUpdateStrategy struct {
	nodePoolManager NodePoolManager
	surge           int
	logger          *log.Entry
}

// NewRollingUpdateStrategy initializes a new RollingUpdateStrategy.
func NewRollingUpdateStrategy(logger *log.Entry, nodePoolManager NodePoolManager, surge int) *RollingUpdateStrategy {
	return &RollingUpdateStrategy{
		nodePoolManager: nodePoolManager,
		surge:           surge,
		logger:          logger.WithField("strategy", "rolling"),
	}
}

// labelNodes label nodes with the correct lifecycle status label.
func (r *RollingUpdateStrategy) labelNodes(nodePool *NodePool) error {
	for _, node := range nodePool.Nodes {
		lifecycleStatus := lifecycleStatusReady
		if node.Generation != nodePool.Generation {
			lifecycleStatus = lifecycleStatusDecommissionPending
			if node.Labels[lifecycleStatusLabel] == lifecycleStatusDraining {
				lifecycleStatus = lifecycleStatusDraining
			}
		}

		// ensure node has the right lifecycle status label
		err := r.nodePoolManager.LabelNode(node, lifecycleStatusLabel, lifecycleStatus)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RollingUpdateStrategy) taintOldNodes(nodePool *NodePool) error {
	for _, node := range nodePool.Nodes {
		if node.Generation != nodePool.Generation {
			err := r.nodePoolManager.TaintNode(node, decommissionPendingTaintKey, decommissionPendingTaintValue, v1.TaintEffectPreferNoSchedule)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// isUpdateDone returns true if there are no old nodes in the node pool.
func (r *RollingUpdateStrategy) isUpdateDone(nodePool *NodePool) bool {
	oldNodes, _ := r.splitOldNewNodes(nodePool)
	return len(oldNodes) == 0
}

// terminateCordonedNodes filters for nodes to be terminated and terminates the
// nodes one by one. It will conditionally scale down the node pool in case
// there is less than surge old nodes left.
func (r *RollingUpdateStrategy) terminateCordonedNodes(nodePool *NodePool, surge int) error {
	oldNodes, _ := r.splitOldNewNodes(nodePool)
	nodesToTerminate := filterNodesToTerminate(oldNodes)
	r.logger.Debugf("Found %d nodes to be terminated", len(nodesToTerminate))

	numOldNodes := len(oldNodes)

	for _, node := range nodesToTerminate {
		// if we only have surge or less old nodes left, then just
		// scale when terminating node.
		scaleDown := numOldNodes <= surge

		err := r.nodePoolManager.TerminateNode(node, scaleDown)
		if err != nil {
			return err
		}

		numOldNodes--
	}

	return nil
}

// cordonNodes cordons a list of nodes.
func (r *RollingUpdateStrategy) cordonNodes(nodes []*Node) error {
	r.logger.Debugf("Found %d nodes to cordon", len(nodes))
	for _, node := range nodes {
		err := r.nodePoolManager.CordonNode(node)
		if err != nil {
			return err
		}
	}
	return nil
}

// increaseByUnmatchedNodes increases the Node Pool by the number of nodes
// where the failure domain was unmatched by new nodes.
func (r *RollingUpdateStrategy) increaseByUnmatchedNodes(nodePool *NodePool, nodePoolDesc *api.NodePool, unmatchedNodes []*Node) error {
	if len(unmatchedNodes) > 0 {
		r.logger.Debugf("Found %d nodes with unmatched failure domain", len(unmatchedNodes))
		newDesired := nodePool.Desired + len(unmatchedNodes)
		err := r.nodePoolManager.ScalePool(nodePoolDesc, newDesired)
		if err != nil {
			return err
		}
	}
	return nil
}

// Update performs a rolling update of a single node pool. Passing a context
// allows stopping the update loop in case the context is canceled.
func (r *RollingUpdateStrategy) Update(ctx context.Context, nodePoolDesc *api.NodePool) error {
	r.logger.Infof("Initializing update of node pool '%s'", nodePoolDesc.Name)

	if nodePoolDesc.MaxSize < 1 {
		return nil
	}

	// limit surge to max size of the node pool
	surge := int(math.Min(float64(nodePoolDesc.MaxSize), float64(r.surge)))

	for {
		// wait/scale to ensure that we have at least 'surge' new nodes in the node pool
		nodePool, err := r.scaleOutAndWaitForNodesToBeReady(ctx, nodePoolDesc, surge)
		if err != nil {
			return err
		}

		// label nodes with correct lifecycle-status
		err = r.labelNodes(nodePool)
		if err != nil {
			return err
		}

		// taint old nodes to prevent scheduling
		err = r.taintOldNodes(nodePool)
		if err != nil {
			return err
		}

		// check if there are no old nodes left to update
		if r.isUpdateDone(nodePool) {
			break
		}

		// terminate all cordoned nodes and conditionally scale
		// down the node pool in case there are less than surge old
		// nodes left to update
		err = r.terminateCordonedNodes(nodePool, surge)
		if err != nil {
			return err
		}

		// wait for current number of nodes equal to the desired number of nodes
		nodePool, err = WaitForDesiredNodes(ctx, r.logger, r.nodePoolManager, nodePoolDesc)
		if err != nil {
			return err
		}

		// compute nodes to cordon and unmatched nodes
		toCordon, unmatchedNodes := r.computeNodesList(nodePool, surge)

		// cordon the selected nodes
		err = r.cordonNodes(toCordon)
		if err != nil {
			return err
		}

		// increase node pool size by unmatched nodes in order to
		// get new nodes in a matching failure domain
		err = r.increaseByUnmatchedNodes(nodePool, nodePoolDesc, unmatchedNodes)
		if err != nil {
			return err
		}

		// stop update in case the context is canceled
		select {
		case <-ctx.Done():
			return errTimeoutExceeded
		default:
		}
	}
	r.logger.Infof("Node pool '%s' successfully updated", nodePoolDesc.Name)
	return nil
}

// computeNodesList computes what old nodes to be cordoned and for which nodes
// the failure domain is unmatched by new nodes. It will at most return surge
// nodes. It will return a list of nodes to be cordoned as the first value and
// a list of nodes for which there were no matching new nodes based on the
// failure domain as the second value.
// Only for nodes which has the VolumesAttached flag set will it attempt to
// find new nodes with a matching failure domain. The failure domain is not
// considered when the node doesn't have any volumes attached as it is not
// necessary.
func (r *RollingUpdateStrategy) computeNodesList(nodePool *NodePool, surge int) ([]*Node, []*Node) {
	oldNodes, newNodes := r.splitOldNewNodes(nodePool)

	newNodesMap := make(map[string]*Node, len(newNodes))

	for _, node := range newNodes {
		newNodesMap[node.ProviderID] = node
	}

	volumesAttached, noVolumesAttached := r.splitVolumeNoVolumeAttachedNodes(oldNodes)

	toCordon := make([]*Node, 0, len(oldNodes))
	unmatchedNodes := []*Node{}
	for _, o := range volumesAttached {
		var matchedNode *Node
		for _, n := range newNodesMap {
			if n.FailureDomain == o.FailureDomain {
				matchedNode = o
				delete(newNodesMap, n.ProviderID)
				break
			}
		}
		if matchedNode != nil {
			toCordon = append(toCordon, o)
		} else {
			unmatchedNodes = append(unmatchedNodes, o)
		}

		// limit the number of old nodes to handle at a time.
		if len(toCordon) == surge {
			return toCordon, nil
		}
	}

	// add up to surge nodes with no volumes attached (no need to match by
	// failure domain)
	extra := int(math.Min(float64(surge-len(toCordon)), float64(len(noVolumesAttached))))
	toCordon = append(toCordon, noVolumesAttached[:extra]...)

	// add up to surge unmatched nodes in case we found less than surge
	// nodes to cordon
	extra = int(math.Min(float64(surge-len(toCordon)), float64(len(unmatchedNodes))))
	unmatchedNodes = unmatchedNodes[:extra]

	return toCordon, unmatchedNodes
}

// scaleAndWaitForNewNodes waits for a minimum of surge new nodes.
func (r *RollingUpdateStrategy) scaleOutAndWaitForNodesToBeReady(ctx context.Context, nodePoolDesc *api.NodePool, surge int) (*NodePool, error) {
	var nodePool *NodePool
	var err error
	for {
		nodePool, err = WaitForDesiredNodes(ctx, r.logger, r.nodePoolManager, nodePoolDesc)
		if err != nil {
			return nil, err
		}

		// separate old and new nodes
		_, newNodes := r.splitOldNewNodes(nodePool)
		if len(newNodes) >= surge {
			break
		}
		newDesired := nodePool.Desired + surge - len(newNodes)
		err = r.nodePoolManager.ScalePool(nodePoolDesc, newDesired)
		if err != nil {
			return nil, err
		}
	}

	return nodePool, nil
}

// splitOldNewNodes splits a slice of nodes into two slices of old and new
// nodes.  Whether a node is old or new is determined by the Generation of the
// node. If it matches the Generation of the NodePool it's considered new,
// otherwise it's considered old.
func (r *RollingUpdateStrategy) splitOldNewNodes(nodePool *NodePool) ([]*Node, []*Node) {
	oldNodes := make([]*Node, 0)
	newNodes := make([]*Node, 0)

	for _, node := range nodePool.Nodes {
		if node.Generation != nodePool.Generation {
			oldNodes = append(oldNodes, node)
		} else {
			newNodes = append(newNodes, node)
		}
	}

	return oldNodes, newNodes
}

// splitVolumeNoVolumeAttachedNodes splits a slice of nodes into two slices of
// nodes with volumes attached and nodes without volumes attached respectively.
func (r *RollingUpdateStrategy) splitVolumeNoVolumeAttachedNodes(nodes []*Node) ([]*Node, []*Node) {
	volumesAttached := make([]*Node, 0)
	noVolumeAttached := make([]*Node, 0)

	for _, node := range nodes {
		if node.VolumesAttached {
			volumesAttached = append(volumesAttached, node)
		} else {
			noVolumeAttached = append(noVolumeAttached, node)
		}
	}

	return volumesAttached, noVolumeAttached
}

// filterNodesToTerminate filters for nodes that are cordoned (unschedulable) and
// wasn't already marked for draining.
func filterNodesToTerminate(nodes []*Node) []*Node {
	cordoned := make([]*Node, 0)
	for _, node := range nodes {
		if node.Cordoned {
			cordoned = append(cordoned, node)
		}
	}

	return cordoned
}
