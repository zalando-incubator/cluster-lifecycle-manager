package provisioner

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"golang.org/x/oauth2"

	log "github.com/sirupsen/logrus"
)

type (
	// A provider ID is a string that identifies a cluster provider.
	ProviderID string

	// Provisioner is an interface describing how to provision or decommission
	// clusters.
	Provisioner interface {
		// Supports returns true if the provisioner supports the given cluster.
		Supports(cluster *api.Cluster) bool

		// Provision provisions a cluster, based on the given cluster info and
		// channel configuration.
		Provision(
			ctx context.Context,
			logger *log.Entry,
			cluster *api.Cluster,
			channelConfig channel.Config,
		) error

		// Decommission decommissions a cluster, based on the given cluster
		// info.
		Decommission(
			ctx context.Context,
			logger *log.Entry,
			cluster *api.Cluster,
		) error
	}
	
	// Options is the options that can be passed to a provisioner when
	// initialized.
	Options struct {
		DryRun          bool
		ApplyOnly       bool
		UpdateStrategy  config.UpdateStrategy
		RemoveVolumes   bool
		ManageEtcdStack bool
	}

	// ZalandoAWSProvisioner is a provisioner for Zalando managed AWS clusters.
	ZalandoAWSProvisioner struct {
		clusterpyProvisioner
	}

	// ZalandoEKSProvisioner is a provisioner for AWS EKS clusters.
	ZalandoEKSProvisioner struct {
		clusterpyProvisioner
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

func NewZalandoEKSProvisioner(
	execManager *command.ExecManager,
	tokenSource oauth2.TokenSource,
	secretDecrypter decrypter.Decrypter,
	assumedRole string,
	awsConfig *aws.Config,
	options *Options,
) Provisioner {
	return &ZalandoEKSProvisioner{
		clusterpyProvisioner: *newClusterpyProvisioner(
			execManager,
			tokenSource,
			secretDecrypter,
			assumedRole,
			awsConfig,
			options,
		),
	}
}

func (z *ZalandoEKSProvisioner) Supports(cluster *api.Cluster) bool {
	return cluster.Provider == string(ZalandoEKSProvider)
}

// func (z *ZalandoEKSProvisioner) Provision(
// 	ctx context.Context,
// 	logger *log.Entry,
// 	cluster *api.Cluster,
// 	channelConfig channel.Config,
// ) error {
// 	if !z.Supports(cluster) {
// 		return ErrProviderNotSupported
// 	}
// 	return e.provision(ctx, logger, cluster, channelConfig)
// }

func newClusterpyProvisioner(
	execManager *command.ExecManager,
	tokenSource oauth2.TokenSource,
	secretDecrypter decrypter.Decrypter,
	assumedRole string,
	awsConfig *aws.Config,
	options *Options,
) *clusterpyProvisioner{

	provisioner := &clusterpyProvisioner{
		awsConfig:       awsConfig,
		execManager:     execManager,
		secretDecrypter: secretDecrypter,
		assumedRole:     assumedRole,
		tokenSource:     tokenSource,
	}

	if options != nil {
		provisioner.dryRun = options.DryRun
		provisioner.applyOnly = options.ApplyOnly
		provisioner.updateStrategy = options.UpdateStrategy
		provisioner.removeVolumes = options.RemoveVolumes
		provisioner.manageEtcdStack = options.ManageEtcdStack
	}

	return provisioner
}