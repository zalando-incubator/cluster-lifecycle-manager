package aws

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"sync"

	log "github.com/sirupsen/logrus"
)

const (
	gigabyte = 1024 * 1024 * 1024
)

type Instance struct {
	InstanceType              string
	VCPU                      int64
	Memory                    int64
	InstanceStorageDevices    int64
	InstanceStorageDeviceSize int64
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
	once      sync.Once
	instances map[string]Instance
}

func InstanceInfo(instanceType string) (Instance, error) {
	AllInstances()

	result, ok := loadedInstances.instances[instanceType]
	if !ok {
		return Instance{}, fmt.Errorf("unknown instance type: %s", instanceType)
	}
	return result, nil
}

// AllInstances returns information for all known AWS EC2 instances.
func AllInstances() map[string]Instance {
	loadedInstances.once.Do(func() {
		loadedInstances.instances = loadInstanceInfo()
	})

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

func loadInstanceInfo() map[string]Instance {
	data := MustAsset("instances.json")

	var instanceInfo []instanceInfo
	err := json.Unmarshal(data, &instanceInfo)
	if err != nil {
		panic(err)
	}

	result := make(map[string]Instance)

	for _, instance := range instanceInfo {
		var vCPU int64
		switch v := instance.VCPU.(type) {
		case float64:
			vCPU = int64(v)
		case string:
			vCPU, err = strconv.ParseInt(v, 10, 32)
			if err != nil {
				log.Warnf("Unable to determine vCPU count for %s: %s", instance.InstanceType, err)
				continue
			}
		default:
			log.Warnf("Unable to determine vCPU count for %s: %s", instance.InstanceType, reflect.TypeOf(instance.VCPU))
			continue
		}

		instanceInfo := Instance{
			InstanceType: instance.InstanceType,
			VCPU:         vCPU,
			Memory:       int64(instance.Memory * gigabyte),
		}

		if instance.Storage.NVMESSD {
			instanceInfo.InstanceStorageDevices = instance.Storage.Devices
			instanceInfo.InstanceStorageDeviceSize = gigabyte * instance.Storage.Size
		}

		result[instance.InstanceType] = instanceInfo
	}

	return result
}
