package provisioner

import (
	"context"
	"errors"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"

	log "github.com/sirupsen/logrus"
)

var (
	// ErrProviderNotSupported is the error returned from porvisioners if
	// they don't support the cluster provider defined.
	ErrProviderNotSupported = errors.New("unsupported provider type")
)

// Options is the options that can be passed to a provisioner when initialized.
type Options struct {
	DryRun          bool
	ApplyOnly       bool
	UpdateStrategy  config.UpdateStrategy
	RemoveVolumes   bool
	ManageEtcdStack bool
}

// Provisioner is an interface describing how to provision or decommission
// clusters.
type Provisioner interface {
	Supports(cluster *api.Cluster) bool
	Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, channelConfig channel.Config) error
	Decommission(ctx context.Context, logger *log.Entry, cluster *api.Cluster) error
}
