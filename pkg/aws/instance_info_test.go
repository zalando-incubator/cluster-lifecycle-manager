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
			InstanceType:           "i3.4xlarge",
			VCPU:                   16,
			Memory:                 130996502528,
			InstanceStorageDevices: []StorageDevice{{Path: "/dev/nvme0n1", NVME: true}, {Path: "/dev/nvme1n1", NVME: true}},
		},
		{
			InstanceType:           "m5d.4xlarge",
			VCPU:                   16,
			Memory:                 68719476736,
			InstanceStorageDevices: []StorageDevice{{Path: "/dev/nvme1n1", NVME: true}, {Path: "/dev/nvme2n1", NVME: true}},
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
