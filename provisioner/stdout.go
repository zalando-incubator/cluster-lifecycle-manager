package provisioner

import (
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
)

type stdoutProvisioner struct{}

// NewStdoutProvisioner creates a new provisioner which prints to stdout
// instead of doing any actual provsioning.
func NewStdoutProvisioner() Provisioner {
	return &stdoutProvisioner{}
}

// Provision mocks provisioning a cluster.
func (p *stdoutProvisioner) Provision(cluster *api.Cluster, channelConfig *channel.Config) error {
	log.Infof("stdout: Provisioning cluster %s.", cluster.ID)

	return nil
}

// Decommission mocks decommissioning a cluster.
func (p *stdoutProvisioner) Decommission(cluster *api.Cluster, channelConfig *channel.Config) error {
	log.Infof("stdout: Decommissioning cluster %s.", cluster.ID)

	return nil
}

// Version mocks geting the version based on cluster resource and channel config.
func (p *stdoutProvisioner) Version(cluster *api.Cluster, channelVersion channel.ConfigVersion) (string, error) {
	return "", nil
}
