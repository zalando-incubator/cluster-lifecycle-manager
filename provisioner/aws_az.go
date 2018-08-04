package provisioner

import (
	"sort"
)

const (
	SubnetAllAZName = "*"
)

// AZInfo tracks information about available AZs based on explicit restrictions or available subnets
type AZInfo struct {
	Subnets map[string]string
}

// RestrictAZs returns a new AZInfo that is restricted to provided AZs
func (info *AZInfo) RestrictAZs(availableAZs []string) *AZInfo {
	result := make(map[string]string)

	for _, az := range availableAZs {
		if subnet, ok := info.Subnets[az]; ok {
			result[az] = subnet
		}
	}

	return &AZInfo{Subnets: result}
}

// Subnets returns a map of AZ->subnet that also contains an entry for the virtual '*' AZ
// TODO drop the *
func (info *AZInfo) SubnetsByAZ() map[string]string {
	result := make(map[string]string)
	for _, az := range info.AvailabilityZones() {
		subnet := info.Subnets[az]
		result[az] = subnet

		if existing, ok := result[SubnetAllAZName]; ok {
			result[SubnetAllAZName] = existing + "," + subnet
		} else {
			result[SubnetAllAZName] = subnet
		}
	}
	return result
}

// AvailabilityZones returns a list of available AZs
func (info *AZInfo) AvailabilityZones() []string {
	var result []string
	for az := range info.Subnets {
		result = append(result, az)
	}
	sort.Strings(result)
	return result
}
