package controller

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
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
	DryRun            bool
	SecretDecrypter   decrypter.SecretDecrypter
	ConcurrentUpdates uint
}

// Controller defines the main control loop for the cluster-lifecycle-manager.
type Controller struct {
	registry             registry.Registry
	provisioner          provisioner.Provisioner
	channelConfigSourcer channel.ConfigSource
	secretDecrypter      decrypter.SecretDecrypter
	interval             time.Duration
	dryRun               bool
	clusterList          *ClusterList
	concurrentUpdates    uint
}

// New initializes a new controller.
func New(registry registry.Registry, provisioner provisioner.Provisioner, channelConfigSourcer channel.ConfigSource, options *Options) *Controller {
	return &Controller{
		registry:             registry,
		provisioner:          provisioner,
		channelConfigSourcer: channelConfigSourcer,
		secretDecrypter:      options.SecretDecrypter,
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
			log.Infof("Sleeping (%s) until next check", c.interval)
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
			nextCluster := c.clusterList.SelectNext()
			if nextCluster != nil {
				c.processCluster(workerNum, nextCluster)
			}
		case <-ctx.Done():
			return
		}
	}
}

// refresh refreshes the channel configuration and the cluster list
func (c *Controller) refresh() error {
	err := c.channelConfigSourcer.Update()
	if err != nil {
		return err
	}

	clusters, err := c.registry.ListClusters(registry.Filter{})
	if err != nil {
		return err
	}

	c.clusterList.UpdateAvailable(clusters)
	return nil
}

// doProcessCluster checks if an action needs to be taken depending on the
// cluster state and triggers the provisioner accordingly.
func (c *Controller) doProcessCluster(cluster *api.Cluster) error {
	if cluster.Status == nil {
		cluster.Status = &api.ClusterStatus{}
	}

	config, err := c.channelConfigSourcer.Get(cluster.Channel)
	if err != nil {
		return err
	}
	defer c.channelConfigSourcer.Delete(config)

	// decrypt any encrypted config items.
	err = c.decryptConfigItems(cluster)
	if err != nil {
		return err
	}

	switch cluster.LifecycleStatus {
	case statusRequested, statusReady:
		var nextVersion string
		nextVersion, err = c.provisioner.Version(cluster, config)
		if err != nil {
			return err
		}

		// don't continue if the status is ready and the version is
		// already the latest.
		if cluster.LifecycleStatus == statusReady && cluster.Status.CurrentVersion == nextVersion {
			break
		}

		cluster.Status.NextVersion = nextVersion
		if !c.dryRun {
			err = c.registry.UpdateCluster(cluster)
			if err != nil {
				return err
			}
		}

		err = c.provisioner.Provision(cluster, config)
		if err == nil {
			cluster.LifecycleStatus = statusReady

			cluster.Status.LastVersion = cluster.Status.CurrentVersion
			cluster.Status.CurrentVersion = cluster.Status.NextVersion
			cluster.Status.NextVersion = ""
			cluster.Status.Problems = []*api.Problem{}
		}
	case statusDecommissionRequested:
		err = c.provisioner.Decommission(cluster, config)
		if err == nil {
			cluster.Status.LastVersion = cluster.Status.CurrentVersion
			cluster.Status.CurrentVersion = ""
			cluster.Status.NextVersion = ""
			cluster.Status.Problems = []*api.Problem{}
			cluster.LifecycleStatus = statusDecommissioned
		}
	}

	return err
}

// processCluster calls doProcessCluster and handles logging and reporting
func (c *Controller) processCluster(workerNum uint, cluster *api.Cluster) {
	defer c.clusterList.ClusterProcessed(cluster.ID)
	clusterLog := log.WithField("cluster", cluster.Alias).WithField("worker", workerNum)

	clusterLog.Infof("Processing cluster (%s)", cluster.LifecycleStatus)

	err := c.doProcessCluster(cluster)

	// log the error and resolve the special error cases
	if err != nil {
		clusterLog.Errorf("Failed to process cluster: %s", err)

		// treat "provider not supported" as no error
		if err == provisioner.ErrProviderNotSupported {
			err = nil
		}
	} else {
		clusterLog.Infof("Finished processing cluster")
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
			clusterLog.Errorf("Unable to update cluster state: %s", err)
		}
	}
}

// decryptConfigItems tries to decrypt encrypted config items in the cluster
// config and modifies the passed cluster config so encrypted items has been
// decrypted.
func (c *Controller) decryptConfigItems(cluster *api.Cluster) error {
	for key, item := range cluster.ConfigItems {
		plaintext, err := c.secretDecrypter.Decrypt(item)
		if err != nil {
			return err
		}
		cluster.ConfigItems[key] = plaintext
	}

	return nil
}
