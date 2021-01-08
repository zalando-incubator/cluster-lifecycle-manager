package aws

import (
	"fmt"
	"sync"

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

type instanceInfo struct {
	InstanceType string      `json:"instance_type"`
	VCPU         interface{} `json:"vCPU"`
	Memory       float64     `json:"memory"`
	EBSAsNVME    bool        `json:"ebs_as_nvme"`
	Storage      struct {
		Devices int64 `json:"devices"`
		NVMESSD bool  `json:"nvme_ssd"`
		Size    int64 `json:"size"`
	} `json:"storage"`
}

var loadedInstances struct {
	instances map[string]Instance
	lock      sync.RWMutex
}

func InitInstanceTypes(ec2client ec2iface.EC2API) error {
	loadedInstances.lock.Lock()
	defer loadedInstances.lock.Unlock()

	if loadedInstances.instances != nil {
		return fmt.Errorf("instance data already initialised")
	}

	newInstances := make(map[string]Instance)

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
			newInstances[info.InstanceType] = info
		}
		return true
	})
	if innerErr != nil {
		return innerErr
	}
	if err != nil {
		return err
	}

	log.Infof("Loaded %d instance types from AWS", len(newInstances))
	loadedInstances.instances = newInstances
	return nil
}

func InstanceInfo(instanceType string) (Instance, error) {
	loadedInstances.lock.RLock()
	defer loadedInstances.lock.RUnlock()

	result, ok := loadedInstances.instances[instanceType]
	if !ok {
		return Instance{}, fmt.Errorf("unknown instance type: %s", instanceType)
	}
	return result, nil
}

// AllInstances returns information for all known AWS EC2 instances.
func AllInstances() map[string]Instance {
	loadedInstances.lock.RLock()
	defer loadedInstances.lock.RUnlock()

	return loadedInstances.instances
}

func SyntheticInstanceInfo(instanceTypes []string) (Instance, error) {
	if len(instanceTypes) == 0 {
		return Instance{}, fmt.Errorf("no instance types provided")
	} else if len(instanceTypes) == 1 {
		return InstanceInfo(instanceTypes[0])
	} else {
		first, err := InstanceInfo(instanceTypes[0])
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
			info, err := InstanceInfo(instanceType)
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
