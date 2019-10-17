package aws

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstanceInfo(t *testing.T) {
	for _, tc := range []Instance{
		{
			InstanceType: "m4.xlarge",
			VCPU:         4,
			Memory:       17179869184,
		},
		{
			InstanceType: "i3.4xlarge",
			VCPU:         16,
			Memory:       130996502528,
		},
		{
			InstanceType: "r3.large",
			VCPU:         2,
			Memory:       16374562816,
		},
	} {
		t.Run(tc.InstanceType, func(t *testing.T) {
			info, err := InstanceInfo(tc.InstanceType)
			require.NoError(t, err)
			require.Equal(t, tc.InstanceType, info.InstanceType)
			require.Equal(t, tc.VCPU, info.VCPU)
			require.Equal(t, tc.Memory, info.Memory)
		})
	}
}

func TestInstanceInfoError(t *testing.T) {
	_, err := InstanceInfo("invalid.type")
	require.Error(t, err)
}

func TestSyntheticInstanceInfo(t *testing.T) {
	for _, tc := range []struct {
		name             string
		instanceTypes    []string
		expectedError    bool
		expectedInstance Instance
	}{
		{
			name:          "one type",
			instanceTypes: []string{"m4.xlarge"},
			expectedInstance: Instance{
				InstanceType: "m4.xlarge",
				VCPU:         4,
				Memory:       17179869184,
			},
		},
		{
			name:          "no types",
			instanceTypes: []string{},
			expectedError: true,
		},
		{
			name:          "invalid type (only one)",
			instanceTypes: []string{"invalid.type"},
			expectedError: true,
		},
		{
			name:          "invalid type (first)",
			instanceTypes: []string{"invalid.type", "m4.xlarge"},
			expectedError: true,
		},
		{
			name:          "invalid type (second)",
			instanceTypes: []string{"m4.xlarge", "invalid.type"},
			expectedError: true,
		},
		{
			name:          "multiple types",
			instanceTypes: []string{"c5.xlarge", "r5d.large"},
			expectedInstance: Instance{
				InstanceType: "<multiple>",
				VCPU:         2,
				Memory:       8589934592,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, err := SyntheticInstanceInfo(tc.instanceTypes)
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedInstance.InstanceType, info.InstanceType)
				require.Equal(t, tc.expectedInstance.VCPU, info.VCPU)
				require.Equal(t, tc.expectedInstance.Memory, info.Memory)
			}
		})
	}
}
