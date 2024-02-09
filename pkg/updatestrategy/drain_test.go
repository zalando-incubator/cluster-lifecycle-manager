package updatestrategy

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	testNodeName = "test-node"
)

func removePod(ctx context.Context, client kubernetes.Interface, pod v1.Pod) error {
	var zero int64
	_ = client.CoreV1().Pods(pod.GetNamespace()).Delete(ctx, pod.GetName(), metav1.DeleteOptions{GracePeriodSeconds: &zero})
	return nil
}

func evictPodFailPDB(_ context.Context, _ kubernetes.Interface, _ *log.Entry, _ v1.Pod) error {
	return &errors.StatusError{
		ErrStatus: metav1.Status{
			Code: http.StatusTooManyRequests,
		},
	}
}

func TestTerminateNode(t *testing.T) {
	evictPod = func(ctx context.Context, client kubernetes.Interface, _ *log.Entry, pod v1.Pod) error {
		return removePod(ctx, client, pod)
	}

	pods := []*v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "a",
				Namespace: "default",
			},
			Spec: v1.PodSpec{
				NodeName: testNodeName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "b",
				Namespace: "default",
				Annotations: map[string]string{
					mirrorPodAnnotation: "",
				},
			},
			Spec: v1.PodSpec{
				NodeName: testNodeName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "c",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "DaemonSet",
					},
				},
			},
			Spec: v1.PodSpec{
				NodeName: testNodeName,
			},
		},
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNodeName,
		},
	}

	logger := log.WithField("test", true)
	backend := &mockProviderNodePoolsBackend{
		nodePool: &NodePool{
			Min:        1,
			Max:        1,
			Current:    1,
			Desired:    1,
			Generation: 1,
			Nodes: []*Node{
				{ProviderID: "provider-id"},
			},
		},
	}
	mgr := &KubernetesNodePoolManager{
		logger:  logger,
		kube:    setupMockKubernetes(context.Background(), t, []*v1.Node{node}, pods, nil),
		backend: backend,
		drainConfig: &DrainConfig{
			PollInterval: time.Second,
		},
	}

	err := mgr.TerminateNode(context.Background(), &Node{Name: node.Name}, false)
	assert.NoError(t, err)

	// test when evictPod returns 429
	evictPod = evictPodFailPDB

	mgr.kube = setupMockKubernetes(context.Background(), t, []*v1.Node{node}, pods, nil)
	err = mgr.TerminateNode(context.Background(), &Node{Name: node.Name}, false)
	assert.NoError(t, err)
}

func TestTerminateNodeCancelled(t *testing.T) {
	testNodeName := "test"

	pods := []*v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "a",
				Namespace: "default",
			},
			Spec: v1.PodSpec{
				NodeName: testNodeName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "b",
				Namespace: "default",
				Annotations: map[string]string{
					mirrorPodAnnotation: "",
				},
			},
			Spec: v1.PodSpec{
				NodeName: testNodeName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "c",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "DaemonSet",
					},
				},
			},
			Spec: v1.PodSpec{
				NodeName: testNodeName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "d",
				Namespace: "default",
			},
			Spec: v1.PodSpec{
				NodeName: testNodeName,
			},
		},
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNodeName,
		},
	}

	logger := log.WithField("test", true)
	backend := &mockProviderNodePoolsBackend{
		nodePool: &NodePool{
			Min:        1,
			Max:        1,
			Current:    1,
			Desired:    1,
			Generation: 1,
			Nodes: []*Node{
				{ProviderID: "provider-id"},
			},
		},
	}

	// immediate cancellation
	{
		var evictCount int32

		mgr := &KubernetesNodePoolManager{
			logger:      logger,
			kube:        setupMockKubernetes(context.Background(), t, []*v1.Node{node}, pods, nil),
			backend:     backend,
			drainConfig: &DrainConfig{},
		}

		evictPod = func(_ context.Context, client kubernetes.Interface, logger *log.Entry, pod v1.Pod) error {
			atomic.AddInt32(&evictCount, 1)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := mgr.TerminateNode(ctx, &Node{Name: node.Name}, false)
		evictCountFinal := atomic.LoadInt32(&evictCount)
		assert.Zero(t, evictCountFinal)
		assert.EqualValues(t, err, context.Canceled)
	}

	// cancel after first pod
	{
		var evictCount int32
		blockEviction := sync.WaitGroup{}
		blockHelper := sync.WaitGroup{}
		blockEviction.Add(1)
		// add two to the wg counter because we have two pods that will be
		// evicted in parallel.
		blockHelper.Add(2)

		evictPod = func(ctx context.Context, client kubernetes.Interface, _ *log.Entry, pod v1.Pod) error {
			// unblock so we can be cancelled
			blockHelper.Done()
			// wait until we're unblocked
			blockEviction.Wait()
			atomic.AddInt32(&evictCount, 1)
			return removePod(ctx, client, pod)
		}

		mgr := &KubernetesNodePoolManager{
			logger:      logger,
			kube:        setupMockKubernetes(context.Background(), t, []*v1.Node{node}, pods, nil),
			backend:     backend,
			drainConfig: &DrainConfig{},
		}

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			// wait until evict() unblocks us
			blockHelper.Wait()
			cancel()
			// unblock evict()
			blockEviction.Done()
		}()

		err := mgr.TerminateNode(ctx, &Node{Name: node.Name}, false)

		evictCountFinal := atomic.LoadInt32(&evictCount)
		assert.Equal(t, int32(2), evictCountFinal)
		assert.EqualValues(t, err, context.Canceled)
	}

	// cancel during slow eviction
	{
		var deleteCount int32
		evictPod = evictPodFailPDB

		mgr := &KubernetesNodePoolManager{
			logger:  logger,
			kube:    setupMockKubernetes(context.Background(), t, []*v1.Node{node}, pods, nil),
			backend: backend,
			drainConfig: &DrainConfig{
				ForceEvictionInterval: time.Minute,
				PollInterval:          time.Second,
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		deletePod = func(_ context.Context, client kubernetes.Interface, logger *log.Entry, pod v1.Pod) error {
			atomic.AddInt32(&deleteCount, 1)
			cancel()
			return nil
		}
		err := mgr.TerminateNode(ctx, &Node{Name: node.Name}, false)

		deleteCountFinal := atomic.LoadInt32(&deleteCount)
		assert.EqualValues(t, int32(1), deleteCountFinal)
		assert.EqualValues(t, err, context.Canceled)
	}
}

var testDrainConfig = &DrainConfig{
	ForceEvictionGracePeriod:       10 * time.Second,
	MinPodLifetime:                 20 * time.Second,
	MinHealthyPDBSiblingLifetime:   30 * time.Second,
	MinUnhealthyPDBSiblingLifetime: 40 * time.Second,
	ForceEvictionInterval:          5 * time.Second,
}

func forceTerminationAllowed(t *testing.T, pods []*v1.Pod, pdbs []*policy.PodDisruptionBudget, now, drainStart, lastForcedTermination time.Time) (bool, error) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNodeName,
		},
	}

	backend := &mockProviderNodePoolsBackend{
		nodePool: &NodePool{
			Min:        1,
			Max:        1,
			Current:    1,
			Desired:    1,
			Generation: 1,
			Nodes: []*Node{
				{ProviderID: "provider-id"},
			},
		},
	}

	mgr := &KubernetesNodePoolManager{
		logger:      log.WithField("test", true),
		kube:        setupMockKubernetes(context.Background(), t, []*v1.Node{node}, pods, pdbs),
		backend:     backend,
		drainConfig: testDrainConfig,
	}

	return mgr.forceTerminationAllowed(context.Background(), *pods[0], now, drainStart, lastForcedTermination)
}

var zeroTime = time.Unix(0, 0)

func pod(name string, creationTimestamp time.Time, healthy bool) *v1.Pod {
	status := v1.ConditionFalse
	if healthy {
		status = v1.ConditionTrue
	}

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: creationTimestamp},
			Labels:            map[string]string{"app": "foo"},
		},
		Spec: v1.PodSpec{
			NodeName: testNodeName,
		},
		Status: v1.PodStatus{
			Conditions: []v1.PodCondition{{Type: v1.PodReady, Status: status}},
		},
	}
}

func TestForceTerminationBasic(t *testing.T) {
	now := time.Now()
	for _, testcase := range []struct {
		msg                   string
		podCreation           time.Time
		drainStart            time.Time
		lastForcedTermination time.Time
		expected              bool
	}{
		{
			msg:                   "grace period (not terminated)",
			podCreation:           zeroTime,
			drainStart:            now,
			lastForcedTermination: zeroTime,
			expected:              false,
		},
		{
			msg:                   "grace period (terminated)",
			podCreation:           zeroTime,
			drainStart:            now.Add(-testDrainConfig.ForceEvictionGracePeriod),
			lastForcedTermination: zeroTime,
			expected:              true,
		},
		{
			msg:                   "force eviction interval (not terminated)",
			podCreation:           zeroTime,
			drainStart:            zeroTime,
			lastForcedTermination: now,
			expected:              false,
		},
		{
			msg:                   "force eviction interval (terminated)",
			podCreation:           zeroTime,
			drainStart:            zeroTime,
			lastForcedTermination: now.Add(-testDrainConfig.ForceEvictionInterval),
			expected:              true,
		},
		{
			msg:                   "pod too young (not terminated)",
			podCreation:           now,
			drainStart:            zeroTime,
			lastForcedTermination: zeroTime,
			expected:              false,
		},
		{
			msg:                   "pod too young (terminated)",
			podCreation:           now.Add(-testDrainConfig.MinPodLifetime),
			drainStart:            zeroTime,
			lastForcedTermination: zeroTime,
			expected:              true,
		},
	} {
		pods := []*v1.Pod{pod("foo-0", testcase.podCreation, true)}
		result, err := forceTerminationAllowed(t, pods, nil, now, testcase.drainStart, testcase.lastForcedTermination)
		assert.NoError(t, err, testcase.msg)
		assert.EqualValues(t, testcase.expected, result, testcase.msg)
	}
}

func TestForceTerminationPDBSiblings(t *testing.T) {
	pdbs := []*policy.PodDisruptionBudget{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
			},
			Spec: policy.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "foo"},
				},
			},
		},
	}

	now := time.Now()

	result, err := forceTerminationAllowed(
		t,
		[]*v1.Pod{
			pod("foo-0", zeroTime, true),
			pod("foo-1", now, true),
			pod("foo-2", now, false),
			pod("foo-3", zeroTime, true),
			pod("foo-4", zeroTime, false),
		},
		pdbs, now, zeroTime, zeroTime)
	require.NoError(t, err)
	require.False(t, result)

	result2, err := forceTerminationAllowed(
		t,
		[]*v1.Pod{
			pod("foo-0", zeroTime, true),
			pod("foo-1", now.Add(-testDrainConfig.MinHealthyPDBSiblingLifetime), true),
			pod("foo-2", now.Add(-testDrainConfig.MinUnhealthyPDBSiblingLifetime), false),
			pod("foo-3", zeroTime, true),
			pod("foo-4", zeroTime, false),
		},
		pdbs, now, zeroTime, zeroTime)
	require.NoError(t, err)
	require.True(t, result2)
}

func podNames(pods []v1.Pod) []string {
	var result []string
	for _, pod := range pods {
		result = append(result, pod.GetName())
	}
	return result
}

func TestFindPDBSiblings(t *testing.T) {
	testPod := func(name string, labels map[string]string) v1.Pod {
		return v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				Labels:    labels,
			},
			Spec: v1.PodSpec{
				NodeName: testNodeName,
			},
		}
	}
	testPDB := func(name string, labels map[string]string) policy.PodDisruptionBudget {
		return policy.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: policy.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
			},
		}
	}

	foo0 := testPod("foo-0", map[string]string{"app": "foo", "env": "production"})
	foo1 := testPod("foo-1", map[string]string{"app": "foo"})
	foo2 := testPod("foo-2", map[string]string{"app": "foo"})
	foo2.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "extensions/v1beta1",
		Kind:       "ReplicaSet",
		Name:       "foo-12345",
		UID:        "5678-9012",
	}}

	// terminated pod
	foo3 := testPod("foo-3", map[string]string{"app": "foo"})
	foo3.Status.Phase = v1.PodSucceeded

	// cronjob pod
	foo4 := testPod("foo-4", map[string]string{"app": "foo"})
	foo4.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       "foo-12345",
		UID:        "1234-5678",
	}}

	bar0 := testPod("bar-0", map[string]string{"app": "bar"})
	bar1 := testPod("bar-1", map[string]string{"app": "bar"})
	bar2 := testPod("bar-2", map[string]string{"app": "bar"})
	baz0 := testPod("baz-0", map[string]string{"app": "baz", "env": "production"})
	quux0 := testPod("quux-0", map[string]string{"app": "quux"})

	allPods := []v1.Pod{foo0, foo1, foo2, foo3, foo4, bar0, bar1, bar2, baz0, quux0}

	fooPdb := testPDB("foo", map[string]string{"app": "foo"})
	barPdb := testPDB("bar", map[string]string{"app": "bar"})
	prodPdb := testPDB("prod", map[string]string{"env": "production"})
	quuxPdb := testPDB("quux", map[string]string{"app": "quux"})
	brokenPdb := policy.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "broken",
			Namespace: "default",
		},
		Spec: policy.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "foo",
					Values:   []string{"bar"},
					Operator: "invalid",
				}},
			},
		},
	}

	allPdbs := []policy.PodDisruptionBudget{fooPdb, barPdb, prodPdb, quuxPdb, brokenPdb}

	logger := log.WithField("test", true)

	require.EqualValues(t, []string{"foo-1", "foo-2", "baz-0"}, podNames(findSiblingPods(logger, foo0, allPods, allPdbs)))
	require.EqualValues(t, []string{"bar-1", "bar-2"}, podNames(findSiblingPods(logger, bar0, allPods, allPdbs)))
	require.Empty(t, podNames(findSiblingPods(logger, quux0, allPods, allPdbs)))
}
