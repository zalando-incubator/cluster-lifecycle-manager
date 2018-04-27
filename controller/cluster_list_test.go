package controller

import (
	"regexp"
	"sort"
	"testing"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/stretchr/testify/assert"
)

var mockStatus = &api.ClusterStatus{
	NextVersion:    "",
	CurrentVersion: "abc123",
}

func TestUpdateIgnoresClusters(t *testing.T) {
	filter := config.IncludeExcludeFilter{
		Exclude: regexp.MustCompile("^aws:123456789222.*"),
		Include: regexp.MustCompile("^aws.*"),
	}

	for _, ti := range []struct {
		cluster *api.Cluster
		ignored bool
	}{
		{
			cluster: &api.Cluster{
				ID: "aws:123456789011:eu-central-1:decommissioned",
				InfrastructureAccount: "aws:123456789011",
				LifecycleStatus:       "decommissioned",
				Status:                mockStatus,
			},
			ignored: true,
		},
		{
			cluster: &api.Cluster{
				ID: "aws:123456789011:eu-central-1:ready",
				InfrastructureAccount: "aws:123456789011",
				LifecycleStatus:       "ready",
				Status:                mockStatus,
			},
			ignored: false,
		},
		{
			cluster: &api.Cluster{
				ID: "aws:123456789011:eu-central-1:requested",
				InfrastructureAccount: "aws:123456789011",
				LifecycleStatus:       "ready",
				Status:                mockStatus,
			},
			ignored: false,
		},
		{
			cluster: &api.Cluster{
				ID: "aws:123456789011:eu-central-1:decommission-requested",
				InfrastructureAccount: "aws:123456789011",
				LifecycleStatus:       "decommission-requested",
				Status:                mockStatus,
			},
			ignored: false,
		},
		{
			cluster: &api.Cluster{
				ID: "aws:123456789222:eu-central-1:excluded",
				InfrastructureAccount: "aws:123456789222",
				LifecycleStatus:       "ready",
				Status:                mockStatus,
			},
			ignored: true,
		},
		{
			cluster: &api.Cluster{
				ID: "foobar:123456789011:eu-central-1:not-included",
				InfrastructureAccount: "foobar:123456789011",
				LifecycleStatus:       "ready",
				Status:                mockStatus,
			},
			ignored: true,
		},
	} {
		clusterList := NewClusterList(filter)
		clusterList.UpdateAvailable([]*api.Cluster{ti.cluster})
		nextCluster := clusterList.SelectNext()
		if ti.ignored {
			assert.Nil(t, nextCluster, "cluster wasn't ignored: %s", ti.cluster.ID)
		} else {
			assert.NotNil(t, nextCluster, "cluster ignored: %s", ti.cluster.ID)
		}
	}
}

func allClusterIds(clusterList *ClusterList) []string {
	var result []string
	for {
		cluster := clusterList.SelectNext()
		if cluster == nil {
			for _, id := range result {
				clusterList.ClusterProcessed(id)
			}
			return result
		} else {
			result = append(result, cluster.ID)
		}
	}
}

func TestUpdateAddsNewClusters(t *testing.T) {
	cluster1 := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Status:                mockStatus,
	}
	cluster2 := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:cluster2",
		InfrastructureAccount: "aws:123456789012",
		LifecycleStatus:       "ready",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter)

	// No clusters yet
	assert.Nil(t, clusterList.SelectNext())

	// One new cluster
	clusterList.UpdateAvailable([]*api.Cluster{cluster1})
	assert.Equal(t, []string{cluster1.ID}, allClusterIds(clusterList))

	// Another new cluster
	clusterList.UpdateAvailable([]*api.Cluster{cluster1, cluster2})
	assert.Equal(t, []string{cluster2.ID, cluster1.ID}, allClusterIds(clusterList))
}

func TestUpdateUpdatesExistingClusters(t *testing.T) {
	cluster := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "requested",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter)

	clusterList.UpdateAvailable([]*api.Cluster{cluster})
	assert.Equal(t, cluster.LifecycleStatus, clusterList.SelectNext().LifecycleStatus)
	clusterList.ClusterProcessed(cluster.ID)

	updated := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Status:                mockStatus,
	}
	clusterList.UpdateAvailable([]*api.Cluster{updated})
	assert.Equal(t, updated.LifecycleStatus, clusterList.SelectNext().LifecycleStatus)
	clusterList.ClusterProcessed(updated.ID)

	assert.Equal(t, []string{cluster.ID}, allClusterIds(clusterList))
}

func sortedStrings(s []string) []string {
	sort.Strings(s)
	return s
}

func TestUpdateDeletesUnusedClusters(t *testing.T) {
	cluster1 := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Status:                mockStatus,
	}
	cluster2 := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:cluster2",
		InfrastructureAccount: "aws:123456789012",
		LifecycleStatus:       "ready",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter)

	clusterList.UpdateAvailable([]*api.Cluster{cluster1, cluster2})
	assert.Equal(t, []string{cluster1.ID, cluster2.ID}, sortedStrings(allClusterIds(clusterList)))

	clusterList.UpdateAvailable([]*api.Cluster{cluster2})
	assert.Equal(t, []string{cluster2.ID}, allClusterIds(clusterList))
}

func TestClusterPriority(t *testing.T) {
	normal := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:normal",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Status:                mockStatus,
	}
	decommissionRequested := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:decommission-requested",
		InfrastructureAccount: "aws:123456789012",
		LifecycleStatus:       "decommission-requested",
		Status:                mockStatus,
	}
	pendingUpdate := &api.Cluster{
		ID: "aws:123456789013:eu-central-1:pendingUpdate",
		InfrastructureAccount: "aws:123456789013",
		LifecycleStatus:       "ready",
		Status: &api.ClusterStatus{
			NextVersion:    "abc123",
			CurrentVersion: "def456",
		},
	}
	normal2 := &api.Cluster{
		ID: "aws:123456789014:eu-central-1:normal-2",
		InfrastructureAccount: "aws:123456789014",
		LifecycleStatus:       "ready",
		Status:                mockStatus,
	}

	for _, clusters := range [][]*api.Cluster{
		{normal, decommissionRequested, pendingUpdate},
		{normal, pendingUpdate, decommissionRequested},
		{decommissionRequested, normal, pendingUpdate},
		{decommissionRequested, pendingUpdate, normal},
		{pendingUpdate, normal, decommissionRequested},
		{pendingUpdate, decommissionRequested, normal},
	} {
		clusterList := NewClusterList(config.DefaultFilter)

		clusterList.UpdateAvailable(clusters)
		assert.Equal(t, []string{pendingUpdate.ID, decommissionRequested.ID, normal.ID}, allClusterIds(clusterList))

		// add normal2, it should now be updated before normal1
		clusterList.UpdateAvailable(append(clusters, normal2))
		assert.Equal(t, []string{pendingUpdate.ID, decommissionRequested.ID, normal2.ID, normal.ID}, allClusterIds(clusterList))
	}
}

func TestClusterLastUpdated(t *testing.T) {
	clusterList := NewClusterList(config.DefaultFilter)
	clusterList.UpdateAvailable([]*api.Cluster{
		{
			ID: "aws:123456789011:eu-central-1:cluster1",
			InfrastructureAccount: "aws:123456789011",
			LifecycleStatus:       "ready",
			Status:                mockStatus,
		},
		{
			ID: "aws:123456789012:eu-central-1:cluster2",
			InfrastructureAccount: "aws:123456789012",
			LifecycleStatus:       "ready",
			Status:                mockStatus,
		},
		{
			ID: "aws:123456789013:eu-central-1:cluster3",
			InfrastructureAccount: "aws:123456789013",
			LifecycleStatus:       "ready",
			Status:                mockStatus,
		},
	})

	// get the next clusters to process
	next1 := clusterList.SelectNext()
	next2 := clusterList.SelectNext()
	next3 := clusterList.SelectNext()

	// finish processing in a different order (2->1->3)
	clusterList.ClusterProcessed(next2.ID)
	clusterList.ClusterProcessed(next1.ID)
	clusterList.ClusterProcessed(next3.ID)

	// the same order should be preserved for next update attempts
	assert.Equal(t, []string{next2.ID, next1.ID, next3.ID}, allClusterIds(clusterList))
}

func TestProcessingClusterNotDeleted(t *testing.T) {
	cluster := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter)
	clusterList.UpdateAvailable([]*api.Cluster{cluster})
	assert.Equal(t, cluster.ID, clusterList.SelectNext().ID)

	// remove the cluster
	clusterList.UpdateAvailable([]*api.Cluster{})

	// add it back, but it still should be processing
	clusterList.UpdateAvailable([]*api.Cluster{cluster})
	assert.Nil(t, clusterList.SelectNext())

	// finish processing
	clusterList.ClusterProcessed(cluster.ID)
	assert.Equal(t, cluster.ID, clusterList.SelectNext().ID)
}

func TestProcessingClusterNotUpdated(t *testing.T) {
	cluster := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "ready",
		Status:                mockStatus,
	}

	clusterList := NewClusterList(config.DefaultFilter)
	clusterList.UpdateAvailable([]*api.Cluster{cluster})
	assert.Equal(t, cluster.ID, clusterList.SelectNext().ID)

	updated := &api.Cluster{
		ID: "aws:123456789011:eu-central-1:cluster1",
		InfrastructureAccount: "aws:123456789011",
		LifecycleStatus:       "decommission-pending",
		Status:                mockStatus,
	}

	clusterList.UpdateAvailable([]*api.Cluster{updated})
	clusterList.ClusterProcessed(cluster.ID)

	// cluster should not be overwritten
	assert.Equal(t, cluster.LifecycleStatus, clusterList.SelectNext().LifecycleStatus)
}
