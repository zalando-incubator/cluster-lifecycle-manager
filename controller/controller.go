package controller

import (
	"context"
	"fmt"
	"slices"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"github.com/zalando-incubator/cluster-lifecycle-manager/provisioner"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

const (
	errTypeGeneral           = "https://cluster-lifecycle-manager.zalando.org/problems/general-error"
	errTypeCoalescedProblems = "https://cluster-lifecycle-manager.zalando.org/problems/too-many-problems"
	errorLimit               = 25
)

var (
	statusRequested             = "requested"
	statusReady                 = "ready"
	statusDecommissionRequested = "decommission-requested"
	statusDecommissioned        = "decommissioned"
)

// Options are options which can be used to configure the controller when it is
// initialized.
type Options struct {
	Interval          time.Duration
	AccountFilter     config.IncludeExcludeFilter
	Providers         []string
	DryRun            bool
	ConcurrentUpdates uint
	EnvironmentOrder  []string
}

// Controller defines the main control loop for the cluster-lifecycle-manager.
type Controller struct {
	logger               *log.Entry
	execManager          *command.ExecManager
	registry             registry.Registry
	provisioners         map[provisioner.ProviderID]provisioner.Provisioner
	providers            []string
	channelConfigSourcer channel.ConfigSource
	interval             time.Duration
	dryRun               bool
	clusterList          *ClusterList
	concurrentUpdates    uint
}

// New initializes a new controller.
func New(
	logger *log.Entry,
	execManager *command.ExecManager,
	registry registry.Registry,
	provisioners map[provisioner.ProviderID]provisioner.Provisioner,
	channelConfigSourcer channel.ConfigSource,
	options *Options,
) *Controller {
	return &Controller{
		logger:               logger,
		execManager:          execManager,
		registry:             registry,
		provisioners:         provisioners,
		providers:            options.Providers,
		channelConfigSourcer: channel.NewCachingSource(channelConfigSourcer),
		interval:             options.Interval,
		dryRun:               options.DryRun,
		clusterList:          NewClusterList(options.AccountFilter),
		concurrentUpdates:    options.ConcurrentUpdates,
	}
}

// Run the main controller loop.
func (c *Controller) Run(ctx context.Context) {
	log.Info("Starting main control loop.")

	// Start the update workers
	for i := uint(0); i < c.concurrentUpdates; i++ {
		go c.processWorkerLoop(ctx, i+1)
	}

	var interval time.Duration

	// Start the refresh loop
	for {
		select {
		case <-time.After(interval):
			interval = c.interval
			err := c.refresh()
			if err != nil {
				log.Errorf("Failed to refresh cluster list: %s", err)
			}
		case <-ctx.Done():
			log.Info("Terminating main controller loop.")
			return
		}
	}
}

func (c *Controller) processWorkerLoop(ctx context.Context, workerNum uint) {
	for {
		select {
		case <-time.After(c.interval):
			updateCtx, cancelFunc := context.WithCancel(ctx)
			nextCluster := c.clusterList.SelectNext(cancelFunc)
			if nextCluster != nil {
				c.processCluster(updateCtx, workerNum, nextCluster)
			}
			cancelFunc()
		case <-ctx.Done():
			return
		}
	}
}

// refresh refreshes the channel configuration and the cluster list
func (c *Controller) refresh() error {
	err := c.channelConfigSourcer.Update(context.Background(), c.logger)
	if err != nil {
		return err
	}

	clusters, err := c.registry.ListClusters(
		registry.Filter{
			Providers: c.providers,
		},
	)
	if err != nil {
		return err
	}

	c.clusterList.UpdateAvailable(c.channelConfigSourcer, c.dropUnsupported(clusters))
	return nil
}

// dropUnsupported removes clusters not supported by the current provisioner
func (c *Controller) dropUnsupported(clusters []*api.Cluster) []*api.Cluster {
	result := make([]*api.Cluster, 0, len(clusters))
	for _, cluster := range clusters {
		supports := false
		for _, provisioner := range c.provisioners {
			if provisioner.Supports(cluster) {
				supports = true
				result = append(result, cluster)
				break
			}
		}

		if !supports {
			log.Debugf("Unsupported cluster: %s", cluster.ID)
			continue
		}
	}
	return result
}

// doProcessCluster checks if an action needs to be taken depending on the
// cluster state and triggers the provisioner accordingly.
func (c *Controller) doProcessCluster(ctx context.Context, logger *log.Entry, clusterInfo *ClusterInfo) (rerr error) {
	cluster := clusterInfo.Cluster
	if cluster.Status == nil {
		cluster.Status = &api.ClusterStatus{}
	}

	// There was an error trying to determine the target configuration, abort
	if clusterInfo.NextError != nil {
		return clusterInfo.NextError
	}

	config, err := clusterInfo.ChannelVersion.Get(ctx, logger)
	if err != nil {
		return err
	}
	defer func() {
		err := config.Delete()
		if err != nil {
			rerr = err
		}
	}()

	provisioner, ok := c.provisioners[provisioner.ProviderID(cluster.Provider)]
	if !ok {
		return fmt.Errorf(
			"cluster %s: unknown provider %q",
			cluster.ID,
			cluster.Provider,
		)
	}

	switch cluster.LifecycleStatus {
	case statusRequested, statusReady:
		cluster.Status.NextVersion = clusterInfo.NextVersion.String()
		if !c.dryRun {
			err = c.registry.UpdateCluster(cluster)
			if err != nil {
				return err
			}
		}

		err = provisioner.Provision(ctx, logger, cluster, config)
		if err != nil {
			return err
		}

		cluster.LifecycleStatus = statusReady
		cluster.Status.LastVersion = cluster.Status.CurrentVersion
		cluster.Status.CurrentVersion = cluster.Status.NextVersion
		cluster.Status.NextVersion = ""
		cluster.Status.Problems = []*api.Problem{}
	case statusDecommissionRequested:
		err = provisioner.Decommission(ctx, logger, cluster)
		if err != nil {
			return err
		}

		cluster.Status.LastVersion = cluster.Status.CurrentVersion
		cluster.Status.CurrentVersion = ""
		cluster.Status.NextVersion = ""
		cluster.Status.Problems = []*api.Problem{}
		cluster.LifecycleStatus = statusDecommissioned
	default:
		return fmt.Errorf("invalid cluster status: %s", cluster.LifecycleStatus)
	}

	return nil
}

// processCluster calls doProcessCluster and handles logging and reporting
func (c *Controller) processCluster(updateCtx context.Context, workerNum uint, clusterInfo *ClusterInfo) {
	defer c.clusterList.ClusterProcessed(clusterInfo)

	cluster := clusterInfo.Cluster
	clusterLog := c.logger.WithField("cluster", cluster.Alias).WithField("worker", workerNum)

	versionedLog := clusterLog
	if clusterInfo.NextVersion != nil {
		versionedLog = clusterLog.WithField("version", clusterInfo.NextVersion.String())
	}

	versionedLog.Infof("Processing cluster (%s)", cluster.LifecycleStatus)

	err := c.doProcessCluster(updateCtx, clusterLog, clusterInfo)

	// log the error and resolve the special error cases
	if err != nil {
		versionedLog.Errorf("Failed to process cluster: %s", err)

		// treat "provider not supported" as no error
		if err == provisioner.ErrProviderNotSupported {
			err = nil
		}
	} else {
		versionedLog.Infof("Finished processing cluster")
	}

	// update the cluster state in the registry
	if !c.dryRun {
		if err != nil {
			if cluster.Status.Problems == nil {
				cluster.Status.Problems = make([]*api.Problem, 0, 1)
			}
			cluster.Status.Problems = append(cluster.Status.Problems, &api.Problem{
				Title: err.Error(),
				Type:  errTypeGeneral,
			})

			cluster.Status.Problems = slices.CompactFunc(cluster.Status.Problems, func(a, b *api.Problem) bool { return *a == *b })

			if len(cluster.Status.Problems) > errorLimit {
				cluster.Status.Problems = cluster.Status.Problems[len(cluster.Status.Problems)-errorLimit:]
				cluster.Status.Problems[0] = &api.Problem{
					Type:  errTypeCoalescedProblems,
					Title: "<multiple problems>",
				}
			}
		} else {
			cluster.Status.Problems = []*api.Problem{}
		}
		err = c.registry.UpdateCluster(cluster)
		if err != nil {
			versionedLog.Errorf("Unable to update cluster state: %s", err)
		}
	}
}
