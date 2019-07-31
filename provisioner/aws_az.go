package provisioner

import (
	"sort"
)

// AZInfo tracks information about available AZs based on explicit restrictions or available subnets
type AZInfo struct {
	subnets map[string]string
}

// RestrictAZs returns a new AZInfo that is restricted to provided AZs
func (info *AZInfo) RestrictAZs(availableAZs []string) *AZInfo {
	result := make(map[string]string)

	for _, az := range availableAZs {
		if subnet, ok := info.subnets[az]; ok {
			result[az] = subnet
		}
	}

	return &AZInfo{subnets: result}
}

// Subnets returns a map of AZ->subnet that also contains an entry for the virtual '*' AZ
// TODO drop the *
func (info *AZInfo) SubnetsByAZ() map[string]string {
	result := make(map[string]string)
	for _, az := range info.AvailabilityZones() {
		subnet := info.subnets[az]
		result[az] = subnet

		if existing, ok := result[subnetAllAZName]; ok {
			result[subnetAllAZName] = existing + "," + subnet
		} else {
			result[subnetAllAZName] = subnet
		}
	}
	return result
}

// AvailabilityZones returns a list of available AZs
func (info *AZInfo) AvailabilityZones() []string {
	var result []string
	for az := range info.subnets {
		result = append(result, az)
	}
	sort.Strings(result)
	return result
}
