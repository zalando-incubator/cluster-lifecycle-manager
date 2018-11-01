package aws

import (
	"encoding/json"
	"reflect"
	"strconv"
	"sync"

	"fmt"

	log "github.com/sirupsen/logrus"
)

const (
	gigabyte = 1024 * 1024 * 1024
)

type Instance struct {
	InstanceType        string
	VCPU                int64
	Memory              int64
	NVMEInstanceStorage bool
	Pricing             map[string]string
}

type pricing struct {
	OnDemand string `json:"ondemand"`
}

type osPricing struct {
	Linux pricing `json:"linux"`
}

type instanceInfo struct {
	InstanceType string      `json:"instance_type"`
	VCPU         interface{} `json:"vCPU"`
	Memory       float64     `json:"memory"`
	Storage      struct {
		Devices             int  `json:"devices"`
		NVMESSD             bool `json:"nvme_ssd"`
		NeedsInitialization bool `json:"storage_needs_initialization"`
	} `json:"storage"`
	Pricing map[string]osPricing `json:"pricing"`
}

var loadedInstances struct {
	once      sync.Once
	instances map[string]Instance
}

func InstanceInfo(instanceType string) (Instance, error) {
	loadedInstances.once.Do(func() {
		loadedInstances.instances = loadInstanceInfo()
	})

	result, ok := loadedInstances.instances[instanceType]
	if !ok {
		return Instance{}, fmt.Errorf("unknown instance type: %s", instanceType)
	}
	return result, nil
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
		vCPU := int64(0)
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

		pricing := make(map[string]string)

		for az, azPricing := range instance.Pricing {
			pricing[az] = azPricing.Linux.OnDemand
		}

		nvmeInstanceStorage := instance.Storage.Devices > 0 && instance.Storage.NVMESSD && !instance.Storage.NeedsInitialization

		result[instance.InstanceType] = Instance{
			InstanceType:        instance.InstanceType,
			VCPU:                vCPU,
			Memory:              int64(instance.Memory * gigabyte),
			NVMEInstanceStorage: nvmeInstanceStorage,
			Pricing:             pricing,
		}
	}

	return result
}
