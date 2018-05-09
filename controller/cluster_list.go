package controller

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
)

const (
	updatePriorityNormal = iota
	updatePriorityDecommissionRequested
	updatePriorityAlreadyUpdating

	stateIdle = iota
	stateProcessing
	stateProcessed
)

type ClusterInfo struct {
	lastProcessed time.Time
	state         int
	Cluster       *api.Cluster
	ConfigVersion channel.ConfigVersion
}

// ClusterList maintains the state of all active clusters
type ClusterList struct {
	sync.Mutex
	accountFilter config.IncludeExcludeFilter
	clusters      map[string]*ClusterInfo
}

func NewClusterList(accountFilter config.IncludeExcludeFilter) *ClusterList {
	return &ClusterList{
		accountFilter: accountFilter,
		clusters:      make(map[string]*ClusterInfo),
	}
}

// UpdateAvailable adds new clusters to the list, updates the cluster data for existing ones and removes clusters
// that are no longer active
func (clusterList *ClusterList) UpdateAvailable(channels channel.ConfigVersions, availableClusters []*api.Cluster) {
	clusterList.Lock()
	defer clusterList.Unlock()

	availableClusterIds := make(map[string]bool)

	for _, cluster := range availableClusters {
		if cluster.LifecycleStatus == statusDecommissioned {
			log.Debugf("Cluster decommissioned: %s", cluster.ID)
			continue
		}

		if !clusterList.accountFilter.Allowed(cluster.InfrastructureAccount) {
			log.Infof("Skipping %s cluster, infrastructure account does not match provided filter.", cluster.ID)
			continue
		}

		availableClusterIds[cluster.ID] = true

		channelVersion, err := channels.Version(cluster.Channel)
		if err != nil {
			channelVersion = ""
		}

		if existing, ok := clusterList.clusters[cluster.ID]; ok {
			if existing.state != stateProcessing {
				existing.state = stateIdle
				existing.Cluster = cluster
				existing.ConfigVersion = channelVersion
			}
		} else {
			clusterList.clusters[cluster.ID] = &ClusterInfo{
				lastProcessed: time.Unix(0, 0),
				state:         stateIdle,
				Cluster:       cluster,
				ConfigVersion: channelVersion,
			}
		}
	}

	for id, cluster := range clusterList.clusters {
		// keep clusters that are still being updated to avoid race conditions
		// if they're deleted and then added again
		if cluster.state == stateProcessing {
			continue
		}

		if _, ok := availableClusterIds[id]; !ok {
			delete(clusterList.clusters, id)
		}
	}
}

// updatePriority returns the update priority of the clusters. Clusters with higher priority will always be selected
// for update before clusters with lower priority.
func updatePriority(cluster *api.Cluster) uint32 {
	if cluster.Status.NextVersion != "" && cluster.Status.NextVersion != cluster.Status.CurrentVersion {
		return updatePriorityAlreadyUpdating
	}
	if cluster.LifecycleStatus == statusDecommissionRequested {
		return updatePriorityDecommissionRequested
	}
	return updatePriorityNormal
}

// SelectNext returns the next cluster of update, if any, and marks it as being processed. A cluster with higher
// priority will be selected first, in case of ties it'll select a cluster that hasn't been updated for the longest
// time.
func (clusterList *ClusterList) SelectNext() *ClusterInfo {
	clusterList.Lock()
	defer clusterList.Unlock()

	var nextCluster *ClusterInfo
	var nextClusterPriority uint32

	for _, cluster := range clusterList.clusters {
		if cluster.state != stateIdle {
			continue
		}

		if nextCluster == nil {
			nextCluster = cluster
			nextClusterPriority = updatePriority(cluster.Cluster)
		} else {
			priority := updatePriority(cluster.Cluster)

			if priority > nextClusterPriority || (priority == nextClusterPriority && cluster.lastProcessed.Before(nextCluster.lastProcessed)) {
				nextCluster = cluster
				nextClusterPriority = priority
			}
		}
	}

	if nextCluster == nil {
		return nil
	}

	nextCluster.state = stateProcessing
	return nextCluster
}

// ClusterProcessed marks a cluster as no longer being processed.
func (clusterList *ClusterList) ClusterProcessed(cluster *ClusterInfo) {
	clusterList.Lock()
	defer clusterList.Unlock()

	if cluster, ok := clusterList.clusters[cluster.Cluster.ID]; ok {
		cluster.state = stateProcessed
		cluster.lastProcessed = time.Now()
	}
}
