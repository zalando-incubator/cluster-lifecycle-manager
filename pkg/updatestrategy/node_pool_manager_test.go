package updatestrategy

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/pkg/api/v1"
)

func setupMockKubernetes(t *testing.T, nodes []*v1.Node, pods []*v1.Pod) kubernetes.Interface {
	client := fake.NewSimpleClientset()

	for _, node := range nodes {
		_, err := client.CoreV1().Nodes().Create(node)
		assert.NoError(t, err)
	}

	for _, pod := range pods {
		_, err := client.CoreV1().Pods(pod.Namespace).Create(pod)
		assert.NoError(t, err)
	}

	return client
}

type mockProviderNodePoolsBackend struct {
	err      error
	nodePool *NodePool
}

func (n *mockProviderNodePoolsBackend) Get(nodePool *api.NodePool) (*NodePool, error) {
	return n.nodePool, n.err
}

func (n *mockProviderNodePoolsBackend) Scale(nodePool *api.NodePool, replicas int) error {
	return n.err
}

func (n *mockProviderNodePoolsBackend) Terminate(node *Node, decrementDesired bool) error {
	newNodes := make([]*Node, 0, len(n.nodePool.Nodes))
	for _, n := range n.nodePool.Nodes {
		if n.Name != node.Name {
			newNodes = append(newNodes, n)
		}
	}
	n.nodePool.Current = len(newNodes)
	n.nodePool.Desired = len(newNodes)
	n.nodePool.Nodes = newNodes
	return n.err
}

func (n *mockProviderNodePoolsBackend) UpdateSize(nodePool *api.NodePool) error {
	return n.err
}

func (n *mockProviderNodePoolsBackend) SuspendAutoscaling(nodePool *api.NodePool) error {
	return n.err
}

func TestGetPool(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: v1.NodeSpec{
			ProviderID: "provider-id",
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
				{ProviderID: "provider-id", Ready: true},
			},
		},
	}
	mgr := NewKubernetesNodePoolManager(
		logger,
		setupMockKubernetes(t, []*v1.Node{node}, nil),
		backend,
		0,
	)

	// test getting nodes successfully
	nodePool, err := mgr.GetPool(&api.NodePool{Name: "test"})
	assert.NoError(t, err)
	assert.Len(t, nodePool.Nodes, 1)

	// test keeping the draining label
	node.ObjectMeta.Labels = map[string]string{
		lifecycleStatusLabel: lifecycleStatusDraining,
	}
	mgr.kube = setupMockKubernetes(t, []*v1.Node{node}, nil)
	nodePool, err = mgr.GetPool(&api.NodePool{Name: "test"})
	assert.NoError(t, err)
	assert.Len(t, nodePool.Nodes, 1)
	assert.Equal(t, nodePool.Nodes[0].Labels[lifecycleStatusLabel], lifecycleStatusDraining)
}

func TestLabelNodes(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	mgr := &KubernetesNodePoolManager{
		kube: setupMockKubernetes(t, []*v1.Node{node}, nil),
	}

	err := mgr.LabelNode(&Node{Name: node.Name}, "foo", "bar")
	assert.NoError(t, err)
}

func TestTaintNode(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	mgr := &KubernetesNodePoolManager{
		kube: setupMockKubernetes(t, []*v1.Node{node}, nil),
	}

	// we can add a new taint
	err := mgr.TaintNode(&Node{Name: node.Name}, "foo", "bar", v1.TaintEffectNoSchedule)
	assert.NoError(t, err)

	updated, err := mgr.kube.CoreV1().Nodes().Get(node.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	assert.EqualValues(
		t,
		[]v1.Taint{
			{Key: "foo", Value: "bar", Effect: v1.TaintEffectNoSchedule},
		},
		updated.Spec.Taints)

	// we can add another taint
	err = mgr.TaintNode(&Node{Name: node.Name, Taints: updated.Spec.Taints}, "bar", "quux", v1.TaintEffectNoExecute)
	assert.NoError(t, err)

	updated, err = mgr.kube.CoreV1().Nodes().Get(node.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	assert.EqualValues(
		t,
		[]v1.Taint{
			{Key: "foo", Value: "bar", Effect: v1.TaintEffectNoSchedule},
			{Key: "bar", Value: "quux", Effect: v1.TaintEffectNoExecute},
		},
		updated.Spec.Taints)

	// we can replace an existing taint
	err = mgr.TaintNode(&Node{Name: node.Name, Taints: updated.Spec.Taints}, "bar", "foo", v1.TaintEffectNoSchedule)
	assert.NoError(t, err)

	updated, err = mgr.kube.CoreV1().Nodes().Get(node.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	assert.EqualValues(
		t,
		[]v1.Taint{
			{Key: "foo", Value: "bar", Effect: v1.TaintEffectNoSchedule},
			{Key: "bar", Value: "foo", Effect: v1.TaintEffectNoSchedule},
		},
		updated.Spec.Taints)

	// no-op updates should work
	err = mgr.TaintNode(&Node{Name: node.Name, Taints: updated.Spec.Taints}, "bar", "foo", v1.TaintEffectNoSchedule)
	assert.NoError(t, err)

	updated, err = mgr.kube.CoreV1().Nodes().Get(node.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	assert.EqualValues(
		t,
		[]v1.Taint{
			{Key: "foo", Value: "bar", Effect: v1.TaintEffectNoSchedule},
			{Key: "bar", Value: "foo", Effect: v1.TaintEffectNoSchedule},
		},
		updated.Spec.Taints)
}

func TestCordonNode(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	mgr := &KubernetesNodePoolManager{
		kube: setupMockKubernetes(t, []*v1.Node{node}, nil),
	}

	err := mgr.CordonNode(&Node{Name: node.Name})
	assert.NoError(t, err)
}

func TestScalePool(tt *testing.T) {
	evictPod = func(client kubernetes.Interface, logger *log.Entry, pod *v1.Pod) error {
		return nil
	}

	for _, tc := range []struct {
		msg      string
		backend  *mockProviderNodePoolsBackend
		nodes    []*v1.Node
		replicas int
	}{
		{
			msg: "no scale needed",
			backend: &mockProviderNodePoolsBackend{
				nodePool: &NodePool{
					Min:        1,
					Max:        1,
					Current:    1,
					Desired:    1,
					Generation: 1,
					Nodes: []*Node{
						{
							ProviderID: "provider-id",
							Ready:      true,
						},
					},
				},
			},
			nodes: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: v1.NodeSpec{
						ProviderID: "provider-id",
					},
				},
			},
			replicas: 1,
		},
		{
			msg: "scale up",
			backend: &mockProviderNodePoolsBackend{
				nodePool: &NodePool{
					Min:        1,
					Max:        1,
					Current:    1,
					Desired:    1,
					Generation: 1,
					Nodes: []*Node{
						{
							ProviderID: "provider-id",
							Ready:      true,
						},
					},
				},
			},
			nodes: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: v1.NodeSpec{
						ProviderID: "provider-id",
					},
				},
			},
			replicas: 2,
		},
		{
			msg: "scale down",
			backend: &mockProviderNodePoolsBackend{
				nodePool: &NodePool{
					Min:        1,
					Max:        1,
					Current:    1,
					Desired:    1,
					Generation: 1,
					Nodes: []*Node{
						{
							ProviderID: "provider-id",
							Ready:      true,
						},
					},
				},
			},
			nodes: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: v1.NodeSpec{
						ProviderID: "provider-id",
					},
				},
			},
			replicas: 0,
		},
	} {
		tt.Run(tc.msg, func(t *testing.T) {
			mgr := &KubernetesNodePoolManager{
				backend: tc.backend,
				kube:    setupMockKubernetes(t, tc.nodes, nil),
				logger:  log.WithField("test", true),
			}
			assert.NoError(t, mgr.ScalePool(context.Background(), &api.NodePool{Name: "test"}, tc.replicas))
		})
	}
}

func TestTerminateNode(t *testing.T) {
	nodeName := "test"
	evictPod = func(client kubernetes.Interface, logger *log.Entry, pod *v1.Pod) error {
		return nil
	}

	pods := []*v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "a",
				Namespace: "default",
			},
			Spec: v1.PodSpec{
				NodeName: nodeName,
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
				NodeName: nodeName,
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
				NodeName: nodeName,
			},
		},
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
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
		logger:          logger,
		kube:            setupMockKubernetes(t, []*v1.Node{node}, pods),
		backend:         backend,
		maxEvictTimeout: 1 * time.Nanosecond,
	}

	err := mgr.TerminateNode(context.Background(), &Node{Name: node.Name}, false)
	assert.NoError(t, err)

	// test when evictPod returns 429
	evictPod = func(client kubernetes.Interface, logger *log.Entry, pod *v1.Pod) error {
		return &errors.StatusError{
			ErrStatus: metav1.Status{
				Code: http.StatusTooManyRequests,
			},
		}
	}

	mgr.kube = setupMockKubernetes(t, []*v1.Node{node}, pods)
	err = mgr.TerminateNode(context.Background(), &Node{Name: node.Name}, false)
	assert.NoError(t, err)
}

func TestTerminateNodeCancelled(t *testing.T) {
	nodeName := "test"

	var evictCount int32
	blockEviction := sync.WaitGroup{}
	blockHelper := sync.WaitGroup{}
	blockEviction.Add(1)
	// add two to the wg counter because we have two pods that will be
	// evicted in parallel.
	blockHelper.Add(2)

	evictPod = func(client kubernetes.Interface, logger *log.Entry, pod *v1.Pod) error {
		// unblock so we can be cancelled
		blockHelper.Done()
		// wait until we're unblocked
		blockEviction.Wait()
		atomic.AddInt32(&evictCount, 1)
		return nil
	}

	pods := []*v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "a",
				Namespace: "default",
			},
			Spec: v1.PodSpec{
				NodeName: nodeName,
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
				NodeName: nodeName,
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
				NodeName: nodeName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "d",
				Namespace: "default",
			},
			Spec: v1.PodSpec{
				NodeName: nodeName,
			},
		},
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
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
		mgr := &KubernetesNodePoolManager{
			logger:          logger,
			kube:            setupMockKubernetes(t, []*v1.Node{node}, pods),
			backend:         backend,
			maxEvictTimeout: 1 * time.Nanosecond,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := mgr.TerminateNode(ctx, &Node{Name: node.Name}, false)
		evictCountFinal := atomic.LoadInt32(&evictCount)
		assert.Zero(t, evictCountFinal)
		assert.Error(t, err)
	}

	// cancel after first pod
	{
		mgr := &KubernetesNodePoolManager{
			logger:          logger,
			kube:            setupMockKubernetes(t, []*v1.Node{node}, pods),
			backend:         backend,
			maxEvictTimeout: 1 * time.Nanosecond,
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
		assert.Error(t, err)
	}
}
