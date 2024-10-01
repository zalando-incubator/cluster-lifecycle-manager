package provisioner

import (
	"sort"
)

// SubnetInfo has information about a subnet.
type SubnetInfo struct {
	SubnetID        string
	SubnetIPV6CIDRs []string
}

// AZInfo tracks information about available AZs based on explicit restrictions or available subnets
type AZInfo struct {
	subnets map[string]SubnetInfo
}

// RestrictAZs returns a new AZInfo that is restricted to provided AZs
func (info *AZInfo) RestrictAZs(availableAZs []string) *AZInfo {
	result := make(map[string]SubnetInfo)

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
		result[az] = subnet.SubnetID

		if existing, ok := result[subnetAllAZName]; ok {
			result[subnetAllAZName] = existing + "," + subnet.SubnetID
		} else {
			result[subnetAllAZName] = subnet.SubnetID
		}
	}
	return result
}

// SubnetIPv6CIDRs returns a list of available subnet IPV6 CIDRs.
func (info *AZInfo) SubnetIPv6CIDRs() []string {
	var result []string
	for _, subnetInfo := range info.subnets {
		result = append(result, subnetInfo.SubnetIPV6CIDRs...)
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
