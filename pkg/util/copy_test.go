package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopyValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value map[string]interface{}
	}{
		{
			name:  "empty",
			value: map[string]interface{}{},
		},
		{
			name: "simple",
			value: map[string]interface{}{
				"foo": "bar",
			},
		},
		{
			name: "nested",
			value: map[string]interface{}{
				"foo": map[string]interface{}{
					"bar": "baz",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := CopyValues(tc.value)
			require.Equal(t, tc.value, result)
		})
	}
}
