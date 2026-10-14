package provisioner

import (
	"fmt"
	"net"
	"sort"
)

// SubnetInfo has information about a subnet.
type SubnetInfo struct {
	SubnetID        string
	SubnetIPV4CIDR  string
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

// awsReservedSubnetAddresses is the number of addresses AWS reserves in every
// subnet (the first four and the last one).
const awsReservedSubnetAddresses = 5

// MaxNodes returns the maximum number of nodes that fit in the IPv4 address
// space of the subnets, considering only usablePercent percent of the usable
// addresses to leave room for other consumers of the subnets. It returns false
// if none of the subnets has an IPv4 CIDR.
//
// This is the limit for clusters where pods don't get their addresses from a
// per-node CIDR carved out of a pod CIDR (e.g. IPv6 EKS clusters), because
// each node then only needs a single IPv4 address from its subnet.
func (info *AZInfo) MaxNodes(usablePercent int64) (int64, bool, error) {
	if usablePercent < 1 || usablePercent > 100 {
		return 0, false, fmt.Errorf("invalid value for usablePercent: %d, must be between 1 and 100", usablePercent)
	}

	var addresses int64
	var found bool
	for _, subnetInfo := range info.subnets {
		if subnetInfo.SubnetIPV4CIDR == "" {
			continue
		}

		_, ipNet, err := net.ParseCIDR(subnetInfo.SubnetIPV4CIDR)
		if err != nil {
			return 0, false, fmt.Errorf("invalid IPv4 CIDR %q of subnet %s: %v", subnetInfo.SubnetIPV4CIDR, subnetInfo.SubnetID, err)
		}

		ones, bits := ipNet.Mask.Size()
		if bits != 32 {
			return 0, false, fmt.Errorf("CIDR %q of subnet %s is not an IPv4 CIDR", subnetInfo.SubnetIPV4CIDR, subnetInfo.SubnetID)
		}

		addresses += int64(1)<<(32-ones) - awsReservedSubnetAddresses
		found = true
	}

	return addresses * usablePercent / 100, found, nil
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
