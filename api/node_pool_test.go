package api

import (
	"sort"
	"testing"
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
