package provisioner

import (
	"context"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
)

// Options is the options that can be passed to a provisioner when initialized.
type Options struct {
	DryRun         bool
	ApplyOnly      bool
	UpdateStrategy config.UpdateStrategy
	RemoveVolumes  bool
}

// Provisioner is an interface describing how to provision or decommission
// clusters.
type Provisioner interface {
	Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, channelConfig *channel.Config) error
	Decommission(logger *log.Entry, cluster *api.Cluster) error
}
