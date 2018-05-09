package controller

import "github.com/zalando-incubator/cluster-lifecycle-manager/channel"

type tag struct{}

type versionKey struct {
	environment string
	channel     string
}

// usedVersions contains information about the channel versions of clusters, grouped by environment and channel
type usedVersions map[versionKey]map[channel.ConfigVersion]tag

func newUsedVersions() usedVersions {
	return make(usedVersions)
}

func (cv usedVersions) addCluster(clusterInfo *ClusterInfo) {
	key := versionKey{environment: clusterInfo.Cluster.Environment, channel: clusterInfo.Cluster.Channel}
	if versions, ok := cv[key]; ok {
		versions[clusterInfo.CurrentVersion.ConfigVersion] = tag{}
	} else {
		cv[key] = map[channel.ConfigVersion]tag{clusterInfo.CurrentVersion.ConfigVersion: {}}
	}
}

// fullyUpdated returns true if all clusters with the provided environment and channel use the provided version
func (cv usedVersions) fullyUpdated(environment, channel string, version channel.ConfigVersion) bool {
	key := versionKey{environment: environment, channel: channel}
	if versions, ok := cv[key]; ok {
		if len(versions) != 1 {
			return false
		}

		_, used := versions[version]
		return used
	} else {
		return false
	}
}
