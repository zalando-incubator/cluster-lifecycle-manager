package updatestrategy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	policy "k8s.io/client-go/pkg/apis/policy/v1beta1"
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
)

// NodePoolManager defines an interface for managing node pools when performing
// update operations.
type NodePoolManager interface {
	GetPool(nodePool *api.NodePool) (*NodePool, error)
	LabelNode(node *Node, labelKey, labelValue string) error
	TaintNode(node *Node, taintKey, taintValue string, effect v1.TaintEffect) error
	ScalePool(ctx context.Context, nodePool *api.NodePool, replicas int) error
	TerminateNode(ctx context.Context, node *Node, decrementDesired bool) error
	CordonNode(node *Node) error
}

// KubernetesNodePoolManager defines a node pool manager which uses the
// Kubernetes API along with a node pool provider backend to manage node pools.
type KubernetesNodePoolManager struct {
	kube            kubernetes.Interface
	backend         ProviderNodePoolsBackend
	logger          *log.Entry
	maxEvictTimeout time.Duration
}

// NewKubernetesNodePoolManager initializes a new Kubernetes NodePool manager
// which can manage single node pools based on the nodes registered in the
// Kubernetes API and the related NodePoolBackend for those nodes e.g.
// ASGNodePool.
func NewKubernetesNodePoolManager(logger *log.Entry, kubeClient kubernetes.Interface, poolBackend ProviderNodePoolsBackend, maxEvictTimeout time.Duration) *KubernetesNodePoolManager {
	return &KubernetesNodePoolManager{
		kube:            kubeClient,
		backend:         poolBackend,
		logger:          logger,
		maxEvictTimeout: maxEvictTimeout,
	}
}

// GetPool gets the current node Pool from the node pool backend and attaches
// the Kubernetes node object name and labels to the corresponding nodes.
func (m *KubernetesNodePoolManager) GetPool(nodePoolDesc *api.NodePool) (*NodePool, error) {
	nodePool, err := m.backend.Get(nodePoolDesc)
	if err != nil {
		return nil, err
	}

	// TODO: labelselector based on nodePool name. Can't do it yet because of how we create node pools in CLM
	// https://github.com/zalando-incubator/cluster-lifecycle-manager/issues/226
	kubeNodes, err := m.kube.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	instanceIDMap := make(map[string]v1.Node)
	for _, node := range kubeNodes.Items {
		instanceIDMap[node.Spec.ProviderID] = node
	}

	nodes := make([]*Node, 0, len(instanceIDMap))

	for _, npNode := range nodePool.Nodes {
		if node, ok := instanceIDMap[npNode.ProviderID]; ok {
			n := &Node{
				ProviderID:      npNode.ProviderID,
				FailureDomain:   npNode.FailureDomain,
				Generation:      npNode.Generation,
				Ready:           npNode.Ready,
				Name:            node.Name,
				Labels:          node.Labels,
				Taints:          node.Spec.Taints,
				Cordoned:        node.Spec.Unschedulable,
				VolumesAttached: len(node.Status.VolumesAttached) > 0,
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

// LabelNode labels a Kubernetes node object in case the label is not already
// defined.
func (m *KubernetesNodePoolManager) LabelNode(node *Node, labelKey, labelValue string) error {
	if value, ok := node.Labels[labelKey]; !ok || value != labelValue {
		label := []byte(fmt.Sprintf(`{"metadata": {"labels": {"%s": "%s"}}}`, labelKey, labelValue))
		_, err := m.kube.CoreV1().Nodes().Patch(node.Name, types.StrategicMergePatchType, label)
		if err != nil {
			return err
		}
	}
	return nil
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
func (m *KubernetesNodePoolManager) TaintNode(node *Node, taintKey, taintValue string, effect v1.TaintEffect) error {
	// fast check: verify if the taint is already set
	for _, taint := range node.Taints {
		if taint.Key == taintKey && taint.Value == taintValue && taint.Effect == effect {
			return nil
		}
	}

	taintNode := func() error {
		// re-fetch the node since we're going to do an update
		updatedNode, err := m.kube.CoreV1().Nodes().Get(node.Name, metav1.GetOptions{})
		if err != nil {
			return backoff.Permanent(err)
		}

		if updateTaint(updatedNode, taintKey, taintValue, effect) {
			_, err := m.kube.CoreV1().Nodes().Update(updatedNode)
			if err != nil {
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

	backoffCfg := backoff.WithMaxTries(backoff.NewConstantBackOff(1*time.Second), maxConflictRetries)
	return backoff.Retry(taintNode, backoffCfg)
}

// TerminateNode terminates a node and optionally decrement the desired size of
// the node pool. Before a node is terminated it's drained to ensure that pods
// running on the nodes are gracefully terminated.
func (m *KubernetesNodePoolManager) TerminateNode(ctx context.Context, node *Node, decrementDesired bool) error {
	err := m.drain(ctx, node)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	err = m.backend.Terminate(node, decrementDesired)
	if err != nil {
		return err
	}

	// remove the node resource from Kubernetes. This ensures that the same
	// empty node is not drained multiple times.
	return m.kube.CoreV1().Nodes().Delete(node.Name, nil)
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
		err := m.backend.SuspendAutoscaling(nodePool)
		if err != nil {
			return err
		}
	}

	for {
		pool, err = WaitForDesiredNodes(ctx, m.logger, m, nodePool)
		if err != nil {
			return err
		}

		if pool.Current < replicas {
			return m.backend.Scale(nodePool, replicas)
		}

		if pool.Current > replicas {
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

			err := m.CordonNode(node)
			if err != nil {
				return err
			}

			err = m.TerminateNode(ctx, node, true)
			if err != nil {
				return err
			}

			continue
		}
		return nil
	}
}

// drain tries to evict all of the pods on a node.
// pods are evicted in parallel.
func (m *KubernetesNodePoolManager) drain(ctx context.Context, node *Node) error {
	m.logger.WithField("node", node.Name).Info("Draining node")

	// mark node as draining
	if err := m.LabelNode(node, lifecycleStatusLabel, lifecycleStatusDraining); err != nil {
		return err
	}

	pods, err := m.getPodsByNode(node.Name)
	if err != nil {
		return err
	}

	errors := make(chan error, len(pods.Items))
	for _, pod := range pods.Items {
		go func(pod v1.Pod, errc chan error) {
			evictPod := func() error {
				// we check at the start because there's a continue in the loop body
				err := ctx.Err()
				if err != nil {
					return backoff.Permanent(err)
				}

				// Don't bother with this pod if it's not evictable.
				if !m.isEvictablePod(pod) {
					return nil
				}

				err = evictPod(m.kube, m.logger, &pod)
				if err != nil {
					if apiErrors.IsTooManyRequests(err) || isMultiplePDBsErr(err) {
						m.logger.WithFields(log.Fields{
							"ns":   pod.Namespace,
							"pod":  pod.Name,
							"node": pod.Spec.NodeName,
						}).Info("Pod Disruption Budget violated")
					}
					return err
				}
				return nil
			}

			// We try to evict all pods of a node by calling evict on all of them once. If we encounter an
			// error we will backoff and try again for as long as `maxEvictTimeout`. If after `maxEvictTimeout`
			// we still receive an error related to pod disruption budget violations we will continue and
			// forcefully shutdown the pod in the next step.
			backoffCfg := backoff.NewExponentialBackOff()
			backoffCfg.MaxElapsedTime = m.maxEvictTimeout
			err := backoff.Retry(evictPod, backoffCfg)
			if err != nil {
				if !apiErrors.IsTooManyRequests(err) && !isMultiplePDBsErr(err) {
					errc <- err
					return
				}
			}
			errc <- nil
		}(pod, errors)
	}

	failedEvictions := 0
	for _, pod := range pods.Items {
		err := <-errors
		if err != nil {
			m.logger.WithFields(log.Fields{
				"ns":   pod.Namespace,
				"pod":  pod.Name,
				"node": pod.Spec.NodeName,
			}).Errorf("Failed to evict pod: %v", err)
			failedEvictions++
		}
	}

	if failedEvictions > 0 {
		return fmt.Errorf("failed to evict %d pods from node '%s'", failedEvictions, node.Name)
	}

	// Delete all remaining evictable pods disregarding their pod disruption budgets. It's necessary
	// in case a pod disruption budget must be violated in order to proceed with a cluster update.
	pods, err = m.getPodsByNode(node.Name)
	if err != nil {
		return err
	}

	errors = make(chan error, len(pods.Items))
	for _, pod := range pods.Items {
		go func(pod v1.Pod, errc chan error) {
			// Don't bother with this pod if it's not evictable.
			if !m.isEvictablePod(pod) {
				errc <- nil
				return
			}

			err := m.kube.CoreV1().Pods(pod.Namespace).Delete(pod.Name, &metav1.DeleteOptions{
				GracePeriodSeconds: pod.Spec.TerminationGracePeriodSeconds,
			})
			if err != nil {
				errc <- err
				return
			}

			if err = ctx.Err(); err != nil {
				errc <- err
				return
			}

			logger := m.logger.WithFields(log.Fields{
				"ns":   pod.Namespace,
				"pod":  pod.Name,
				"node": pod.Spec.NodeName,
			})

			// wait for pod to be terminated and gone from the node.
			err = waitForPodTermination(m.kube, pod)
			if err != nil {
				logger.Warnf("Pod not terminated within grace period: %s", err)
			}

			logger.Info("Pod deleted")
			errc <- nil
		}(pod, errors)
	}

	failedDeletions := 0
	for _, pod := range pods.Items {
		err := <-errors
		if err != nil {
			m.logger.WithFields(log.Fields{
				"ns":   pod.Namespace,
				"pod":  pod.Name,
				"node": pod.Spec.NodeName,
			}).Errorf("Failed to delete pod: %v", err)
			failedDeletions++
		}
	}

	if failedDeletions > 0 {
		return fmt.Errorf("failed to delete %d pods from node '%s'", failedDeletions, node.Name)
	}

	return nil
}

// isMultiplePDBsErr returns true if the error is caused by multiple PDBs
// defined for a single pod.
func isMultiplePDBsErr(err error) bool {
	return strings.Contains(err.Error(), multiplePDBsErrMsg)
}

// evictPod tries to evict a pod from a node.
// Note: this is defined as a variable so it can be easily mocked in tests.
var evictPod = func(client kubernetes.Interface, logger *log.Entry, pod *v1.Pod) error {
	localLogger := logger.WithFields(log.Fields{
		"ns":   pod.Namespace,
		"pod":  pod.Name,
		"node": pod.Spec.NodeName,
	})

	eviction := &policy.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{
			GracePeriodSeconds: pod.Spec.TerminationGracePeriodSeconds,
		},
	}

	err := client.CoreV1().Pods(pod.Namespace).Evict(eviction)
	if err != nil {
		return err
	}
	localLogger.Info("Evicting pod")

	// wait for the pod to be actually evicted and gone from the node.
	// It has TerminationGracePeriodSeconds time to clean up.
	start := time.Now().UTC()
	err = waitForPodTermination(client, *pod)
	if err != nil {
		localLogger.Warnf("Pod not terminated within grace period: %s", err)
	}

	localLogger.Infof("Pod evicted. (Observed termination period: %s)", time.Now().UTC().Sub(start))
	return nil
}

// waitForPodTermination waits for a pod to be terminated by looking up the pod
// in the API server.
// It waits for up to TerminationGracePeriodSeconds as specified on the pod +
// an additional eviction head room.
// This is to fully respect the termination expectations as described in:
// https://kubernetes.io/docs/concepts/workloads/pods/pod/#termination-of-pods
func waitForPodTermination(client kubernetes.Interface, pod v1.Pod) error {
	if pod.Spec.TerminationGracePeriodSeconds == nil {
		// if no grace period is defined, we don't wait.
		return nil
	}

	waitForTermination := func() error {
		newpod, err := client.CoreV1().Pods(pod.Namespace).Get(pod.Name, metav1.GetOptions{})
		if err != nil {
			if apiErrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		// statefulset pods have the same name after restart, check the uid as well
		if newpod.GetObjectMeta().GetUID() == pod.GetObjectMeta().GetUID() {
			return fmt.Errorf("pod not terminated")
		}

		return nil
	}

	gracePeriod := time.Duration(*pod.Spec.TerminationGracePeriodSeconds)*time.Second + podEvictionHeadroom

	backoffCfg := backoff.NewExponentialBackOff()
	backoffCfg.MaxElapsedTime = gracePeriod
	return backoff.Retry(waitForTermination, backoffCfg)
}

// CordonNode marks a node unschedulable.
func (m *KubernetesNodePoolManager) CordonNode(node *Node) error {
	unschedulable := []byte(`{"spec": {"unschedulable": true}}`)
	_, err := m.kube.CoreV1().Nodes().Patch(node.Name, types.StrategicMergePatchType, unschedulable)
	return err
}

// getPodsByNode returns all pods currently scheduled to a node, regardless of their status.
func (m *KubernetesNodePoolManager) getPodsByNode(nodeName string) (*v1.PodList, error) {
	opts := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	}

	return m.kube.CoreV1().Pods(v1.NamespaceAll).List(opts)
}

// isEvictablePod detects whether it makes sense to evict a pod.
// Non-evictable pods are pods managed by DaemonSets and mirror pods.
func (m *KubernetesNodePoolManager) isEvictablePod(pod v1.Pod) bool {
	logger := m.logger.WithFields(log.Fields{
		"ns":   pod.Namespace,
		"pod":  pod.Name,
		"node": pod.Spec.NodeName,
	})

	if _, ok := pod.Annotations[mirrorPodAnnotation]; ok {
		logger.Debug("Mirror Pod not evictable")
		return false
	}

	if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
		logger.Debug("Terminated pod (%s) not evictable", pod.Status.Phase)
		return false
	}

	for _, owner := range pod.GetOwnerReferences() {
		if owner.Kind == "DaemonSet" {
			logger.Debug("DaemonSet Pod not evictable")
			return false
		}
	}

	return true
}

// WaitForDesiredNodes waits for the current number of nodes to match the
// desired number. The final node pool will be returned.
func WaitForDesiredNodes(ctx context.Context, logger *log.Entry, n NodePoolManager, nodePoolDesc *api.NodePool) (*NodePool, error) {
	ctx, cancel := context.WithTimeout(ctx, operationMaxTimeout)
	defer cancel()

	var err error
	var nodePool *NodePool

	for {
		nodePool, err = n.GetPool(nodePoolDesc)
		if err != nil {
			return nil, err
		}

		readyNodes := len(nodePool.ReadyNodes())

		if readyNodes == nodePool.Desired {
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
