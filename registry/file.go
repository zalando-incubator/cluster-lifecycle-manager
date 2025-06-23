package registry

import (
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"gopkg.in/yaml.v2"
)

type fileRegistry struct {
	filePath string
}

// FileRegistryData wrapper around cluster items read from file
type FileRegistryData struct {
	Clusters []*api.Cluster `json:"clusters" yaml:"clusters"`
}

var fileClusters = &FileRegistryData{} //store file content locally

// NewFileRegistry returns file registry client
func NewFileRegistry(filePath string) Registry {
	return &fileRegistry{
		filePath: filePath,
	}
}

func (r *fileRegistry) ListClusters(_ Filter) ([]*api.Cluster, error) {
	fileContent, err := os.ReadFile(r.filePath)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(fileContent, &fileClusters)
	if err != nil {
		return nil, err
	}

	clustersByAccount := map[string][]*api.Cluster{}

	for _, cluster := range fileClusters.Clusters {
		for _, nodePool := range cluster.NodePools {
			if nodePool.Profile == "worker-karpenter" && len(nodePool.InstanceTypes) == 0 {
				nodePool.InstanceType = ""
				continue
			}
			if len(nodePool.InstanceTypes) == 0 {
				return nil, fmt.Errorf("no instance types for cluster %s, pool %s", cluster.ID, nodePool.Name)
			}
			nodePool.InstanceType = nodePool.InstanceTypes[0]
		}
		clustersByAccount[cluster.InfrastructureAccount] = append(clustersByAccount[cluster.InfrastructureAccount], cluster)
	}
	for _, cluster := range fileClusters.Clusters {
		cluster.AccountClusters = clustersByAccount[cluster.InfrastructureAccount]
		if err := cluster.InitOIDCProvider(); err != nil {
			return nil, err
		}
	}

	return fileClusters.Clusters, nil
}

func (r *fileRegistry) UpdateLifecycleStatus(cluster *api.Cluster) error {
	if cluster == nil {
		return fmt.Errorf("failed to update the cluster. Empty cluster is passed")
	}
	for _, c := range fileClusters.Clusters {
		if c.ID == cluster.ID {
			log.Debugf("[Cluster %s updated] Lifecycle status: %s", cluster.ID, cluster.LifecycleStatus)
			log.Debugf("[Cluster %s updated] Current status: %#v", cluster.ID, *cluster.Status)
			return nil
		}
	}
	return fmt.Errorf("failed to update the cluster: cluster %s not found", cluster.ID)
}

func (r *fileRegistry) UpdateConfigItems(
	cluster *api.Cluster,
	configItems map[string]string,
) error {
	if cluster == nil {
		return fmt.Errorf(
			"failed to update the cluster. Empty cluster is passed",
		)
	}
	for _, c := range fileClusters.Clusters {
		if c.ID == cluster.ID {
			log.Debugf(
				"[Cluster %s updated] Config Items: %v",
				cluster.ID,
				configItems,
			)
			for key, value := range configItems {
				cluster.ConfigItems[key] = value
			}
			return nil
		}
	}
	return fmt.Errorf(
		"failed to update the cluster: cluster %s not found",
		cluster.ID,
	)
}
