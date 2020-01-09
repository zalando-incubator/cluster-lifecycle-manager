package api

import (
	"strings"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
)

// NodePool describes a node pool in a kubernetes cluster.
type NodePool struct {
	DiscountStrategy string            `json:"discount_strategy" yaml:"discount_strategy"`
	InstanceTypes    []string          `json:"instance_types"    yaml:"instance_types"`
	Name             string            `json:"name"              yaml:"name"`
	Profile          string            `json:"profile"           yaml:"profile"`
	MinSize          int64             `json:"min_size"          yaml:"min_size"`
	MaxSize          int64             `json:"max_size"          yaml:"max_size"`
	ConfigItems      map[string]string `json:"config_items"      yaml:"config_items"`

	// Deprecated, only kept here so the existing clusters don't break
	InstanceType string
}

func (np NodePool) IsSpot() bool {
	return np.DiscountStrategy == "spot_max_price" || np.DiscountStrategy == "spot"
}

// AvailableStorage returns the storage available on the instance for the user data based on the
// instance type. If the nodes have instance storage devices, it returns the minimum of the total
// size scaled by scaleFactor, otherwise it returns ebsSize.
func (np NodePool) AvailableStorage(ebsSize int64, scaleFactor float64) (int64, error) {
	instanceInfo, err := aws.SyntheticInstanceInfo(np.InstanceTypes)
	if err != nil {
		return 0, err
	}

	instanceStorageSize := scaleFactor * float64(instanceInfo.InstanceStorageDevices*instanceInfo.InstanceStorageDeviceSize)
	if instanceStorageSize == 0 {
		return ebsSize, nil
	}
	return int64(instanceStorageSize), nil
}

// NodePools is a slice of *NodePool which implements the sort interface to
// sort the pools such that the master pools are ordered first.
type NodePools []*NodePool

// Len returns the length of the NodePools list.
func (p NodePools) Len() int { return len(p) }

// Swap swaps two elements in the NodePools list.
func (p NodePools) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

// Less compares two nodePools. A node Pool is considered less than the other
// if the profile has prefix master.
func (p NodePools) Less(i, j int) bool {
	if strings.HasPrefix(p[i].Profile, "master") {
		return true
	}
	return !strings.HasPrefix(p[j].Profile, "master")
}
