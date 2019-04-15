package provisioner

import (
	"context"

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

func (p *stdoutProvisioner) Supports(cluster *api.Cluster) bool {
	return true
}

// Provision mocks provisioning a cluster.
func (p *stdoutProvisioner) Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, channelConfig *channel.Config) error {
	logger.Infof("stdout: Provisioning cluster %s.", cluster.ID)

	return nil
}

// Decommission mocks decommissioning a cluster.
func (p *stdoutProvisioner) Decommission(logger *log.Entry, cluster *api.Cluster) error {
	logger.Infof("stdout: Decommissioning cluster %s.", cluster.ID)

	return nil
}
