package api

import (
	"strings"
)

// NodePool describes a node pool in a kubernetes cluster.
type NodePool struct {
	DiscountStrategy string            `json:"discount_strategy" yaml:"discount_strategy"`
	InstanceTypes    []string          `json:"instance_types"    yaml:"instance_types"`
	Name             string            `json:"name"              yaml:"name"`
	Profile          string            `json:"profile"           yaml:"profile"`
	MinSize          int64             `json:"min_size"          yaml:"min_size"`
	MaxSize          int64             `json:"max_size"          yaml:"max_size"`
	ConfigItems      map[string]string `json:"config_items"      yaml:"config_items"`

	// Deprecated, only kept here so the existing clusters don't break
	InstanceType string
}

func (np NodePool) IsSpot() bool {
	return np.DiscountStrategy == "spot_max_price" || np.DiscountStrategy == "spot"
}

func (np NodePool) IsMaster() bool {
	return strings.Contains(np.Profile, "master")
}

type Taint struct {
	Key    string
	Value  string
	Effect string
}

func (np NodePool) Taints() []Taint {
	conf, exist := np.ConfigItems["taints"]
	if !exist {
		return nil
	}
	var taints []Taint
	for _, t := range strings.Split(conf, ",") {
		taintData := strings.FieldsFunc(t, func(r rune) bool {
			return r == '=' || r == ':'
		})
		if len(taintData) == 3 {
			taints = append(taints, Taint{
				Key:    taintData[0],
				Value:  taintData[1],
				Effect: taintData[2],
			})
		} else if len(taintData) == 2 {
			taints = append(taints, Taint{
				Key:    taintData[0],
				Effect: taintData[1],
			})
		}
	}
	return taints
}
