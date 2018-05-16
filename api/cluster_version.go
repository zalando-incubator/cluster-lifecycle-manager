package api

import (
	"strings"

	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
)

// ClusterVersion is a combination of configuration version from the configuration repository
// and a hash of cluster's metadata.
type ClusterVersion struct {
	ConfigVersion channel.ConfigVersion
	ClusterHash   string
}

// ParseVersion parses a version string into a ConfigVersion. Invalid version strings are parsed
// into empty ClusterVersion structs.
func ParseVersion(version string) *ClusterVersion {
	tokens := strings.Split(version, "#")
	if len(tokens) != 2 {
		return &ClusterVersion{
			ConfigVersion: "",
			ClusterHash:   "",
		}
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
