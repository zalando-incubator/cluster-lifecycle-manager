package provisioner

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws/eks"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
)

type (
	ZalandoEKSProvisioner struct {
		clusterpyProvisioner
	}

	ZalandoEKSModifier struct{}
)

// NewZalandoEKSProvisioner returns a new provisioner capable of provisioning
// EKS clusters by passing its location and and IAM role to use.
func NewZalandoEKSProvisioner(
	execManager *command.ExecManager,
	secretDecrypter decrypter.Decrypter,
	assumedRole string,
	awsConfig *aws.Config,
	options *Options,
) Provisioner {
	provisioner := &ZalandoEKSProvisioner{
		clusterpyProvisioner: clusterpyProvisioner{
			awsConfig:         awsConfig,
			assumedRole:       assumedRole,
			execManager:       execManager,
			secretDecrypter:   secretDecrypter,
			manageMasterNodes: false,
			manageEtcdStack:   false,
		},
	}

	if options != nil {
		provisioner.dryRun = options.DryRun
		provisioner.applyOnly = options.ApplyOnly
		provisioner.updateStrategy = options.UpdateStrategy
		provisioner.removeVolumes = options.RemoveVolumes
		provisioner.modifier = options.Modifier
	}

	return provisioner
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

	awsAdapter, err := z.setupAWSAdapter(logger, cluster)
	if err != nil {
		return fmt.Errorf("failed to setup AWS Adapter: %v", err)
	}

	eksTokenSource := eks.NewTokenSource(awsAdapter.session, eksID(cluster.ID))

	logger.Infof(
		"clusterpy: Prepare for provisioning EKS cluster %s (%s)..",
		cluster.ID,
		cluster.LifecycleStatus,
	)

	return z.provision(
		ctx,
		logger,
		awsAdapter,
		eksTokenSource,
		cluster,
		channelConfig,
	)
}

func (z *ZalandoEKSProvisioner) Decommission(
	ctx context.Context,
	logger *log.Entry,
	cluster *api.Cluster,
) error {
	if !z.Supports(cluster) {
		return ErrProviderNotSupported
	}

	logger.Infof(
		"Decommissioning EKS cluster: %s (%s)",
		cluster.Alias,
		cluster.ID,
	)

	awsAdapter, err := z.setupAWSAdapter(logger, cluster)
	if err != nil {
		return err
	}
	clusterInfo, err := awsAdapter.GetEKSClusterCA(cluster)
	if err != nil {
		return err
	}
	caData, err := base64.StdEncoding.DecodeString(
		clusterInfo.CertificateAuthority,
	)
	if err != nil {
		return err
	}

	cluster.APIServerURL = clusterInfo.Endpoint
	tokenSource := eks.NewTokenSource(awsAdapter.session, eksID(cluster.ID))

	return z.decommission(
		ctx,
		logger,
		awsAdapter,
		tokenSource,
		cluster,
		caData,
	)
}

func (z *ZalandoEKSModifier) GetPostOptions(
	adapter *awsAdapter,
	cluster *api.Cluster,
	cloudFormationOutput map[string]string,
) (*PostOptions, error) {
	res := &PostOptions{}

	clusterInfo, err := adapter.GetEKSClusterCA(cluster)
	if err != nil {
		return nil, err
	}
	decodedCA, err := base64.StdEncoding.DecodeString(
		clusterInfo.CertificateAuthority,
	)
	if err != nil {
		return nil, err
	}

	res.APIServerURL = clusterInfo.Endpoint
	res.CAData = decodedCA
	res.ConfigItems = map[string]string{
		"eks_endpoint":                  clusterInfo.Endpoint,
		"eks_certficate_authority_data": clusterInfo.CertificateAuthority,
	}

	subnets := map[string]string{}
	for key, az := range map[string]string{
		"EKSSubneta": "eu-central-1a",
		"EKSSubnetb": "eu-central-1b",
		"EKSSubnetc": "eu-central-1c",
	} {
		if v, ok := cloudFormationOutput[key]; ok {
			subnets[az] = v
		}
	}
	if len(subnets) > 0 {
		res.AZInfo = &AZInfo{
			subnets: subnets,
		}
		res.TemplateValues = map[string]interface{}{
			subnetsValueKey: subnets,
		}
	}

	return res, nil
}
