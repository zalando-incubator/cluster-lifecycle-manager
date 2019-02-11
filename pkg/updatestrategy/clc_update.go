package updatestrategy

import (
	"context"
	"time"

	"github.com/cenkalti/backoff"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

type CLCUpdateStrategy struct {
	nodePoolManager NodePoolManager
	logger          *log.Entry
	pollingInterval time.Duration
}

// NewCLCUpdateStrategy initializes a new CLCUpdateStrategy.
func NewCLCUpdateStrategy(logger *log.Entry, nodePoolManager NodePoolManager, pollingInterval time.Duration) *CLCUpdateStrategy {
	return &CLCUpdateStrategy{
		nodePoolManager: nodePoolManager,
		logger:          logger.WithField("strategy", "clc"),
		pollingInterval: pollingInterval,
	}
}

func (c *CLCUpdateStrategy) Update(ctx context.Context, nodePoolDesc *api.NodePool) error {
	c.logger.Infof("Initializing update of node pool '%s'", nodePoolDesc.Name)

	err := c.doUpdate(ctx, nodePoolDesc)
	if err != nil {
		if ctx.Err() == context.Canceled {
			backoffCfg := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 10)
			err := backoff.Retry(func() error {
				return c.rollbackUpdate(nodePoolDesc)
			}, backoffCfg)
			if err != nil {
				c.logger.Errorf("Error while aborting the update for node pool '%s': %v", nodePoolDesc.Name, err)
			}
		}
		return err
	}

	c.logger.Infof("Node pool '%s' successfully updated", nodePoolDesc.Name)
	return nil
}

func (c *CLCUpdateStrategy) PrepareForRemoval(ctx context.Context, nodePoolDesc *api.NodePool) error {
	c.logger.Infof("Preparing for removal of node pool '%s'", nodePoolDesc.Name)

	for {
		nodePool, err := c.nodePoolManager.GetPool(nodePoolDesc)
		if err != nil {
			return err
		}

		for _, node := range nodePool.Nodes {
			err := c.nodePoolManager.DisableReplacementNodeProvisioning(node)
			if err != nil {
				return err
			}
		}

		nodes, err := c.markNodes(nodePool, func(_ *Node) bool {
			return true
		})
		if err != nil {
			return err
		}

		if nodes == 0 {
			return nil
		}

		c.logger.WithField("node-pool", nodePoolDesc.Name).Infof("Waiting for decommissioning of the nodes (%d left)", nodes)

		// wait for CLC to finish removing the nodes
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.pollingInterval):
		}
	}
}

func (c *CLCUpdateStrategy) rollbackUpdate(nodePoolDesc *api.NodePool) error {
	nodePool, err := c.nodePoolManager.GetPool(nodePoolDesc)
	if err != nil {
		return err
	}

	for _, node := range nodePool.Nodes {
		if node.Generation != nodePool.Generation {
			err := c.nodePoolManager.AbortNodeDecommissioning(node)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *CLCUpdateStrategy) doUpdate(ctx context.Context, nodePoolDesc *api.NodePool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		nodePool, err := c.nodePoolManager.GetPool(nodePoolDesc)
		if err != nil {
			return err
		}

		oldNodes, err := c.markNodes(nodePool, func(node *Node) bool {
			return node.Generation != nodePool.Generation
		})
		if err != nil {
			return err
		}

		if oldNodes == 0 {
			return nil
		}

		c.logger.WithField("node-pool", nodePoolDesc.Name).Infof("Waiting for decommissioning of old nodes (%d left)", oldNodes)

		// wait for CLC to finish rolling the nodes
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.pollingInterval):
		}
	}
}

func (c *CLCUpdateStrategy) markNodes(nodePool *NodePool, predicate func(*Node) bool) (int, error) {
	marked := 0
	for _, node := range nodePool.Nodes {
		if predicate(node) {
			err := c.nodePoolManager.MarkNodeForDecommission(node)
			marked++
			if err != nil {
				return 0, err
			}
		}
	}
	return marked, nil
}
