package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	log "github.com/sirupsen/logrus"
)

const (
	mebibyte = 1024 * 1024

	gigabyte = 1000 * 1000 * 1000

	archX86_64 = "x86_64"
	archAMD64  = "amd64"
	archARM64  = "arm64"
)

type Instance struct {
	InstanceType              string
	VCPU                      int32
	Memory                    int64
	InstanceStorageDevices    int32
	InstanceStorageDeviceSize int64
	Architecture              string
}

func (i Instance) AvailableStorage(instanceStorageScaleFactor float64, rootVolumeSize int64, rootVolumeScaleFactor float64) int64 {
	if i.InstanceStorageDevices == 0 {
		return int64(float64(rootVolumeSize) * rootVolumeScaleFactor)
	}
	return int64(instanceStorageScaleFactor * float64(int64(i.InstanceStorageDevices)*i.InstanceStorageDeviceSize))
}

func (i Instance) MemoryFraction(percent int64) int64 {
	return percent * i.Memory / 100
}

type InstanceTypes struct {
	instances map[string]Instance
}

func NewInstanceTypes(instanceData []Instance) *InstanceTypes {
	result := make(map[string]Instance)
	for _, instanceType := range instanceData {
		result[instanceType.InstanceType] = instanceType
	}
	return &InstanceTypes{instances: result}
}

func NewInstanceTypesFromAWS(ec2client EC2API) (*InstanceTypes, error) {
	instances := make(map[string]Instance)

	var innerErr error

	paginator := ec2.NewDescribeInstanceTypesPaginator(ec2client, &ec2.DescribeInstanceTypesInput{})

pagination:
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(context.TODO())
		if err != nil {
			return nil, err
		}
		for _, instanceType := range output.InstanceTypes {
			var (
				deviceCount int32
				deviceSize  int64
			)

			if instanceType.InstanceStorageInfo != nil {
				storageDisks := instanceType.InstanceStorageInfo.Disks
				switch len(storageDisks) {
				case 0:
					// do nothing
				case 1:
					deviceCount = aws.ToInt32(storageDisks[0].Count)
					deviceSize = aws.ToInt64(storageDisks[0].SizeInGB) * gigabyte
				default:
					// doesn't happen at the moment, raise an error so we can decide how to handle this
					innerErr = fmt.Errorf("invalid number of disk sets (%d) for %s, expecting 0 or 1", len(storageDisks), string(instanceType.InstanceType))
					break pagination
				}
			}

			cpuArch, err := getCompatibleCPUArchitecture(instanceType)
			if err != nil {
				// Let's skip any instance types that we don't support.
				log.Debug(err.Error())
				continue
			}

			info := Instance{
				InstanceType:              string(instanceType.InstanceType),
				VCPU:                      aws.ToInt32(instanceType.VCpuInfo.DefaultVCpus),
				Memory:                    aws.ToInt64(instanceType.MemoryInfo.SizeInMiB) * mebibyte,
				InstanceStorageDevices:    deviceCount,
				InstanceStorageDeviceSize: deviceSize,
				Architecture:              cpuArch,
			}
			instances[info.InstanceType] = info
		}

	}
	if innerErr != nil {
		return nil, innerErr
	}
	log.Debugf("Loaded %d instance types from AWS", len(instances))
	return &InstanceTypes{instances: instances}, nil
}

func (types *InstanceTypes) InstanceInfo(instanceType string) (Instance, error) {
	result, ok := types.instances[instanceType]
	if !ok {
		return Instance{}, fmt.Errorf("unknown instance type: %s", instanceType)
	}
	return result, nil
}

// AllInstances returns information for all known AWS EC2 instances.
func (types *InstanceTypes) AllInstances() map[string]Instance {
	return types.instances
}

func (types *InstanceTypes) SyntheticInstanceInfo(instanceTypes []string) (Instance, error) {
	if len(instanceTypes) == 0 {
		return Instance{}, fmt.Errorf("no instance types provided")
	} else if len(instanceTypes) == 1 {
		return types.InstanceInfo(instanceTypes[0])
	}

	first, err := types.InstanceInfo(instanceTypes[0])
	if err != nil {
		return Instance{}, err
	}

	result := Instance{
		InstanceType:              "<multiple>",
		VCPU:                      first.VCPU,
		Memory:                    first.Memory,
		InstanceStorageDevices:    first.InstanceStorageDevices,
		InstanceStorageDeviceSize: first.InstanceStorageDeviceSize,
		Architecture:              first.Architecture,
	}
	for _, instanceType := range instanceTypes[1:] {
		info, err := types.InstanceInfo(instanceType)
		if err != nil {
			return Instance{}, err
		}

		result.VCPU = min(result.VCPU, info.VCPU)
		result.Memory = min(result.Memory, info.Memory)
		result.InstanceStorageDeviceSize = min(result.InstanceStorageDeviceSize, info.InstanceStorageDeviceSize)
		result.InstanceStorageDevices = min(result.InstanceStorageDevices, info.InstanceStorageDevices)
	}
	return result, nil
}

// getCompatibleCPUArchitecture returns a single compatible CPU architecture. It's either `amd64` or `arm64`.
// Other intance types might return 32-bit or macos specific types which will be ignored.
func getCompatibleCPUArchitecture(instanceType types.InstanceTypeInfo) (string, error) {
	supportedArchitectures := instanceType.ProcessorInfo.SupportedArchitectures

	if contains(supportedArchitectures, types.ArchitectureTypeX8664) {
		return archAMD64, nil
	}
	if contains(supportedArchitectures, types.ArchitectureTypeArm64) {
		return archARM64, nil
	}

	return "", fmt.Errorf("didn't find compatible cpu architecture within '%v'", supportedArchitectures)
}

func min[T int64 | int32](a T, b T) T {
	if a < b {
		return a
	}
	return b
}

func contains[T string | types.ArchitectureType](s []T, e T) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
