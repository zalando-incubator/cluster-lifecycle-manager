package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		expected string
	}{
		{
			name:    "empty",
			version: "",
			expected: "#",
		},
		{
			name:    "simple",
			version: "foo#bar",
			expected: "foo#bar",
		},
		{
			name:    "missing hash",
			version: "foo",
			expected: "#",
		},
		{
			name:    "missing version",
			version: "#bar",
			expected: "#bar",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version := ParseVersion(tc.version)
			result := version.String()
			require.Equal(t, tc.expected, result)
		})
	}
}