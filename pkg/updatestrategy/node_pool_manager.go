package updatestrategy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	v1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	mirrorPodAnnotation = "kubernetes.io/config.mirror"
	multiplePDBsErrMsg  = "This pod has more than one PodDisruptionBudget"
	maxConflictRetries  = 50
	// podEvictionHeadroom is the extra time we wait to catch situations when the pod is ignoring SIGTERM and
	// is killed with SIGKILL after TerminationGracePeriodSeconds
	// Same headroom as the cluster-autoscaler:
	// https://github.com/kubernetes/autoscaler/blob/cluster-autoscaler-1.2.2/cluster-autoscaler/core/scale_down.go#L77
	podEvictionHeadroom = 30 * time.Second

	lifecycleStatusLabel               = "lifecycle-status"
	lifecycleStatusDraining            = "draining"
	lifecycleStatusDecommissionPending = "decommission-pending"
	lifecycleStatusReady               = "ready"

	decommissionPendingTaintKey   = "decommission-pending"
	decommissionPendingTaintValue = "rolling-upgrade"

	clcReplacementStrategyLabel = "cluster-lifecycle-controller.zalan.do/replacement-strategy"
	clcReplacementStrategyNone  = "none"
)

// NodePoolManager defines an interface for managing node pools when performing
// update operations.
type NodePoolManager interface {
	GetPool(ctx context.Context, nodePool *api.NodePool) (*NodePool, error)
	MarkNodeForDecommission(ctx context.Context, node *Node) error
	AbortNodeDecommissioning(ctx context.Context, node *Node) error
	ScalePool(ctx context.Context, nodePool *api.NodePool, replicas int) error
	TerminateNode(ctx context.Context, nodePool *api.NodePool, node *Node, decrementDesired bool) error
	MarkPoolForDecommission(ctx context.Context, nodePool *api.NodePool) error
	DisableReplacementNodeProvisioning(ctx context.Context, node *Node) error
	CordonNode(ctx context.Context, node *Node) error
}

// DrainConfig contains the various settings for the smart node draining algorithm
type DrainConfig struct {
	// Start forcefully evicting pods <ForceEvictionGracePeriod> after node drain started
	ForceEvictionGracePeriod time.Duration

	// Only force evict pods that are at least <MinPodLifetime> old
	MinPodLifetime time.Duration

	// Wait until all healthy pods in the same PDB are at least <MinHealthyPDBSiblingCreationTime> old
	MinHealthyPDBSiblingLifetime time.Duration

	// Wait until all unhealthy pods in the same PDB are at least <MinUnhealthyPDBSiblingCreationTime> old
	MinUnhealthyPDBSiblingLifetime time.Duration

	// Wait at least <ForceEvictionInterval> between force evictions to allow controllers to catch up
	ForceEvictionInterval time.Duration

	// Wait for <PollInterval> between force eviction attempts
	PollInterval time.Duration
}

// KubernetesNodePoolManager defines a node pool manager which uses the
// Kubernetes API along with a node pool provider backend to manage node pools.
type KubernetesNodePoolManager struct {
	kube            kubernetes.Interface
	backend         ProviderNodePoolsBackend
	logger          *log.Entry
	drainConfig     *DrainConfig
	noScheduleTaint bool
}

// NewKubernetesNodePoolManager initializes a new Kubernetes NodePool manager
// which can manage single node pools based on the nodes registered in the
// Kubernetes API and the related NodePoolBackend for those nodes e.g.
// ASGNodePool.
func NewKubernetesNodePoolManager(logger *log.Entry, kubeClient kubernetes.Interface, poolBackend ProviderNodePoolsBackend, drainConfig *DrainConfig, noScheduleTaint bool) *KubernetesNodePoolManager {
	return &KubernetesNodePoolManager{
		kube:            kubeClient,
		backend:         poolBackend,
		logger:          logger,
		drainConfig:     drainConfig,
		noScheduleTaint: noScheduleTaint,
	}
}

// GetPool gets the current node Pool from the node pool backend and attaches
// the Kubernetes node object name and labels to the corresponding nodes.
func (m *KubernetesNodePoolManager) GetPool(ctx context.Context, nodePoolDesc *api.NodePool) (*NodePool, error) {
	nodePool, err := m.backend.Get(ctx, nodePoolDesc)
	if err != nil {
		return nil, fmt.Errorf("failed to get node pool details: %w", err)
	}

	var (
		kubeNodes *v1.NodeList
		kubeErr   error
	)

	backoffCfg := backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 60)
	if err := backoff.Retry(func() error {
		// TODO: labelselector based on nodePool name. Can't do it yet because of how we create node pools in CLM
		// https://github.com/zalando-incubator/cluster-lifecycle-manager/issues/226
		kubeNodes, kubeErr = m.kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		return kubeErr
	}, backoffCfg); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	instanceIDMap := make(map[string]v1.Node)
	for _, node := range kubeNodes.Items {
		instanceIDMap[node.Spec.ProviderID] = node
	}

	nodes := make([]*Node, 0, len(instanceIDMap))

	master := nodePoolDesc.IsMaster()

	for _, npNode := range nodePool.Nodes {
		if node, ok := instanceIDMap[npNode.ProviderID]; ok {
			n := &Node{
				ProviderID:      npNode.ProviderID,
				FailureDomain:   npNode.FailureDomain,
				Generation:      npNode.Generation,
				Ready:           npNode.Ready,
				Name:            node.Name,
				Annotations:     node.Annotations,
				Labels:          node.Labels,
				Taints:          node.Spec.Taints,
				Cordoned:        node.Spec.Unschedulable,
				VolumesAttached: len(node.Status.VolumesAttached) > 0,
				Master:          master,
			}

			// TODO(mlarsen): Think about how this could be
			// enabled. Currently it's not enabled because nodes
			// will be NotReady when flannel is not running,
			// meaning we can get stuck on initial provisioning
			// when the daemonset hasn't been submitted yet.
			// if n.Ready {
			// 	n.Ready = v1.IsNodeReady(&node)
			// }

			nodes = append(nodes, n)
		}
	}

	// if a node is not found in Kubernetes we don't consider it ready
	// and thus doesn't include it in the list of nodes
	nodePool.Current = len(nodes)
	nodePool.Nodes = nodes
	return nodePool, nil
}

func (m *KubernetesNodePoolManager) MarkNodeForDecommission(ctx context.Context, node *Node) error {
	taint := v1.TaintEffectPreferNoSchedule
	if m.noScheduleTaint && !node.Master {
		taint = v1.TaintEffectNoSchedule
	}
	err := m.taintNode(ctx, node, decommissionPendingTaintKey, decommissionPendingTaintValue, taint)
	if err != nil {
		return err
	}

	err = m.compareAndSetNodeLabel(ctx, node, lifecycleStatusLabel, lifecycleStatusReady, lifecycleStatusDecommissionPending)
	if err != nil {
		return err
	}
	return nil
}

func (m *KubernetesNodePoolManager) AbortNodeDecommissioning(ctx context.Context, node *Node) error {
	err := m.compareAndSetNodeLabel(ctx, node, lifecycleStatusLabel, lifecycleStatusDecommissionPending, lifecycleStatusReady)
	if err != nil {
		return err
	}
	return nil
}

func (m *KubernetesNodePoolManager) DisableReplacementNodeProvisioning(ctx context.Context, node *Node) error {
	return m.labelNode(ctx, node, clcReplacementStrategyLabel, clcReplacementStrategyNone)
}

func (m *KubernetesNodePoolManager) updateNode(ctx context.Context, node *Node, needsUpdate func(*Node) bool, patch func(*v1.Node) bool) error {
	// fast check: verify if already up-to-date
	if !needsUpdate(node) {
		return nil
	}

	taintNode := func() error {
		// re-fetch the node since we're going to do an update
		updatedNode, err := m.kube.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
		if err != nil {
			return backoff.Permanent(err)
		}

		if patch(updatedNode) {
			if _, err := m.kube.CoreV1().Nodes().Update(ctx, updatedNode, metav1.UpdateOptions{}); err != nil {
				// automatically retry if there was a conflicting update.
				serr, ok := err.(*apiErrors.StatusError)
				if ok && serr.Status().Reason == metav1.StatusReasonConflict {
					return err
				}

				return backoff.Permanent(err)
			}
		}

		return nil
	}

	backoffCfg := backoff.WithMaxRetries(backoff.NewConstantBackOff(1*time.Second), maxConflictRetries)
	return backoff.Retry(taintNode, backoffCfg)
}

// annotateNode annotates a Kubernetes node object in case the annotation is not already
// defined.
func (m *KubernetesNodePoolManager) annotateNode(ctx context.Context, node *Node, annotationKey, annotationValue string) error {
	return m.updateNode(
		ctx,
		node,
		func(node *Node) bool {
			value, ok := node.Annotations[annotationKey]
			return !ok || value != annotationValue
		},
		func(node *v1.Node) bool {
			if node.Annotations == nil {
				node.Annotations = make(map[string]string)
			}
			value, ok := node.Annotations[annotationKey]
			node.Annotations[annotationKey] = annotationValue
			return !ok || value != annotationValue
		})
}

// labelNode labels a Kubernetes node object in case the label is not already
// defined.
func (m *KubernetesNodePoolManager) labelNode(ctx context.Context, node *Node, labelKey, labelValue string) error {
	return m.updateNode(
		ctx,
		node,
		func(node *Node) bool {
			value, ok := node.Labels[labelKey]
			return !ok || value != labelValue
		},
		func(node *v1.Node) bool {
			if node.Labels == nil {
				node.Labels = make(map[string]string)
			}
			value, ok := node.Labels[labelKey]
			node.Labels[labelKey] = labelValue
			return !ok || value != labelValue
		})
}

// compareAndSetNodeLabel updates a label of a Kubernetes node object if the current value is set to `expectedValue` or
// not already defined.
func (m *KubernetesNodePoolManager) compareAndSetNodeLabel(ctx context.Context, node *Node, labelKey, expectedValue, newValue string) error {
	return m.updateNode(
		ctx,
		node,
		func(node *Node) bool {
			value, ok := node.Labels[labelKey]
			return !ok || value == expectedValue
		},
		func(node *v1.Node) bool {
			if node.Labels == nil {
				node.Labels = make(map[string]string)
			}
			value, ok := node.Labels[labelKey]
			if ok && value != expectedValue {
				return false
			}
			node.Labels[labelKey] = newValue
			return true
		})

}

// updateTaint adds a taint with the provided key, value and effect if it isn't present or
// updates an existing one. Returns true if anything was changed.
func updateTaint(node *v1.Node, taintKey, taintValue string, effect v1.TaintEffect) bool {
	for i, taint := range node.Spec.Taints {
		if taint.Key == taintKey {
			if taint.Value == taintValue && taint.Effect == effect {
				return false
			}

			node.Spec.Taints[i].Value = taintValue
			node.Spec.Taints[i].Effect = effect
			return true
		}
	}

	node.Spec.Taints = append(node.Spec.Taints, v1.Taint{
		Key:    taintKey,
		Value:  taintValue,
		Effect: effect,
	})
	return true
}

// TaintNode sets a taint on a Kubernetes node object with a specified value and effect.
func (m *KubernetesNodePoolManager) taintNode(ctx context.Context, node *Node, taintKey, taintValue string, effect v1.TaintEffect) error {
	return m.updateNode(
		ctx,
		node,
		func(node *Node) bool {
			for _, taint := range node.Taints {
				if taint.Key == taintKey && taint.Value == taintValue && taint.Effect == effect {
					return false
				}
			}
			return true
		},
		func(node *v1.Node) bool {
			return updateTaint(node, taintKey, taintValue, effect)
		})
}

// TerminateNode terminates a node and optionally decrement the desired size of
// the node pool. Before a node is terminated it's drained to ensure that pods
// running on the nodes are gracefully terminated.
func (m *KubernetesNodePoolManager) TerminateNode(ctx context.Context, nodePool *api.NodePool, node *Node, decrementDesired bool) error {
	err := m.drain(ctx, node)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	m.logger.WithField("node", node.Name).Info("Terminating node")

	return m.backend.Terminate(ctx, nodePool, node, decrementDesired)
}

func (m *KubernetesNodePoolManager) MarkPoolForDecommission(ctx context.Context, nodePool *api.NodePool) error {
	return m.backend.MarkForDecommission(ctx, nodePool)
}

// ScalePool scales a nodePool to the specified number of replicas.
// On scale down it will attempt to do it gracefully by draining the nodes
// before terminating them.
func (m *KubernetesNodePoolManager) ScalePool(ctx context.Context, nodePool *api.NodePool, replicas int) error {
	var pool *NodePool
	var err error

	// in case we are scaling down to 0 replicas, disable the autoscaler to
	// not fight with it.
	if replicas == 0 {
		err := m.backend.MarkForDecommission(ctx, nodePool)
		if err != nil {
			return err
		}
	}

	for {
		pool, err = WaitForDesiredNodes(ctx, m.logger, m, nodePool)
		if err != nil {
			return err
		}

		if pool.Current > replicas && replicas != 0 {
			return fmt.Errorf("refusing to scale down: current %d, desired %d nodes", pool.Current, replicas)
		}

		// mark nodes to be removed
		if replicas == 0 {
			for _, node := range pool.Nodes {
				err := m.MarkNodeForDecommission(ctx, node)
				if err != nil {
					return err
				}
			}
		}

		if pool.Current < replicas {
			return m.backend.Scale(ctx, nodePool, replicas)
		} else if pool.Current > replicas {
			// pick a random node to terminate
			if len(pool.Nodes) < 1 {
				return errors.New("expected at least 1 node in the node pool, found 0")
			}
			node := pool.Nodes[0]

			// if there are already cordoned nodes prefer one of those
			cordonedNodes := filterNodesToTerminate(pool.Nodes)
			if len(cordonedNodes) > 0 {
				node = cordonedNodes[0]
			}

			err := m.CordonNode(ctx, node)
			if err != nil {
				return err
			}

			err = m.TerminateNode(ctx, nodePool, node, true)
			if err != nil {
				return err
			}

			continue
		}
		return nil
	}
}

// CordonNode marks a node unschedulable.
func (m *KubernetesNodePoolManager) CordonNode(ctx context.Context, node *Node) error {
	unschedulable := []byte(`{"spec": {"unschedulable": true}}`)
	_, err := m.kube.CoreV1().Nodes().Patch(ctx, node.Name, types.StrategicMergePatchType, unschedulable, metav1.PatchOptions{})
	return err
}

// WaitForDesiredNodes waits for the current number of nodes to match the
// desired number. The final node pool will be returned.
func WaitForDesiredNodes(ctx context.Context, logger *log.Entry, n NodePoolManager, nodePoolDesc *api.NodePool) (*NodePool, error) {
	ctx, cancel := context.WithTimeout(ctx, operationMaxTimeout)
	defer cancel()

	var err error
	var nodePool *NodePool

	for {
		nodePool, err = n.GetPool(ctx, nodePoolDesc)
		if err != nil {
			return nil, err
		}

		readyNodes := len(nodePool.ReadyNodes())

		if readyNodes == nodePool.Desired {
			break
		}

		// Don't wait for Spot nodes, just proceed with whatever we want to do
		if nodePoolDesc.IsSpot() {
			break
		}

		logger.WithFields(log.Fields{"node-pool": nodePoolDesc.Name}).
			Infof("Waiting for ready and desired number of nodes to match: %d/%d", readyNodes, nodePool.Desired)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(operationCheckInterval):
		}
	}

	return nodePool, nil
}
