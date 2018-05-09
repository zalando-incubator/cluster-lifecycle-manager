package api

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
)

// Cluster describes a kubernetes cluster and related configuration.
type Cluster struct {
	Alias                 string            `json:"alias"                  yaml:"alias"`
	APIServerURL          string            `json:"api_server_url"         yaml:"api_server_url"`
	Channel               string            `json:"channel"                yaml:"channel"`
	ConfigItems           map[string]string `json:"config_items"           yaml:"config_items"`
	CriticalityLevel      int32             `json:"criticality_level"      yaml:"criticality_level"`
	Environment           string            `json:"environment"            yaml:"environment"`
	ID                    string            `json:"id"                     yaml:"id"`
	InfrastructureAccount string            `json:"infrastructure_account" yaml:"infrastructure_account"`
	LifecycleStatus       string            `json:"lifecycle_status"       yaml:"lifecycle_status"`
	LocalID               string            `json:"local_id"               yaml:"local_id"`
	NodePools             []*NodePool       `json:"node_pools"             yaml:"node_pools"`
	Provider              string            `json:"provider"               yaml:"provider"`
	Region                string            `json:"region"                 yaml:"region"`
	Status                *ClusterStatus    `json:"status"                 yaml:"status"`
	Outputs               map[string]string `json:"outputs"                yaml:"outputs"`
	Owner                 string            `json:"owner"                  yaml:"owner"`
}

// Version returns the version derived from a sha1 hash of the cluster struct
// and the channel config version.
func (cluster *Cluster) Version(channelVersion channel.ConfigVersion) (string, error) {
	state := new(bytes.Buffer)

	_, err := state.WriteString(cluster.ID)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.InfrastructureAccount)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.LocalID)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.APIServerURL)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.Channel)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.Environment)
	if err != nil {
		return "", err
	}
	err = binary.Write(state, binary.LittleEndian, cluster.CriticalityLevel)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.LifecycleStatus)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.Provider)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.Region)
	if err != nil {
		return "", err
	}

	// config items are sorted by key to produce a predictable string for
	// hashing.
	keys := make([]string, 0, len(cluster.ConfigItems))
	for key := range cluster.ConfigItems {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, err = state.WriteString(key)
		if err != nil {
			return "", err
		}
		_, err = state.WriteString(cluster.ConfigItems[key])
		if err != nil {
			return "", err
		}
	}

	// node pools
	for _, nodePool := range cluster.NodePools {
		_, err = state.WriteString(nodePool.Name)
		if err != nil {
			return "", err
		}
		_, err = state.WriteString(nodePool.Profile)
		if err != nil {
			return "", err
		}
		_, err = state.WriteString(nodePool.InstanceType)
		if err != nil {
			return "", err
		}
		_, err = state.WriteString(nodePool.DiscountStrategy)
		if err != nil {
			return "", err
		}
		err = binary.Write(state, binary.LittleEndian, nodePool.MinSize)
		if err != nil {
			return "", err
		}
		err = binary.Write(state, binary.LittleEndian, nodePool.MaxSize)
		if err != nil {
			return "", err
		}
	}

	// sha1 hash the cluster content
	hasher := sha1.New()
	_, err = hasher.Write(state.Bytes())
	if err != nil {
		return "", err
	}
	sha := base64.RawURLEncoding.EncodeToString(hasher.Sum(nil))

	return fmt.Sprintf("%s#%s", string(channelVersion), sha), nil
}
