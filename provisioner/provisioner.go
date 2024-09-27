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
	// A provider ID is a string that identifies a cluster provider.
	ProviderID string

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

	// HookResponse contain configuration parameters that a provisioner can use
	// at a later stage.
	HookResponse struct {
		APIServerURL   string
		AZInfo         *AZInfo
		CAData         []byte
		TemplateValues map[string]interface{}
	}

	// Options is the options that can be passed to a provisioner when initialized.
	Options struct {
		DryRun          bool
		ApplyOnly       bool
		UpdateStrategy  config.UpdateStrategy
		RemoveVolumes   bool
		ManageEtcdStack bool
	}
)

const (
	// ZalandoAWSProvider is the provider ID for Zalando managed AWS clusters.
	ZalandoAWSProvider ProviderID = "zalando-aws"
	// ZalandoEKSProvider is the provider ID for AWS EKS clusters.
	ZalandoEKSProvider ProviderID = "zalando-eks"
)

var (
	// ErrProviderNotSupported is the error returned from porvisioners if
	// they don't support the cluster provider defined.
	ErrProviderNotSupported = errors.New("unsupported provider type")
)
