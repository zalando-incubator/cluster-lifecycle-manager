package updatestrategy

import (
	"context"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	v1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func setupMockKubernetes(ctx context.Context, t *testing.T, nodes []*v1.Node, pods []*v1.Pod, pdbs []*policy.PodDisruptionBudget) kubernetes.Interface {
	client := fake.NewSimpleClientset()

	for _, node := range nodes {
		_, err := client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		require.NoError(t, err)
	}

	for _, pod := range pods {
		_, err := client.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
		require.NoError(t, err)
	}

	for _, pdb := range pdbs {
		_, err := client.PolicyV1beta1().PodDisruptionBudgets(pdb.GetNamespace()).Create(ctx, pdb, metav1.CreateOptions{})
		require.NoError(t, err)
	}

	return client
}

type mockProviderNodePoolsBackend struct {
	err      error
	nodePool *NodePool
}

func (n *mockProviderNodePoolsBackend) Get(context.Context, *api.NodePool) (*NodePool, error) {
	return n.nodePool, n.err
}

func (n *mockProviderNodePoolsBackend) Scale(context.Context, *api.NodePool, int) error {
	return n.err
}

func (n *mockProviderNodePoolsBackend) Terminate(_ context.Context, _ *api.NodePool, node *Node, _ bool) error {
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

func (n *mockProviderNodePoolsBackend) UpdateSize(_ *api.NodePool) error {
	return n.err
}

func (n *mockProviderNodePoolsBackend) MarkForDecommission(context.Context, *api.NodePool) error {
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
		setupMockKubernetes(context.Background(), t, []*v1.Node{node}, nil, nil),
		backend,
		&DrainConfig{},
		false,
	)

	// test getting nodes successfully
	nodePool, err := mgr.GetPool(context.Background(), &api.NodePool{Name: "test"})
	assert.NoError(t, err)
	assert.Len(t, nodePool.Nodes, 1)

	// test keeping the draining label
	node.ObjectMeta.Labels = map[string]string{
		lifecycleStatusLabel: lifecycleStatusDraining,
	}
	mgr.kube = setupMockKubernetes(context.Background(), t, []*v1.Node{node}, nil, nil)
	nodePool, err = mgr.GetPool(context.Background(), &api.NodePool{Name: "test"})
	assert.NoError(t, err)
	assert.Len(t, nodePool.Nodes, 1)
	assert.Equal(t, nodePool.Nodes[0].Labels[lifecycleStatusLabel], lifecycleStatusDraining)
}

func TestMarkForDecommission(t *testing.T) {
	for _, tc := range []struct {
		msg                 string
		nodePoolProfile     string
		noScheduleTaint     bool
		expectedTaintEffect v1.TaintEffect
	}{
		{
			msg:                 "master should always have PreferNoSchedule",
			nodePoolProfile:     "master",
			noScheduleTaint:     true,
			expectedTaintEffect: v1.TaintEffectPreferNoSchedule,
		},
		{
			msg:                 "worker should have NoSchedule if noScheduleTaint is true",
			nodePoolProfile:     "worker",
			noScheduleTaint:     true,
			expectedTaintEffect: v1.TaintEffectNoSchedule,
		},
		{
			msg:                 "worker should have PreferNoSchedule if noScheduleTaint is false",
			nodePoolProfile:     "worker",
			noScheduleTaint:     false,
			expectedTaintEffect: v1.TaintEffectPreferNoSchedule,
		},
	} {
		t.Run(tc.msg, func(t *testing.T) {
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
			kube := setupMockKubernetes(context.Background(), t, []*v1.Node{node}, nil, nil)
			mgr := NewKubernetesNodePoolManager(
				logger,
				kube,
				backend,
				&DrainConfig{},
				tc.noScheduleTaint,
			)

			// test getting nodes successfully
			nodePool, err := mgr.GetPool(context.Background(), &api.NodePool{Name: "test", Profile: tc.nodePoolProfile})
			require.NoError(t, err)
			require.Len(t, nodePool.Nodes, 1)

			// mark node for decomissioning
			err = mgr.MarkNodeForDecommission(context.Background(), nodePool.Nodes[0])
			require.NoError(t, err)

			updated, err := mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
			require.NoError(t, err)

			require.EqualValues(
				t,
				[]v1.Taint{
					{Key: decommissionPendingTaintKey, Value: decommissionPendingTaintValue, Effect: tc.expectedTaintEffect},
				},
				updated.Spec.Taints)
		})
	}
}

func TestLabelNodes(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	mgr := &KubernetesNodePoolManager{
		kube: setupMockKubernetes(context.Background(), t, []*v1.Node{node}, nil, nil),
	}

	err := mgr.labelNode(context.Background(), &Node{Name: node.Name}, "foo", "bar")
	assert.NoError(t, err)

	updated, err := mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, updated.Labels, map[string]string{"foo": "bar"})

	err = mgr.labelNode(context.Background(), &Node{Name: node.Name}, "foo", "baz")
	assert.NoError(t, err)

	updated2, err := mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, updated2.Labels, map[string]string{"foo": "baz"})
}

func TestCompareAndSetNodeLabel(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	mgr := &KubernetesNodePoolManager{
		kube: setupMockKubernetes(context.Background(), t, []*v1.Node{node}, nil, nil),
	}

	err := mgr.compareAndSetNodeLabel(context.Background(), &Node{Name: node.Name}, "foo", "baz", "bar")
	assert.NoError(t, err)

	updated, err := mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, updated.Labels, map[string]string{"foo": "bar"})

	err = mgr.compareAndSetNodeLabel(context.Background(), &Node{Name: node.Name}, "foo", "baz", "quux")
	assert.NoError(t, err)

	updated2, err := mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, updated2.Labels, map[string]string{"foo": "bar"})

	err = mgr.compareAndSetNodeLabel(context.Background(), &Node{Name: node.Name}, "foo", "bar", "quux")
	assert.NoError(t, err)

	updated3, err := mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, updated3.Labels, map[string]string{"foo": "quux"})
}

func TestAnnotateNodes(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	mgr := &KubernetesNodePoolManager{
		kube: setupMockKubernetes(context.Background(), t, []*v1.Node{node}, nil, nil),
	}

	err := mgr.annotateNode(context.Background(), &Node{Name: node.Name}, "foo", "bar")
	assert.NoError(t, err)

	updated, err := mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, updated.Annotations, map[string]string{"foo": "bar"})

	err = mgr.annotateNode(context.Background(), &Node{Name: node.Name}, "foo", "baz")
	assert.NoError(t, err)

	updated2, err := mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.EqualValues(t, updated2.Annotations, map[string]string{"foo": "baz"})
}

func TestTaintNode(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	mgr := &KubernetesNodePoolManager{
		kube: setupMockKubernetes(context.Background(), t, []*v1.Node{node}, nil, nil),
	}

	// we can add a new taint
	err := mgr.taintNode(context.Background(), &Node{Name: node.Name}, "foo", "bar", v1.TaintEffectNoSchedule)
	assert.NoError(t, err)

	updated, err := mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	assert.EqualValues(
		t,
		[]v1.Taint{
			{Key: "foo", Value: "bar", Effect: v1.TaintEffectNoSchedule},
		},
		updated.Spec.Taints)

	// we can add another taint
	err = mgr.taintNode(context.Background(), &Node{Name: node.Name, Taints: updated.Spec.Taints}, "bar", "quux", v1.TaintEffectNoExecute)
	assert.NoError(t, err)

	updated, err = mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	assert.EqualValues(
		t,
		[]v1.Taint{
			{Key: "foo", Value: "bar", Effect: v1.TaintEffectNoSchedule},
			{Key: "bar", Value: "quux", Effect: v1.TaintEffectNoExecute},
		},
		updated.Spec.Taints)

	// we can replace an existing taint
	err = mgr.taintNode(context.Background(), &Node{Name: node.Name, Taints: updated.Spec.Taints}, "bar", "foo", v1.TaintEffectNoSchedule)
	assert.NoError(t, err)

	updated, err = mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	assert.EqualValues(
		t,
		[]v1.Taint{
			{Key: "foo", Value: "bar", Effect: v1.TaintEffectNoSchedule},
			{Key: "bar", Value: "foo", Effect: v1.TaintEffectNoSchedule},
		},
		updated.Spec.Taints)

	// no-op updates should work
	err = mgr.taintNode(context.Background(), &Node{Name: node.Name, Taints: updated.Spec.Taints}, "bar", "foo", v1.TaintEffectNoSchedule)
	assert.NoError(t, err)

	updated, err = mgr.kube.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
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
		kube: setupMockKubernetes(context.Background(), t, []*v1.Node{node}, nil, nil),
	}

	err := mgr.CordonNode(context.Background(), &Node{Name: node.Name})
	assert.NoError(t, err)
}

func TestScalePool(tt *testing.T) {
	evictPod = func(_ context.Context, _ kubernetes.Interface, _ *log.Entry, _ v1.Pod) error {
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
				kube:    setupMockKubernetes(context.Background(), t, tc.nodes, nil, nil),
				logger:  log.WithField("test", true),
			}
			assert.NoError(t, mgr.ScalePool(context.Background(), &api.NodePool{Name: "test"}, tc.replicas))
		})
	}
}
