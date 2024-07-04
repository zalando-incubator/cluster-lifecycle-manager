package provisioner

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"golang.org/x/oauth2"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
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

func (z *ZalandoEKSProvisioner) Provision(
	ctx context.Context,
	logger *log.Entry,
	cluster *api.Cluster,
	channelConfig channel.Config,
) error {
	if !z.Supports(cluster) {
		return ErrProviderNotSupported
	}

	awsAdapter, bucketName, values, instanceTypes, outputs, azInfo, deletions, err := z.provisionClusterStack(
		ctx,
		logger,
		cluster,
		channelConfig,
	)
	if err != nil {
		return err
	}

	// Custom EKS SUPPORT
	clusterInfo, err := awsAdapter.GetEKSClusterCA(cluster)
	if err != nil {
		return err
	}

	cluster.ConfigItems["eks_endpoint"] = clusterInfo.Endpoint
	cluster.APIServerURL = clusterInfo.Endpoint
	cluster.ConfigItems["eks_certficate_authority_data"] = clusterInfo.CertificateAuthority

	// TODO: having it this late means late feedback on invalid manifests
	manifests, err := renderManifests(channelConfig, cluster, values, awsAdapter, instanceTypes)
	if err != nil {
		return err
	}

	// EKS: get subnet IDs from cluster stack output
	newAzInfo := &AZInfo{
		subnets: map[string]string{},
	}
	for key, az := range map[string]string{"EKSSubneta": "eu-central-1a", "EKSSubnetb": "eu-central-1b", "EKSSubnetc": "eu-central-1c"} {
		if v, ok := outputs[key]; ok {
			newAzInfo.subnets[az] = v
		}
	}

	if len(newAzInfo.subnets) > 0 {
		azInfo = newAzInfo
		values[subnetsValueKey] = azInfo.SubnetsByAZ()
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	err = z.provisionNodePools(
		ctx,
		logger,
		awsAdapter,
		cluster,
		channelConfig,
		bucketName,
		instanceTypes,
		manifests,
		deletions,
		values,
		azInfo,
	)
	if err != nil {
		return err
	}

	return nil
}

// func (z *ZalandoEKSProvisioner) Decommission(
// 	ctx context.Context,
// 	logger *log.Entry,
// 	cluster *api.Cluster,
// ) error {
// 	if !z.Supports(cluster) {
// 		return ErrProviderNotSupported
// 	}

// 	logger.Infof("Decommissioning EKS cluster: %s (%s)", cluster.Alias, cluster.ID)

// }