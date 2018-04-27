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
				&NodePool{
					Profile: "master/default",
					Name:    "one",
					MinSize: 2,
					MaxSize: 2,
				},
				&NodePool{
					Profile: "worker/default",
					Name:    "two",
					MinSize: 3,
					MaxSize: 20,
				},
			}),
			first: "one",
		},
		{
			msg: "test master sorted last becomes first",
			pools: NodePools([]*NodePool{
				&NodePool{
					Profile: "worker/default",
					Name:    "two",
				},
				&NodePool{
					Profile: "master/default",
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
