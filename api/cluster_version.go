package api

import (
	"strings"

	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
)

type ClusterVersion struct {
	ConfigVersion channel.ConfigVersion
	ClusterHash   string
}

func NewClusterVersion(configVersion channel.ConfigVersion, clusterHash string) *ClusterVersion {
	return &ClusterVersion{
		ConfigVersion: configVersion,
		ClusterHash:   clusterHash,
	}
}

func ParseVersion(version string) *ClusterVersion {
	tokens := strings.Split(version, "#")
	if len(tokens) != 2 {
		return NewClusterVersion("", "")
	}
	return &ClusterVersion{
		ConfigVersion: channel.ConfigVersion(tokens[0]),
		ClusterHash:   tokens[1],
	}
}

func (version *ClusterVersion) String() string {
	if version == nil {
		return ""
	}

	return string(version.ConfigVersion) + "#" + version.ClusterHash
}
