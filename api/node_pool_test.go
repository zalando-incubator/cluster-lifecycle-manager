package api

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortNodePools(t *testing.T) {
	for _, tc := range []struct {
		msg   string
		pools NodePools
		first string
	}{
		{
			msg: "test master sorted first stays first",
			pools: NodePools([]*NodePool{
				{
					Profile: "master-default",
					Name:    "one",
					MinSize: 2,
					MaxSize: 2,
				},
				{
					Profile: "worker-default",
					Name:    "two",
					MinSize: 3,
					MaxSize: 21,
					ConfigItems: map[string]string{
						"taints": "my-taint=:NoSchedule",
					},
				},
			}),
			first: "one",
		},
		{
			msg: "test master sorted last becomes first",
			pools: NodePools([]*NodePool{
				{
					Profile: "worker-default",
					Name:    "two",
				},
				{
					Profile: "master-default",
					Name:    "one",
					MinSize: 2,
					MaxSize: 2,
				},
			}),
			first: "one",
		},
	} {
		t.Run(tc.msg, func(t *testing.T) {
			sort.Sort(tc.pools)
			if tc.pools[0].Name != tc.first {
				t.Errorf("expected Node Pool '%s' to be sorted first", tc.first)
			}
		})
	}
}

func TestAvailableStorage(t *testing.T) {
	ebsSize := int64(10 * 1024 * 1024)

	for _, tc := range []struct {
		msg           string
		instanceTypes []string
		expected      int64
	}{
		{
			msg:           "no instance storage",
			instanceTypes: []string{"m5.xlarge"},
			expected:      ebsSize,
		},
		{
			msg:           "mixed with/without instance storage",
			instanceTypes: []string{"m5.xlarge", "m5d.xlarge"},
			expected:      ebsSize,
		},
		{
			msg:           "nodes with instance storage",
			instanceTypes: []string{"m5d.8xlarge", "m5d.24xlarge"},
			expected:      0.75 * 2 * 600 * 1024 * 1024 * 1024,
		},
	} {
		t.Run(tc.msg, func(t *testing.T) {
			pool := NodePool{InstanceTypes: tc.instanceTypes}
			size, err := pool.AvailableStorage(ebsSize, 0.75)
			require.NoError(t, err)
			require.Equal(t, tc.expected, size)
		})
	}
}
