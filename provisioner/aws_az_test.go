package provisioner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	info = &AZInfo{
		subnets: map[string]SubnetInfo{
			"eu-central-1a": {
				SubnetID:        "subnet-1a",
				SubnetIPV6CIDRs: []string{"2001:db8::/64"},
			},
			"eu-central-1b": {
				SubnetID:        "subnet-1b",
				SubnetIPV6CIDRs: []string{"2001:db8::/64"},
			},
			"eu-central-1c": {
				SubnetID:        "subnet-1c",
				SubnetIPV6CIDRs: []string{"2001:db8::/64"},
			},
		},
	}
)

func TestSubnetsByAZ(t *testing.T) {
	expected := map[string]string{
		"*":             "subnet-1a,subnet-1b,subnet-1c",
		"eu-central-1a": "subnet-1a",
		"eu-central-1b": "subnet-1b",
		"eu-central-1c": "subnet-1c",
	}
	require.Equal(t, expected, info.SubnetsByAZ())
}

func TestAvailabilityZones(t *testing.T) {
	require.Equal(t, []string{"eu-central-1a", "eu-central-1b", "eu-central-1c"}, info.AvailabilityZones())
}

func TestRestrictAZs(t *testing.T) {
	restricted := info.RestrictAZs([]string{"eu-central-1b", "eu-central-1d"})
	require.NotEqual(t, info, restricted)
	require.Equal(t, map[string]string{"*": "subnet-1b", "eu-central-1b": "subnet-1b"}, restricted.SubnetsByAZ())
	require.Equal(t, []string{"eu-central-1b"}, restricted.AvailabilityZones())
}

func TestSubnetIPv6CIDRs(t *testing.T) {
	require.Equal(t, []string{"2001:db8::/64", "2001:db8::/64", "2001:db8::/64"}, info.SubnetIPv6CIDRs())
}
