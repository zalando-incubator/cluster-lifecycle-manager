package api

import (
	"strings"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
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

func (np NodePool) IsKarpenter() bool {
	return np.Profile == "worker-karpenter"
}

func (np NodePool) Taints() []*corev1.Taint {
	conf, exist := np.ConfigItems["taints"]
	if !exist {
		return nil
	}
	var taints []*corev1.Taint
	for _, t := range strings.Split(conf, ",") {
		taint, err := kubernetes.ParseTaint(t)
		if err == nil {
			taints = append(taints, taint)
		}
	}
	return taints
}
