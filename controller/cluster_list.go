package controller

import (
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
)

type ClusterInfo struct {
	lastProcessed time.Time
	state         int
	Cluster       *api.Cluster

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
// that are no longer active.
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

		currentVersion := api.ParseVersion(cluster.Status.CurrentVersion)

		var channelVersion channel.ConfigVersion
		var nextVersion *api.ClusterVersion
		var nextError error
		channelVersion, nextError = channels.Version(cluster.Channel)
		if nextError == nil {
			nextVersion, nextError = cluster.Version(channelVersion)
		}

		if existing, ok := clusterList.clusters[cluster.ID]; ok {
			if existing.state != stateProcessing {
				existing.state = stateIdle
				existing.Cluster = cluster
				existing.CurrentVersion = currentVersion
				existing.NextVersion = nextVersion
				existing.NextError = nextError
			}
		} else {
			clusterList.clusters[cluster.ID] = &ClusterInfo{
				lastProcessed:  time.Unix(0, 0),
				state:          stateIdle,
				Cluster:        cluster,
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

	// find out which clusters need updating
	var pendingUpdate []*ClusterInfo
	for _, cluster := range clusterList.clusters {
		if cluster.state != stateIdle {
			continue
		}

		if updatePriority(cluster) != updatePriorityNone {
			pendingUpdate = append(pendingUpdate, cluster)
		}
	}
	sort.Slice(pendingUpdate, func(i, j int) bool {
		pi := updatePriority(pendingUpdate[i])
		pj := updatePriority(pendingUpdate[j])

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

// updatePriority returns the update priority of the clusters. Clusters with higher priority will always be selected
// for update before clusters with lower priority. A special value updatePriorityNone signifies that no update is needed.
func updatePriority(clusterInfo *ClusterInfo) uint32 {
	cluster := clusterInfo.Cluster

	if cluster.Status.NextVersion != "" && cluster.Status.NextVersion != cluster.Status.CurrentVersion {
		return updatePriorityAlreadyUpdating
	}

	if cluster.LifecycleStatus == statusDecommissionRequested {
		return updatePriorityDecommissionRequested
	}

	if clusterInfo.NextError != nil || *clusterInfo.NextVersion != *clusterInfo.CurrentVersion {
		return updatePriorityNormal
	}

	return updatePriorityNone
}

// SelectNext returns the next cluster to update, if any, and marks it as being processed. A cluster with higher
// priority will be selected first, in case of ties it'll select a cluster that hasn't been updated for the longest
// time.
func (clusterList *ClusterList) SelectNext() *ClusterInfo {
	clusterList.Lock()
	defer clusterList.Unlock()

	if len(clusterList.pendingUpdate) == 0 {
		return nil
	}

	result := clusterList.pendingUpdate[0]
	result.state = stateProcessing
	clusterList.pendingUpdate = clusterList.pendingUpdate[1:]

	return result
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
