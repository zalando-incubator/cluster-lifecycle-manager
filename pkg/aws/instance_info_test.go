package aws

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAvailableStorage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		devices    int64
		deviceSize int64
		expected   int64
	}{
		{
			name:       "ebs only",
			devices:    0,
			deviceSize: 0,
			expected:   42949672960,
		},
		{
			name:       "instance storage, 1 device",
			devices:    1,
			deviceSize: 107374182400,
			expected:   102005473280,
		},
		{
			name:       "instance storage, multiple devices",
			devices:    4,
			deviceSize: 107374182400,
			expected:   408021893120,
		},
	} {
		info := Instance{
			InstanceType:              "test",
			InstanceStorageDevices:    tc.devices,
			InstanceStorageDeviceSize: tc.deviceSize,
		}
		require.Equal(t, tc.expected, info.AvailableStorage(0.95, 50*1024*1024*1024, 0.8))
	}
}

func TestInstanceInfo(t *testing.T) {
	for _, tc := range []Instance{
		{
			InstanceType: "m4.xlarge",
			VCPU:         4,
			Memory:       17179869184,
		},
		{
			InstanceType:              "i3.4xlarge",
			VCPU:                      16,
			Memory:                    130996502528,
			InstanceStorageDevices:    2,
			InstanceStorageDeviceSize: 1900 * gigabyte,
		},
		{
			InstanceType: "i2.2xlarge",
			VCPU:         8,
			Memory:       65498251264,
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
			require.Equal(t, tc.InstanceStorageDevices, info.InstanceStorageDevices)
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
			name:          "multiple types, different storage device size",
			instanceTypes: []string{"c5d.xlarge", "r5d.large"},
			expectedInstance: Instance{
				InstanceType:              "<multiple>",
				VCPU:                      2,
				Memory:                    8589934592,
				InstanceStorageDevices:    1,
				InstanceStorageDeviceSize: 75 * gigabyte,
			},
		},
		{
			name:          "multiple types, different storage device count",
			instanceTypes: []string{"m5d.xlarge", "m5d.4xlarge"},
			expectedInstance: Instance{
				InstanceType:              "<multiple>",
				VCPU:                      4,
				Memory:                    17179869184,
				InstanceStorageDevices:    1,
				InstanceStorageDeviceSize: 150 * gigabyte,
			},
		},
		{
			name:          "multiple types, some with storage, some without",
			instanceTypes: []string{"m5d.xlarge", "m5.xlarge"},
			expectedInstance: Instance{
				InstanceType: "<multiple>",
				VCPU:         4,
				Memory:       17179869184,
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
				require.Equal(t, tc.expectedInstance.InstanceStorageDevices, info.InstanceStorageDevices)
				require.Equal(t, tc.expectedInstance.InstanceStorageDeviceSize, info.InstanceStorageDeviceSize)
			}
		})
	}
}
