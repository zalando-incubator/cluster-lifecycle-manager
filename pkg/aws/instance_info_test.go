package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/require"
)

type mockEC2 struct {
}

func (mock *mockEC2) DescribeInstanceTypes(_ context.Context, params *ec2.DescribeInstanceTypesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error) {
	if params.NextToken == nil {
		return &ec2.DescribeInstanceTypesOutput{
			InstanceTypes: []ec2types.InstanceTypeInfo{
				{
					InstanceType: ec2types.InstanceTypeM4Xlarge,
					VCpuInfo: &ec2types.VCpuInfo{
						DefaultVCpus: aws.Int32(4),
					},
					MemoryInfo: &ec2types.MemoryInfo{
						SizeInMiB: aws.Int64(16384),
					},
					InstanceStorageInfo: &ec2types.InstanceStorageInfo{},
					ProcessorInfo: &ec2types.ProcessorInfo{
						// Test that unsupported architectures are correctly ignored.
						SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664Mac, ec2types.ArchitectureTypeI386, ec2types.ArchitectureTypeX8664},
					},
				},
				{
					InstanceType: ec2types.InstanceTypeI34xlarge,
					VCpuInfo: &ec2types.VCpuInfo{
						DefaultVCpus: aws.Int32(16),
					},
					MemoryInfo: &ec2types.MemoryInfo{
						SizeInMiB: aws.Int64(124928),
					},
					InstanceStorageInfo: &ec2types.InstanceStorageInfo{
						Disks: []ec2types.DiskInfo{
							{
								Count:    aws.Int32(2),
								SizeInGB: aws.Int64(1900),
							},
						},
					},
					ProcessorInfo: &ec2types.ProcessorInfo{
						SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664},
					},
				},
				{
					InstanceType: ec2types.InstanceTypeM5Xlarge,
					VCpuInfo: &ec2types.VCpuInfo{
						DefaultVCpus: aws.Int32(4),
					},
					MemoryInfo: &ec2types.MemoryInfo{
						SizeInMiB: aws.Int64(16384),
					},
					InstanceStorageInfo: &ec2types.InstanceStorageInfo{},
					ProcessorInfo: &ec2types.ProcessorInfo{
						SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664},
					},
				},
				{
					InstanceType: ec2types.InstanceTypeM5d4xlarge,
					VCpuInfo: &ec2types.VCpuInfo{
						DefaultVCpus: aws.Int32(16),
					},
					MemoryInfo: &ec2types.MemoryInfo{
						SizeInMiB: aws.Int64(65536),
					},
					InstanceStorageInfo: &ec2types.InstanceStorageInfo{
						Disks: []ec2types.DiskInfo{
							{
								Count:    aws.Int32(2),
								SizeInGB: aws.Int64(300),
							},
						},
					},
					ProcessorInfo: &ec2types.ProcessorInfo{
						SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664},
					},
				},
				{
					InstanceType: ec2types.InstanceTypeM6gXlarge,
					VCpuInfo: &ec2types.VCpuInfo{
						DefaultVCpus: aws.Int32(4),
					},
					MemoryInfo: &ec2types.MemoryInfo{
						SizeInMiB: aws.Int64(16384),
					},
					ProcessorInfo: &ec2types.ProcessorInfo{
						SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeArm64},
					},
				},
			},
		}, nil
	}

	return &ec2.DescribeInstanceTypesOutput{
		InstanceTypes: []ec2types.InstanceTypeInfo{
			{
				InstanceType: ec2types.InstanceTypeC5dXlarge,
				VCpuInfo: &ec2types.VCpuInfo{
					DefaultVCpus: aws.Int32(4),
				},
				MemoryInfo: &ec2types.MemoryInfo{
					SizeInMiB: aws.Int64(8192),
				},
				InstanceStorageInfo: &ec2types.InstanceStorageInfo{
					Disks: []ec2types.DiskInfo{
						{
							Count:    aws.Int32(1),
							SizeInGB: aws.Int64(100),
						},
					},
				},
				ProcessorInfo: &ec2types.ProcessorInfo{
					SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664},
				},
			},
			{
				InstanceType: ec2types.InstanceTypeR5dXlarge,
				VCpuInfo: &ec2types.VCpuInfo{
					DefaultVCpus: aws.Int32(4),
				},
				MemoryInfo: &ec2types.MemoryInfo{
					SizeInMiB: aws.Int64(32768),
				},
				InstanceStorageInfo: &ec2types.InstanceStorageInfo{
					Disks: []ec2types.DiskInfo{
						{
							Count:    aws.Int32(1),
							SizeInGB: aws.Int64(150),
						},
					},
				},
				ProcessorInfo: &ec2types.ProcessorInfo{
					SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664},
				},
			},
			{
				InstanceType: ec2types.InstanceTypeM5dXlarge,
				VCpuInfo: &ec2types.VCpuInfo{
					DefaultVCpus: aws.Int32(4),
				},
				MemoryInfo: &ec2types.MemoryInfo{
					SizeInMiB: aws.Int64(16384),
				},
				InstanceStorageInfo: &ec2types.InstanceStorageInfo{
					Disks: []ec2types.DiskInfo{
						{
							Count:    aws.Int32(1),
							SizeInGB: aws.Int64(150),
						},
					},
				},
				ProcessorInfo: &ec2types.ProcessorInfo{
					SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664},
				},
			},
			{
				InstanceType: ec2types.InstanceTypeR5dLarge,
				VCpuInfo: &ec2types.VCpuInfo{
					DefaultVCpus: aws.Int32(2),
				},
				MemoryInfo: &ec2types.MemoryInfo{
					SizeInMiB: aws.Int64(16384),
				},
				InstanceStorageInfo: &ec2types.InstanceStorageInfo{
					Disks: []ec2types.DiskInfo{
						{
							Count:    aws.Int32(1),
							SizeInGB: aws.Int64(75),
						},
					},
				},
				ProcessorInfo: &ec2types.ProcessorInfo{
					SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664},
				},
			},
		},
	}, nil
}

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

func TestInstanceInfoFromAWS(t *testing.T) {
	instanceTypes, err := NewInstanceTypesFromAWS(context.Background(), &mockEC2{})
	require.NoError(t, err)

	for _, tc := range []Instance{
		{
			InstanceType: "m4.xlarge",
			VCPU:         4,
			Memory:       17179869184,
			Architecture: "amd64",
		},
		{
			InstanceType:              "i3.4xlarge",
			VCPU:                      16,
			Memory:                    130996502528,
			InstanceStorageDevices:    2,
			InstanceStorageDeviceSize: 1900 * 1000 * 1000 * 1000,
			Architecture:              "amd64",
		},
		{
			InstanceType: "m6g.xlarge",
			VCPU:         4,
			Memory:       17179869184,
			Architecture: "arm64",
		},
	} {
		t.Run(string(tc.InstanceType), func(t *testing.T) {
			info, err := instanceTypes.InstanceInfo(tc.InstanceType)
			require.NoError(t, err)
			require.Equal(t, tc.InstanceType, info.InstanceType)
			require.Equal(t, tc.VCPU, info.VCPU)
			require.Equal(t, tc.Memory, info.Memory)
			require.Equal(t, tc.InstanceStorageDevices, info.InstanceStorageDevices)
			require.Equal(t, tc.InstanceStorageDeviceSize, info.InstanceStorageDeviceSize)
			require.Equal(t, tc.Architecture, info.Architecture)
		})
	}
}

func TestInstanceInfoError(t *testing.T) {
	instanceTypes := NewInstanceTypes([]Instance{
		{
			InstanceType: "m4.xlarge",
			VCPU:         4,
			Memory:       17179869184,
		},
	})

	_, err := instanceTypes.InstanceInfo("invalid.type")
	require.Error(t, err)
}

func TestSyntheticInstanceInfo(t *testing.T) {
	instanceTypes := NewInstanceTypes([]Instance{
		{
			InstanceType:              "m4.xlarge",
			VCPU:                      4,
			Memory:                    17179869184,
			InstanceStorageDevices:    0,
			InstanceStorageDeviceSize: 0,
			Architecture:              "amd64",
		},
		{
			InstanceType:              "m5.xlarge",
			VCPU:                      4,
			Memory:                    17179869184,
			InstanceStorageDevices:    0,
			InstanceStorageDeviceSize: 0,
			Architecture:              "amd64",
		},
		{
			InstanceType:              "m5d.xlarge",
			VCPU:                      4,
			Memory:                    17179869184,
			InstanceStorageDevices:    1,
			InstanceStorageDeviceSize: 161061273600,
			Architecture:              "amd64",
		},
		{
			InstanceType:              "m5d.4xlarge",
			VCPU:                      16,
			Memory:                    68719476736,
			InstanceStorageDevices:    2,
			InstanceStorageDeviceSize: 322122547200,
			Architecture:              "amd64",
		},
		{
			InstanceType:              "c5d.xlarge",
			VCPU:                      4,
			Memory:                    8589934592,
			InstanceStorageDevices:    1,
			InstanceStorageDeviceSize: 107374182400,
			Architecture:              "amd64",
		},
		{
			InstanceType:              "r5d.large",
			VCPU:                      2,
			Memory:                    17179869184,
			InstanceStorageDevices:    1,
			InstanceStorageDeviceSize: 80530636800,
			Architecture:              "amd64",
		},
		{
			InstanceType: "m6g.xlarge",
			VCPU:         4,
			Memory:       17179869184,
			Architecture: "arm64",
		},
	})

	for _, tc := range []struct {
		name             string
		instanceTypes    []ec2types.InstanceType
		expectedError    bool
		expectedInstance Instance
	}{
		{
			name:          "one type",
			instanceTypes: []ec2types.InstanceType{"m4.xlarge"},
			expectedInstance: Instance{
				InstanceType: "m4.xlarge",
				VCPU:         4,
				Memory:       17179869184,
				Architecture: "amd64",
			},
		},
		{
			name:          "no types",
			instanceTypes: []ec2types.InstanceType{},
			expectedError: true,
		},
		{
			name:          "invalid type (only one)",
			instanceTypes: []ec2types.InstanceType{"invalid.type"},
			expectedError: true,
		},
		{
			name:          "invalid type (first)",
			instanceTypes: []ec2types.InstanceType{"invalid.type", "m4.xlarge"},
			expectedError: true,
		},
		{
			name:          "invalid type (second)",
			instanceTypes: []ec2types.InstanceType{"m4.xlarge", "invalid.type"},
			expectedError: true,
		},
		{
			name:          "multiple types, different storage device size",
			instanceTypes: []ec2types.InstanceType{"c5d.xlarge", "r5d.large"},
			expectedInstance: Instance{
				InstanceType:              "<multiple>",
				VCPU:                      2,
				Memory:                    8589934592,
				InstanceStorageDevices:    1,
				InstanceStorageDeviceSize: 75 * 1024 * mebibyte,
				Architecture:              "amd64",
			},
		},
		{
			name:          "multiple types, different storage device count",
			instanceTypes: []ec2types.InstanceType{"m5d.xlarge", "m5d.4xlarge"},
			expectedInstance: Instance{
				InstanceType:              "<multiple>",
				VCPU:                      4,
				Memory:                    17179869184,
				InstanceStorageDevices:    1,
				InstanceStorageDeviceSize: 150 * 1024 * mebibyte,
				Architecture:              "amd64",
			},
		},
		{
			name:          "multiple types, some with storage, some without",
			instanceTypes: []ec2types.InstanceType{"m5d.xlarge", "m5.xlarge"},
			expectedInstance: Instance{
				InstanceType: "<multiple>",
				VCPU:         4,
				Memory:       17179869184,
				Architecture: "amd64",
			},
		},
		{
			name:          "one type with arm64",
			instanceTypes: []ec2types.InstanceType{"m6g.xlarge"},
			expectedInstance: Instance{
				InstanceType: "m6g.xlarge",
				VCPU:         4,
				Memory:       17179869184,
				Architecture: "arm64",
			},
		},
		{
			name:          "multiple types, different architectures, amd64 architecture first",
			instanceTypes: []ec2types.InstanceType{"m5.xlarge", "m6g.xlarge"},
			expectedInstance: Instance{
				InstanceType: "<multiple>",
				VCPU:         4,
				Memory:       17179869184,
				Architecture: "amd64",
			},
		},
		{
			name:          "multiple types, different architectures, arm64 architecture first",
			instanceTypes: []ec2types.InstanceType{"m6g.xlarge", "m5.xlarge"},
			expectedInstance: Instance{
				InstanceType: "<multiple>",
				VCPU:         4,
				Memory:       17179869184,
				Architecture: "arm64",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, err := instanceTypes.SyntheticInstanceInfo(tc.instanceTypes)
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedInstance.InstanceType, info.InstanceType)
				require.Equal(t, tc.expectedInstance.VCPU, info.VCPU)
				require.Equal(t, tc.expectedInstance.Memory, info.Memory)
				require.Equal(t, tc.expectedInstance.InstanceStorageDevices, info.InstanceStorageDevices)
				require.Equal(t, tc.expectedInstance.InstanceStorageDeviceSize, info.InstanceStorageDeviceSize)
				require.Equal(t, tc.expectedInstance.Architecture, info.Architecture)
			}
		})
	}
}

func TestMemoryFraction(t *testing.T) {
	instance := Instance{
		InstanceType:              "<multiple>",
		VCPU:                      2,
		Memory:                    8589934592,
		InstanceStorageDevices:    1,
		InstanceStorageDeviceSize: 75 * 1024 * mebibyte,
		Architecture:              "amd64",
	}

	require.Equal(t, int64(4294967296), instance.MemoryFraction(50))
}
