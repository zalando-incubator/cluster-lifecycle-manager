package provisioner

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"golang.org/x/oauth2"
)

type ZalandoAWSProvisioner struct {
	clusterpyProvisioner
}

// NewZalandoAWSProvisioner returns a new provisioner by passing its location
// and and IAM role to use.
func NewZalandoAWSProvisioner(
	execManager *command.ExecManager,
	tokenSource oauth2.TokenSource,
	secretDecrypter decrypter.Decrypter,
	assumedRole string,
	awsConfigOptions []func(*config.LoadOptions) error,
	options *Options,
) Provisioner {
	provisioner := &ZalandoAWSProvisioner{
		clusterpyProvisioner: clusterpyProvisioner{
			awsConfigOptions:  awsConfigOptions,
			assumedRole:       assumedRole,
			execManager:       execManager,
			secretDecrypter:   secretDecrypter,
			tokenSource:       tokenSource,
			manageMasterNodes: true,
		},
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

func (z *ZalandoAWSProvisioner) Supports(cluster *api.Cluster) bool {
	return cluster.Provider == api.ZalandoAWSProvider
}

func (z *ZalandoAWSProvisioner) Provision(
	ctx context.Context,
	logger *log.Entry,
	cluster *api.Cluster,
	channelConfig channel.Config,
) error {
	if !z.Supports(cluster) {
		return ErrProviderNotSupported
	}

	awsAdapter, err := z.setupAWSAdapter(ctx, logger, cluster)
	if err != nil {
		return fmt.Errorf("failed to setup AWS Adapter: %v", err)
	}

	logger.Infof(
		"clusterpy: Prepare for provisioning cluster %s (%s)..",
		cluster.ID,
		cluster.LifecycleStatus,
	)

	return z.provision(
		ctx,
		logger,
		awsAdapter,
		z.tokenSource,
		cluster,
		channelConfig,
	)
}

// Decommission decommissions a cluster provisioned in AWS.
func (z *ZalandoAWSProvisioner) Decommission(
	ctx context.Context,
	logger *log.Entry,
	cluster *api.Cluster,
) error {
	if !z.Supports(cluster) {
		return ErrProviderNotSupported
	}

	awsAdapter, err := z.setupAWSAdapter(ctx, logger, cluster)
	if err != nil {
		return fmt.Errorf("failed to setup AWS Adapter: %v", err)
	}

	return z.decommission(ctx, logger, awsAdapter, z.tokenSource, cluster, nil)
}
