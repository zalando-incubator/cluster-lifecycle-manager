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
	InstanceType string
	VCPU         int64
	Memory       int64
}

type instanceInfo struct {
	InstanceType string      `json:"instance_type"`
	VCPU         interface{} `json:"vCPU"`
	Memory       float64     `json:"memory"`
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
			InstanceType: first.InstanceType,
			VCPU:         first.VCPU,
			Memory:       first.Memory,
		}
		for _, instanceType := range instanceTypes[1:] {
			info, err := InstanceInfo(instanceType)
			if err != nil {
				return Instance{}, err
			}

			if info.VCPU < result.VCPU {
				result.VCPU = info.VCPU
			}
			if info.Memory < result.Memory {
				result.Memory = info.Memory
			}
		}
		result.InstanceType = "<multiple>"
		return result, nil
	}
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

		result[instance.InstanceType] = instanceInfo
	}

	return result
}
