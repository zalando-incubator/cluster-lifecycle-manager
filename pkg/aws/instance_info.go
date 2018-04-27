package aws

import (
	"encoding/json"
	"reflect"
	"strconv"
	"sync"

	log "github.com/sirupsen/logrus"
)

const (
	gigabyte = 1024 * 1024 * 1024
)

type Instance struct {
	VCPU    int64
	Memory  int64
	Pricing map[string]string
}

type pricing struct {
	OnDemand string `json:"ondemand"`
}

type osPricing struct {
	Linux pricing `json:"linux"`
}

type instanceInfo struct {
	InstanceType string               `json:"instance_type"`
	VCPU         interface{}          `json:"vCPU"`
	Memory       float64              `json:"memory"`
	Pricing      map[string]osPricing `json:"pricing"`
}

var loadedInstances struct {
	once      sync.Once
	instances map[string]Instance
}

func InstanceInfo() map[string]Instance {
	loadedInstances.once.Do(func() {
		loadedInstances.instances = loadInstanceInfo()
	})
	return loadedInstances.instances
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

		result[instance.InstanceType] = Instance{
			VCPU:    vCPU,
			Memory:  int64(instance.Memory * gigabyte),
			Pricing: pricing,
		}
	}

	return result
}
