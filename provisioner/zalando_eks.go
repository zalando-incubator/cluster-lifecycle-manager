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
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

const (
	KeyEKSEndpoint      = "eks_endpoint"
	KeyEKSCAData        = "eks_certificate_authority_data"
	KeyEKSOIDCIssuerURL = "eks_oidc_issuer_url"
)

type (
	ZalandoEKSProvisioner struct {
		clusterpyProvisioner
	}

	// ZalandoEKSCreationHook is a hook specific for EKS cluster provisioning.
	ZalandoEKSCreationHook struct {
		clusterRegistry registry.Registry
	}
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
		provisioner.hook = options.Hook
	}

	return provisioner
}

func (z *ZalandoEKSProvisioner) Supports(cluster *api.Cluster) bool {
	return cluster.Provider == api.ZalandoEKSProvider
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

	clusterDetails, err := awsAdapter.GetEKSClusterDetails(cluster)
	if err != nil {
		if isClusterNotFoundErr(err) {
			logger.Infof("EKS cluster not found: %s", cluster.ID)
			return nil
		}
		return err
	}
	cluster.APIServerURL = clusterDetails.Endpoint

	caData, err := base64.StdEncoding.DecodeString(
		clusterDetails.CertificateAuthority,
	)
	if err != nil {
		return err
	}

	tokenSource := eks.NewTokenSource(awsAdapter.session, eksID(cluster.ID))

	return z.decommission(
		ctx,
		logger,
		awsAdapter,
		tokenSource,
		cluster,
		caData,
		awsAdapter.DeleteLeakedAWSVPCCNIENIs,
	)
}

// NewZalandoEKSCreationHook returns a new hook for EKS cluster provisioning,
// configured to use the given cluster registry.
func NewZalandoEKSCreationHook(
	clusterRegistry registry.Registry,
) CreationHook {
	return &ZalandoEKSCreationHook{
		clusterRegistry: clusterRegistry,
	}
}

// Execute updates the configuration only known after deploying the first
// CloudFormation stack.
//
// The method returns the API server URL, the Certificate Authority data,
// and the subnets. Additionally Execute updates the configured cluster
// registry with the EKS API Server URL and the Certificate Authority data.
func (z *ZalandoEKSCreationHook) Execute(
	adapter awsInterface,
	cluster *api.Cluster,
) (*HookResponse, error) {
	res := &HookResponse{}

	clusterDetails, err := adapter.GetEKSClusterDetails(cluster)
	if err != nil {
		return nil, err
	}
	decodedCA, err := base64.StdEncoding.DecodeString(
		clusterDetails.CertificateAuthority,
	)
	if err != nil {
		return nil, err
	}

	if cluster.ConfigItems == nil {
		cluster.ConfigItems = map[string]string{}
	}

	toUpdate := map[string]string{}
	if cluster.ConfigItems[KeyEKSEndpoint] != clusterDetails.Endpoint {
		toUpdate[KeyEKSEndpoint] = clusterDetails.Endpoint
	}
	if cluster.ConfigItems[KeyEKSCAData] != clusterDetails.CertificateAuthority {
		toUpdate[KeyEKSCAData] = clusterDetails.CertificateAuthority
	}
	if cluster.ConfigItems[KeyEKSOIDCIssuerURL] != clusterDetails.OIDCIssuerURL {
		toUpdate[KeyEKSOIDCIssuerURL] = clusterDetails.OIDCIssuerURL
	}

	err = z.clusterRegistry.UpdateConfigItems(cluster, toUpdate)
	if err != nil {
		return nil, err
	}

	res.APIServerURL = clusterDetails.Endpoint
	res.CAData = decodedCA
	res.ServiceCIDR = clusterDetails.ServiceCIDR

	return res, nil
}
