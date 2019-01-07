package updatestrategy

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

const (
	drainStartAnnotation      = "cluster-lifecycle-manager.zalando.org/drain-start"
	lastForcedDrainAnnotation = "cluster-lifecycle-manager.zalando.org/last-forced-drain"
)

func timestampAnnotation(logger *log.Entry, node *Node, annotation string, fallback time.Time) time.Time {
	ts, ok := node.Annotations[annotation]
	if !ok {
		return fallback
	}

	value, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		logger.Warnf("invalid value for %s (will use %s): %v", annotation, fallback, err)
		return fallback
	}

	return value
}

// drain tries to cleanly evict all of the pods on a node, and then forcibly terminates the remaining ones.
func (m *KubernetesNodePoolManager) drain(ctx context.Context, node *Node) error {
	m.logger.WithField("node", node.Name).Info("Draining node")

	err := m.labelNode(node, lifecycleStatusLabel, lifecycleStatusDraining)
	if err != nil {
		return err
	}

	drainStart := timestampAnnotation(m.logger, node, drainStartAnnotation, time.Now())
	err = m.annotateNode(node, drainStartAnnotation, drainStart.Format(time.RFC3339))
	if err != nil {
		return err
	}

	lastForcedTermination := timestampAnnotation(m.logger, node, lastForcedDrainAnnotation, time.Unix(0, 0))

	// fast eviction (in parallel) as long as we can evict something without violating PDBs
	for {
		err = ctx.Err()
		if err != nil {
			return err
		}

		pods, err := m.evictablePods(node.Name)
		if err != nil {
			return err
		}

		if len(pods) == 0 {
			return nil
		}

		evicted, err := m.evictParallel(ctx, pods)
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			return err
		}
		if !evicted {
			break
		}
		time.Sleep(m.drainConfig.PollInterval)
	}

	// slow eviction, one by one
	for {
		err = ctx.Err()
		if err != nil {
			return err
		}

		pods, err := m.evictablePods(node.Name)
		if err != nil {
			return err
		}

		if len(pods) == 0 {
			return nil
		}

		forceEvicted, err := m.evictOrForceTerminatePod(ctx, pods, drainStart, lastForcedTermination)
		if err != nil {
			return err
		}

		if forceEvicted {
			lastForcedTermination = time.Now()
			err := m.annotateNode(node, lastForcedDrainAnnotation, lastForcedTermination.Format(time.RFC3339))
			if err != nil {
				return err
			}
		}

		time.Sleep(m.drainConfig.PollInterval)
	}
}

func (m *KubernetesNodePoolManager) evictParallel(ctx context.Context, pods []v1.Pod) (bool, error) {
	var evicted int64
	var group errgroup.Group

	for _, pod := range pods {
		pod := pod
		group.Go(func() error {
			err := evictPod(m.kube, m.logger, pod)
			if err != nil {
				if isPDBViolation(err) {
					m.logPdbViolated(pod)
					return nil
				}
				return err
			}

			atomic.AddInt64(&evicted, 1)
			return nil
		})
	}

	err := group.Wait()
	return atomic.LoadInt64(&evicted) > 0, err
}

func (m *KubernetesNodePoolManager) podLogger(pod v1.Pod) *log.Entry {
	return m.logger.WithFields(log.Fields{
		"ns":   pod.Namespace,
		"pod":  pod.Name,
		"node": pod.Spec.NodeName,
	})
}

func (m *KubernetesNodePoolManager) logPdbViolated(pod v1.Pod) {
	m.podLogger(pod).Info("Pod Disruption Budget violated")
}

func (m *KubernetesNodePoolManager) evictOrForceTerminatePod(ctx context.Context, pods []v1.Pod, drainStart, lastForcedTermination time.Time) (bool, error) {
	for _, pod := range pods {
		err := ctx.Err()
		if err != nil {
			return false, err
		}

		// try evicting normally
		err = evictPod(m.kube, m.logger, pod)
		if err == nil {
			return false, nil
		}
		if !isPDBViolation(err) {
			return false, err
		}

		// PDB violation, log and check if we can force terminate the pod
		m.logPdbViolated(pod)

		forceTerminate, _ := m.forceTerminationAllowed(pod, time.Now(), drainStart, lastForcedTermination)
		if forceTerminate {
			err = deletePod(m.kube, m.podLogger(pod), pod)
			if err != nil {
				return false, err
			}
			return true, nil
		}
	}

	return false, nil
}

func (m *KubernetesNodePoolManager) forceTerminationAllowed(pod v1.Pod, now, drainStart, lastForcedTermination time.Time) (bool, error) {
	// too early to start force terminating
	if now.Before(drainStart.Add(m.drainConfig.ForceEvictionGracePeriod)) {
		m.podLogger(pod).Debug("Won't force terminate (node in grace period)")
		return false, nil
	}

	// we've recently force killed a pod
	if now.Before(lastForcedTermination.Add(m.drainConfig.ForceEvictionInterval)) {
		m.podLogger(pod).Debug("Won't force terminate (recently force terminated a pod)")
		return false, nil
	}

	// pod too young
	if now.Before(pod.GetCreationTimestamp().Add(m.drainConfig.MinPodLifetime)) {
		m.podLogger(pod).Debug("Won't force terminate (pod too young)")
		return false, nil
	}

	// find all other pods matched by the same PDBs
	allPods, err := m.getPodsByNamespace(pod.GetNamespace())
	if err != nil {
		return false, err
	}
	allPdbs, err := m.getPDBsByNamespace(pod.GetNamespace())
	if err != nil {
		return false, err
	}
	siblingPods := findSiblingPods(m.logger, pod, allPods.Items, allPdbs.Items)

	// check if PDB siblings are old enough
	siblingsOldEnough := true
	for _, siblingPod := range siblingPods {
		waitTime := m.drainConfig.MinUnhealthyPDBSiblingLifetime
		if podReady(siblingPod) {
			waitTime = m.drainConfig.MinHealthyPDBSiblingLifetime
		}
		if now.Before(siblingPod.GetCreationTimestamp().Add(waitTime)) {
			siblingsOldEnough = false
			break
		}
	}
	if !siblingsOldEnough {
		var siblingNames []string
		for _, siblingPod := range siblingPods {
			siblingNames = append(siblingNames, siblingPod.GetName())
		}
		m.podLogger(pod).Debugf("Won't force terminate (siblings %s too young)", strings.Join(siblingNames, ", "))
		return false, nil
	}

	return true, nil
}

func podReady(pod v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}

func findSiblingPods(logger *log.Entry, pod v1.Pod, allPods []v1.Pod, allPdbs []policy.PodDisruptionBudget) []v1.Pod {
	var siblingSelectors []labels.Selector
	for _, pdb := range allPdbs {
		selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
		if err != nil {
			logger.Debugf("pdb %s/%s has an invalid selector: %v", pod.GetNamespace(), pdb.GetName(), err)
			continue
		}
		if selector.Matches(labels.Set(pod.Labels)) {
			siblingSelectors = append(siblingSelectors, selector)
		}
	}

	var result []v1.Pod
	for _, candidate := range allPods {
		if candidate.GetName() == pod.GetName() {
			continue
		}
		if ignoredSiblingPod(&candidate) {
			continue
		}

		for _, selector := range siblingSelectors {
			if selector.Matches(labels.Set(candidate.Labels)) {
				result = append(result, candidate)
				break
			}
		}
	}

	return result
}

// ignoredSiblingPod returns true if a sibling pod should be ignored (e.g. because it's a cronjob pod)
func ignoredSiblingPod(pod *v1.Pod) bool {
	// terminated pod, ignore
	if podTerminated(pod) {
		return true
	}

	for _, owner := range pod.GetObjectMeta().GetOwnerReferences() {
		// job/cronjob pod, ignore
		if owner.Kind == "Job" {
			return true
		}
	}

	return false
}

func isPDBViolation(err error) bool {
	return apiErrors.IsTooManyRequests(err) || strings.Contains(err.Error(), multiplePDBsErrMsg)
}

var deletePod = func(client kubernetes.Interface, logger *log.Entry, pod v1.Pod) error {
	err := client.CoreV1().Pods(pod.Namespace).Delete(pod.Name, &metav1.DeleteOptions{
		GracePeriodSeconds: pod.Spec.TerminationGracePeriodSeconds,
	})
	if err != nil {
		if apiErrors.IsNotFound(err) {
			return nil
		}
		logger.Errorf("Failed to delete pod: %v", err)
		return err
	}

	// wait for pod to be terminated and gone from the node.
	err = waitForPodTermination(client, pod)
	if err != nil {
		logger.Warnf("Pod not terminated within grace period: %s", err)
	}

	logger.Info("Pod deleted")
	return nil

}

func podTerminated(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed
}

// evictPod tries to evict a pod from a node.
// Note: this is defined as a variable so it can be easily mocked in tests.
var evictPod = func(client kubernetes.Interface, logger *log.Entry, pod v1.Pod) error {
	localLogger := logger.WithFields(log.Fields{
		"ns":   pod.Namespace,
		"pod":  pod.Name,
		"node": pod.Spec.NodeName,
	})

	updated, err := client.CoreV1().Pods(pod.Namespace).Get(pod.Name, metav1.GetOptions{})
	if err != nil {
		if apiErrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if podTerminated(updated) {
		// Completed, just ignore
		return nil
	}

	if updated.Spec.NodeName != pod.Spec.NodeName {
		// Already evicted
		return nil
	}

	eviction := &policy.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: &metav1.DeleteOptions{
			GracePeriodSeconds: pod.Spec.TerminationGracePeriodSeconds,
		},
	}

	err = client.CoreV1().Pods(pod.Namespace).Evict(eviction)
	if err != nil {
		if apiErrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	localLogger.Info("Evicting pod")

	// wait for the pod to be actually evicted and gone from the node.
	// It has TerminationGracePeriodSeconds time to clean up.
	start := time.Now().UTC()
	err = waitForPodTermination(client, pod)
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

// evictablePods returns all evictable pods currently scheduled to a node, regardless of their status.
func (m *KubernetesNodePoolManager) evictablePods(nodeName string) ([]v1.Pod, error) {
	opts := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	}

	podList, err := m.kube.CoreV1().Pods(v1.NamespaceAll).List(opts)
	if err != nil {
		return nil, err
	}

	var result []v1.Pod
	for _, pod := range podList.Items {
		if m.isEvictablePod(pod) {
			result = append(result, pod)
		}
	}

	return result, nil
}

func (m *KubernetesNodePoolManager) getPodsByNamespace(namespace string) (*v1.PodList, error) {
	return m.kube.CoreV1().Pods(namespace).List(metav1.ListOptions{})
}

func (m *KubernetesNodePoolManager) getPDBsByNamespace(namespace string) (*policy.PodDisruptionBudgetList, error) {
	return m.kube.PolicyV1beta1().PodDisruptionBudgets(namespace).List(metav1.ListOptions{})
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
		logger.Debug("Pod terminated")
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
