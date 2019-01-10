package registry

import (
	"fmt"
	"io/ioutil"

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

func (r *fileRegistry) ListClusters(filter Filter) ([]*api.Cluster, error) {
	fileContent, err := ioutil.ReadFile(r.filePath)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(fileContent, &fileClusters)
	if err != nil {
		return nil, err
	}

	for _, cluster := range fileClusters.Clusters {
		for _, nodePool := range cluster.NodePools {
			if len(nodePool.InstanceTypes) == 0 {
				return nil, fmt.Errorf("no instance types for cluster %s, pool %s", cluster.ID, nodePool.Name)
			}
			nodePool.InstanceType = nodePool.InstanceTypes[0]
		}
	}

	return fileClusters.Clusters, nil
}

func (r *fileRegistry) UpdateCluster(cluster *api.Cluster) error {
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
