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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
)

func removePod(client kubernetes.Interface, pod v1.Pod) error {
	var zero int64
	client.CoreV1().Pods(pod.GetNamespace()).Delete(pod.GetName(), &metav1.DeleteOptions{GracePeriodSeconds: &zero})
	return nil
}

func evictPodFailPDB(client kubernetes.Interface, logger *log.Entry, pod v1.Pod) error {
	return &errors.StatusError{
		ErrStatus: metav1.Status{
			Code: http.StatusTooManyRequests,
		},
	}
}

func TestTerminateNode(t *testing.T) {
	nodeName := "test"
	evictPod = func(client kubernetes.Interface, logger *log.Entry, pod v1.Pod) error {
		return removePod(client, pod)
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
		logger:  logger,
		kube:    setupMockKubernetes(t, []*v1.Node{node}, pods),
		backend: backend,
		drainConfig: &DrainConfig{
			PollInterval: time.Second,
		},
	}

	err := mgr.TerminateNode(context.Background(), &Node{Name: node.Name}, false)
	assert.NoError(t, err)

	// test when evictPod returns 429
	evictPod = evictPodFailPDB

	mgr.kube = setupMockKubernetes(t, []*v1.Node{node}, pods)
	err = mgr.TerminateNode(context.Background(), &Node{Name: node.Name}, false)
	assert.NoError(t, err)
}

func TestTerminateNodeCancelled(t *testing.T) {
	nodeName := "test"

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
		var evictCount int32

		mgr := &KubernetesNodePoolManager{
			logger:      logger,
			kube:        setupMockKubernetes(t, []*v1.Node{node}, pods),
			backend:     backend,
			drainConfig: &DrainConfig{},
		}

		evictPod = func(client kubernetes.Interface, logger *log.Entry, pod v1.Pod) error {
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

		evictPod = func(client kubernetes.Interface, logger *log.Entry, pod v1.Pod) error {
			// unblock so we can be cancelled
			blockHelper.Done()
			// wait until we're unblocked
			blockEviction.Wait()
			atomic.AddInt32(&evictCount, 1)
			return removePod(client, pod)
		}

		mgr := &KubernetesNodePoolManager{
			logger:      logger,
			kube:        setupMockKubernetes(t, []*v1.Node{node}, pods),
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
			kube:    setupMockKubernetes(t, []*v1.Node{node}, pods),
			backend: backend,
			drainConfig: &DrainConfig{
				ForceEvictionInterval: time.Minute,
				PollInterval:          time.Second,
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		deletePod = func(client kubernetes.Interface, logger *log.Entry, pod v1.Pod) error {
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
