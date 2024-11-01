package provisioner

import (
	"context"
	"errors"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
)

type (
	// Provisioner is an interface describing how to provision or decommission
	// clusters.
	Provisioner interface {
		Supports(cluster *api.Cluster) bool

		Provision(
			ctx context.Context,
			logger *log.Entry,
			cluster *api.Cluster,
			channelConfig channel.Config,
		) error

		Decommission(
			ctx context.Context,
			logger *log.Entry,
			cluster *api.Cluster,
		) error
	}

	// CreationHook is an interface that provisioners can use while provisioning
	// a cluster.
	//
	// This is useful for example to pass additional configuration only known at
	// a later stage of provisioning. For example, when provisioning an EKS
	// cluster, the provisioner only knows what is the API Server URL after
	// applying the initial CloudFormation.
	CreationHook interface {
		// Execute performs updates used by a provisioner during cluster
		// creation.
		Execute(
			adapter awsInterface,
			cluster *api.Cluster,
		) (
			*HookResponse,
			error,
		)
	}

	// HookResponse contain configuration parameters that a provisioner can use
	// at a later stage.
	HookResponse struct {
		APIServerURL string
		CAData       []byte
		ServiceCIDR  string
	}

	// Options is the options that can be passed to a provisioner when initialized.
	Options struct {
		DryRun          bool
		ApplyOnly       bool
		UpdateStrategy  config.UpdateStrategy
		RemoveVolumes   bool
		ManageEtcdStack bool
		Hook            CreationHook
	}
)

var (
	// ErrProviderNotSupported is the error returned from porvisioners if
	// they don't support the cluster provider defined.
	ErrProviderNotSupported = errors.New("unsupported provider type")
)
