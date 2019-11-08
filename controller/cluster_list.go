package controller

import (
	"context"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
)

const (
	updatePriorityNone = iota
	updatePriorityNormal
	updatePriorityDecommissionRequested
	updatePriorityAlreadyUpdating

	stateIdle = iota
	stateProcessing
	stateProcessed

	updateBlockedConfigItem = "cluster_update_block"
)

type ClusterInfo struct {
	lastProcessed  time.Time
	state          int
	cancelUpdate   context.CancelFunc
	updatePriority uint32
	Cluster        *api.Cluster

	ChannelVersion channel.ConfigVersion
	CurrentVersion *api.ClusterVersion
	NextVersion    *api.ClusterVersion
	NextError      error
}

// ClusterList maintains the state of all active clusters
type ClusterList struct {
	sync.Mutex
	accountFilter config.IncludeExcludeFilter
	clusters      map[string]*ClusterInfo
	pendingUpdate []*ClusterInfo
}

func NewClusterList(accountFilter config.IncludeExcludeFilter) *ClusterList {
	return &ClusterList{
		accountFilter: accountFilter,
		clusters:      make(map[string]*ClusterInfo),
	}
}

// UpdateAvailable adds new clusters to the list, updates the cluster data for existing ones and removes clusters
// that are no longer active. It also updates the list of clusters that need to be updated with the new information.
func (clusterList *ClusterList) UpdateAvailable(configSource channel.ConfigSource, availableClusters []*api.Cluster) {
	clusterList.Lock()
	defer clusterList.Unlock()

	clusterList.updateClusters(configSource, availableClusters)

	// Find out which clusters need updating
	var pendingUpdate []*ClusterInfo
	for _, cluster := range clusterList.clusters {
		if cluster.state != stateIdle {
			continue
		}

		// Compute and cache update priority; add the clusterInfo to the list if it needs anything done
		cluster.updatePriority = clusterList.updatePriority(cluster)
		if cluster.updatePriority != updatePriorityNone {
			pendingUpdate = append(pendingUpdate, cluster)
		}
	}
	sort.Slice(pendingUpdate, func(i, j int) bool {
		pi := pendingUpdate[i].updatePriority
		pj := pendingUpdate[j].updatePriority

		if pi > pj {
			return true
		} else if pi < pj {
			return false
		} else {
			return pendingUpdate[i].lastProcessed.Before(pendingUpdate[j].lastProcessed)
		}
	})
	clusterList.pendingUpdate = pendingUpdate
}

func updateBlocked(cluster *api.Cluster) bool {
	_, ok := cluster.ConfigItems[updateBlockedConfigItem]
	return ok
}

func (clusterList *ClusterList) updateClusters(configSource channel.ConfigSource, availableClusters []*api.Cluster) {
	availableClusterIds := make(map[string]bool)

	for _, cluster := range availableClusters {
		if cluster.LifecycleStatus == statusDecommissioned {
			log.Debugf("Cluster decommissioned: %s", cluster.ID)
			continue
		}

		if !clusterList.accountFilter.Allowed(cluster.InfrastructureAccount) {
			log.Debugf("Skipping %s cluster, infrastructure account does not match provided filter.", cluster.ID)
			continue
		}

		availableClusterIds[cluster.ID] = true

		currentVersion := api.ParseVersion(cluster.Status.CurrentVersion)

		var channelVersion channel.ConfigVersion
		var nextVersion *api.ClusterVersion
		var nextError error
		channelVersion, nextError = configSource.Version(cluster.Channel)

		if nextError == nil {
			nextVersion, nextError = cluster.Version(channelVersion)
		}

		if existing, ok := clusterList.clusters[cluster.ID]; ok {
			if existing.state != stateProcessing {
				existing.state = stateIdle
				existing.Cluster = cluster
				existing.ChannelVersion = channelVersion
				existing.CurrentVersion = currentVersion
				existing.NextVersion = nextVersion
				existing.NextError = nextError
			} else if existing.state == stateProcessing && updateBlocked(cluster) {
				// abort an update in progress
				existing.cancelUpdate()
			}
		} else {
			clusterList.clusters[cluster.ID] = &ClusterInfo{
				lastProcessed:  time.Unix(0, 0),
				state:          stateIdle,
				cancelUpdate:   func() {},
				Cluster:        cluster,
				ChannelVersion: channelVersion,
				CurrentVersion: currentVersion,
				NextVersion:    nextVersion,
				NextError:      nextError,
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
// for update before clusters with lower priority. A special value updatePriorityNone signifies that no update is needed.
func (clusterList *ClusterList) updatePriority(clusterInfo *ClusterInfo) uint32 {
	cluster := clusterInfo.Cluster

	// cluster updates are blocked
	if updateBlocked(clusterInfo.Cluster) {
		return updatePriorityNone
	}

	// something is wrong with cluster configuration (e.g. missing channel)
	if clusterInfo.NextError != nil {
		return updatePriorityNormal
	}

	// cluster needs to be decommissioned
	if cluster.LifecycleStatus == statusDecommissionRequested {
		return updatePriorityDecommissionRequested
	}

	// cluster is already being updated (CLM restart?)
	if cluster.Status.NextVersion != "" && cluster.Status.NextVersion != cluster.Status.CurrentVersion {
		return updatePriorityAlreadyUpdating
	}

	// channel version or configuration changed
	if *clusterInfo.NextVersion != *clusterInfo.CurrentVersion {
		return updatePriorityNormal
	}

	return updatePriorityNone
}

// SelectNext returns the next cluster to update, if any, and marks it as being processed. A cluster with higher
// priority will be selected first, in case of ties it'll select a cluster that hasn't been updated for the longest
// time.
func (clusterList *ClusterList) SelectNext(cancelUpdate context.CancelFunc) *ClusterInfo {
	clusterList.Lock()
	defer clusterList.Unlock()

	if len(clusterList.pendingUpdate) == 0 {
		return nil
	}

	result := clusterList.pendingUpdate[0]
	result.state = stateProcessing
	result.cancelUpdate = cancelUpdate
	clusterList.pendingUpdate = clusterList.pendingUpdate[1:]

	return result
}

// ClusterProcessed marks a cluster as no longer being processed.
func (clusterList *ClusterList) ClusterProcessed(cluster *ClusterInfo) {
	clusterList.Lock()
	defer clusterList.Unlock()

	if cluster, ok := clusterList.clusters[cluster.Cluster.ID]; ok {
		cluster.state = stateProcessed
		cluster.cancelUpdate = func() {}
		cluster.lastProcessed = time.Now()
	}
}
