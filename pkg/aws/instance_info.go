package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	log "github.com/sirupsen/logrus"
)

const (
	megabyte = 1024 * 1024
	gigabyte = 1024 * megabyte
)

type Instance struct {
	InstanceType              string
	VCPU                      int64
	Memory                    int64
	InstanceStorageDevices    int64
	InstanceStorageDeviceSize int64
}

func (i Instance) AvailableStorage(instanceStorageScaleFactor float64, rootVolumeSize int64, rootVolumeScaleFactor float64) int64 {
	if i.InstanceStorageDevices == 0 {
		return int64(float64(rootVolumeSize) * rootVolumeScaleFactor)
	}
	return int64(instanceStorageScaleFactor * float64(i.InstanceStorageDevices*i.InstanceStorageDeviceSize))
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

func NewInstanceTypesFromAWS(ec2client ec2iface.EC2API) (*InstanceTypes, error) {
	instances := make(map[string]Instance)

	var innerErr error
	err := ec2client.DescribeInstanceTypesPages(&ec2.DescribeInstanceTypesInput{}, func(output *ec2.DescribeInstanceTypesOutput, _ bool) bool {
		for _, instanceType := range output.InstanceTypes {
			var (
				deviceCount, deviceSize int64
			)

			if instanceType.InstanceStorageInfo != nil {
				storageDisks := instanceType.InstanceStorageInfo.Disks
				switch len(storageDisks) {
				case 0:
					// do nothing
				case 1:
					deviceCount = aws.Int64Value(storageDisks[0].Count)
					deviceSize = aws.Int64Value(storageDisks[0].SizeInGB) * gigabyte
				default:
					// doesn't happen at the moment, raise an error so we can decide how to handle this
					innerErr = fmt.Errorf("invalid number of disk sets (%d) for %s, expecting 0 or 1", len(storageDisks), aws.StringValue(instanceType.InstanceType))
					return false
				}
			}

			info := Instance{
				InstanceType:              aws.StringValue(instanceType.InstanceType),
				VCPU:                      aws.Int64Value(instanceType.VCpuInfo.DefaultVCpus),
				Memory:                    aws.Int64Value(instanceType.MemoryInfo.SizeInMiB) * megabyte,
				InstanceStorageDevices:    deviceCount,
				InstanceStorageDeviceSize: deviceSize,
			}
			instances[info.InstanceType] = info
		}
		return true
	})
	if innerErr != nil {
		return nil, innerErr
	}
	if err != nil {
		return nil, err
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
	} else {
		first, err := types.InstanceInfo(instanceTypes[0])
		if err != nil {
			return Instance{}, err
		}

		result := Instance{
			InstanceType:              first.InstanceType,
			VCPU:                      first.VCPU,
			Memory:                    first.Memory,
			InstanceStorageDevices:    first.InstanceStorageDevices,
			InstanceStorageDeviceSize: first.InstanceStorageDeviceSize,
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
		result.InstanceType = "<multiple>"
		return result, nil
	}
}

func min(a int64, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
