package provisioner

import (
	"fmt"
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

func TestMaxNodes(t *testing.T) {
	azInfo := func(cidrs ...string) *AZInfo {
		subnets := make(map[string]SubnetInfo, len(cidrs))
		for i, cidr := range cidrs {
			subnets[fmt.Sprintf("az-%d", i)] = SubnetInfo{SubnetID: fmt.Sprintf("subnet-%d", i), SubnetIPV4CIDR: cidr}
		}
		return &AZInfo{subnets: subnets}
	}

	for _, tc := range []struct {
		name          string
		info          *AZInfo
		percent       int64
		expected      int64
		expectedFound bool
		expectedError bool
	}{
		{
			name:          "3x /22, 90%",
			info:          azInfo("10.153.132.0/22", "10.153.140.0/22", "10.153.152.0/22"),
			percent:       90,
			expected:      2751,
			expectedFound: true,
		},
		{
			name:          "3x /22, 100%",
			info:          azInfo("10.153.132.0/22", "10.153.140.0/22", "10.153.152.0/22"),
			percent:       100,
			expected:      3057,
			expectedFound: true,
		},
		{
			name:          "3x /24, 90%",
			info:          azInfo("10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24"),
			percent:       90,
			expected:      677,
			expectedFound: true,
		},
		{
			name:          "mixed subnet sizes are summed",
			info:          azInfo("10.0.0.0/22", "10.0.4.0/23", "10.0.6.0/24"),
			percent:       100,
			expected:      1777,
			expectedFound: true,
		},
		{
			name:          "smallest subnet size",
			info:          azInfo("10.0.0.0/28"),
			percent:       100,
			expected:      11,
			expectedFound: true,
		},
		{
			name:          "subnets without an IPv4 CIDR are ignored",
			info:          &AZInfo{subnets: map[string]SubnetInfo{"az-0": {SubnetID: "subnet-0"}, "az-1": {SubnetID: "subnet-1", SubnetIPV4CIDR: "10.0.0.0/24"}}},
			percent:       100,
			expected:      251,
			expectedFound: true,
		},
		{
			name:          "no subnets",
			info:          &AZInfo{},
			percent:       90,
			expectedFound: false,
		},
		{
			name:          "no subnet with an IPv4 CIDR",
			info:          &AZInfo{subnets: map[string]SubnetInfo{"az-0": {SubnetID: "subnet-0"}}},
			percent:       90,
			expectedFound: false,
		},
		{
			name:          "error: percent too low",
			info:          azInfo("10.0.0.0/22"),
			percent:       0,
			expectedError: true,
		},
		{
			name:          "error: percent too high",
			info:          azInfo("10.0.0.0/22"),
			percent:       101,
			expectedError: true,
		},
		{
			name:          "error: invalid CIDR",
			info:          azInfo("foo"),
			percent:       90,
			expectedError: true,
		},
		{
			name:          "error: IPv6 CIDR",
			info:          azInfo("2a05:d024:20:1003::/64"),
			percent:       90,
			expectedError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			maxNodes, found, err := tc.info.MaxNodes(tc.percent)
			if tc.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedFound, found)
			require.EqualValues(t, tc.expected, maxNodes)
		})
	}
}
