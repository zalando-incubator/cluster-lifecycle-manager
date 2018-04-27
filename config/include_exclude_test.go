package config

import (
	"regexp"
	"testing"
)

func TestIncludeExcludeFilter(t *testing.T) {
	for _, ti := range []struct {
		msg     string
		filter  IncludeExcludeFilter
		item    string
		allowed bool
	}{
		{
			msg: "Default filter grants access to any item",
			filter: IncludeExcludeFilter{
				Include: regexp.MustCompile(DefaultInclude),
				Exclude: regexp.MustCompile(DefaultExclude),
			},
			item:    "a",
			allowed: true,
		},
		{
			msg: "Prefix include filter grants access all items with the prefix",
			filter: IncludeExcludeFilter{
				Include: regexp.MustCompile("gke:*"),
				Exclude: regexp.MustCompile(DefaultExclude),
			},
			item:    "gke:123",
			allowed: true,
		},
		{
			msg: "Prefix include filter does not grant access to items without the prefix",
			filter: IncludeExcludeFilter{
				Include: regexp.MustCompile("gke:*"),
				Exclude: regexp.MustCompile(DefaultExclude),
			},
			item:    "aws:123",
			allowed: false,
		},
		{
			msg: "Exclude has precedence over Include",
			filter: IncludeExcludeFilter{
				Include: regexp.MustCompile("gke:*"),
				Exclude: regexp.MustCompile("gke:*"),
			},
			item:    "gke:123",
			allowed: false,
		},
	} {
		t.Run(ti.msg, func(t *testing.T) {
			if ti.allowed != ti.filter.Allowed(ti.item) {
				t.Errorf("Expected %s to be allowed: %t, got allowed %t", ti.item, ti.allowed, ti.filter.Allowed(ti.item))
			}
		})
	}
}
