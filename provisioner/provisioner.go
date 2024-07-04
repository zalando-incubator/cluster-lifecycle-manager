package provisioner

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"encoding/base64"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	awsUtils "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	"github.com/zalando-incubator/kube-ingress-aws-controller/certs"
	"golang.org/x/oauth2"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws/eks"

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

func (p *clusterpyProvisioner) provisionClusterStack(
	ctx context.Context,
	logger *log.Entry,
	cluster *api.Cluster,
	channelConfig channel.Config,
) (
	*awsAdapter,
	string,
	map[string]interface{},
	*awsUtils.InstanceTypes,
	map[string]string,
	*AZInfo,
	*deletions,
	error,
) {
	instanceTypes, awsAdapter, err := p.prepareProvision(logger, cluster, channelConfig)
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	// get VPC information
	var vpc *ec2.Vpc
	vpcID, ok := cluster.ConfigItems[vpcIDConfigItemKey]
	if !ok { // if vpcID is not defined, autodiscover it
		vpc, err = awsAdapter.GetDefaultVPC()
		if err != nil {
			return nil, "", nil, nil, nil, nil, nil, err
		}
		vpcID = aws.StringValue(vpc.VpcId)
		cluster.ConfigItems[vpcIDConfigItemKey] = vpcID
	} else {
		vpc, err = awsAdapter.GetVPC(vpcID)
		if err != nil {
			return nil, "", nil, nil, nil, nil, nil, err
		}
	}

	if err = ctx.Err(); err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	subnets, err := awsAdapter.GetSubnets(vpcID)
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	if err = ctx.Err(); err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	subnets = filterSubnets(subnets, subnetNot(isCustomSubnet))

	// if subnets are defined in the config items, filter the subnet list
	if subnetIDs, ok := cluster.ConfigItems[subnetsConfigItemKey]; ok {
		ids := strings.Split(subnetIDs, ",")
		subnets = filterSubnets(subnets, subnetIDIncluded(ids))
		if len(subnets) != len(ids) {
			return nil, "", nil, nil, nil, nil, nil, fmt.Errorf("invalid or unknown subnets; desired %v", ids)
		}
	}

	// find the best subnet for each AZ
	azInfo := selectSubnetIDs(subnets)

	// if availability zones are defined, filter the subnet list
	if azNames, ok := cluster.ConfigItems[availabilityZonesConfigItemKey]; ok {
		azInfo = azInfo.RestrictAZs(strings.Split(azNames, ","))
	}

	// TODO legacy, remove once we switch to Values in all clusters
	if _, ok := cluster.ConfigItems[subnetsConfigItemKey]; !ok {
		cluster.ConfigItems[subnetsConfigItemKey] = azInfo.SubnetsByAZ()[subnetAllAZName]
	}

	apiURL, err := url.Parse(cluster.APIServerURL)
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	// TODO: should this be done like this or via a config item?
	hostedZone, err := getHostedZone(cluster.APIServerURL)
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	certificates, err := awsAdapter.GetCertificates()
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	loadBalancerCert, err := certs.FindBestMatchingCertificate(certificates, apiURL.Host)
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	etcdKMSKeyARN, err := awsAdapter.resolveKeyID(etcdKMSKeyAlias)
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	values := map[string]interface{}{
		subnetsValueKey:             azInfo.SubnetsByAZ(),
		availabilityZonesValueKey:   azInfo.AvailabilityZones(),
		"hosted_zone":               hostedZone,
		"load_balancer_certificate": loadBalancerCert.ID(),
		"vpc_ipv4_cidr":             aws.StringValue(vpc.CidrBlock),
		"etcd_kms_key_arn":          etcdKMSKeyARN,
	}

	// render the manifests to find out if they're valid
	deletions, err := parseDeletions(channelConfig, cluster, values, awsAdapter, instanceTypes)
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	// create S3 bucket with AWS account ID to ensure uniqueness across
	// accounts
	bucketName := fmt.Sprintf(clmCFBucketPattern, strings.TrimPrefix(cluster.InfrastructureAccount, "aws:"), cluster.Region)
	err = awsAdapter.createS3Bucket(bucketName)
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	// create or update the etcd stack
	if p.manageEtcdStack {
		err = createOrUpdateEtcdStack(ctx, logger, channelConfig, cluster, values, etcdKMSKeyARN, awsAdapter, bucketName, instanceTypes)
		if err != nil {
			return nil, "", nil, nil, nil, nil, nil, err
		}
	}

	if err = ctx.Err(); err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}

	outputs, err := createOrUpdateClusterStack(ctx, channelConfig, cluster, values, awsAdapter, bucketName, instanceTypes)
	if err != nil {
		return nil, "", nil, nil, nil, nil, nil, err
	}
	values[clusterStackOutputKey] = outputs

	return awsAdapter, bucketName, values, instanceTypes, outputs, azInfo, deletions, nil
}

func (p *clusterpyProvisioner) provisionNodePools(
	ctx context.Context,
	logger *log.Entry,
	awsAdapter *awsAdapter,
	cluster *api.Cluster,
	channelConfig channel.Config,
	bucketName string,
	instanceTypes *awsUtils.InstanceTypes,
	manifests []manifestPackage,
	deletions *deletions,
	values map[string]interface{},
	azInfo *AZInfo,
) error {
	// provision node pools
	caNodePoolProvisioner := &AWSNodePoolProvisioner{
		NodePoolTemplateRenderer: NodePoolTemplateRenderer{
			awsAdapter:     awsAdapter,
			config:         channelConfig,
			cluster:        cluster,
			bucketName:     bucketName,
			logger:         logger,
			encodeUserData: true,
			instanceTypes:  instanceTypes,
		},
		azInfo: azInfo,
	}

	// TODO: improve eks support
	eksTokenSource := eks.NewTokenSource(awsAdapter.session, eksID(cluster.ID))
	decodedCA, err := base64.StdEncoding.DecodeString(cluster.ConfigItems["eks_certficate_authority_data"])
	if err != nil {
		return err
	}

	karpenterProvisioner, err := NewKarpenterNodePoolProvisioner(
		NodePoolTemplateRenderer{
			awsAdapter:     awsAdapter,
			config:         channelConfig,
			cluster:        cluster,
			bucketName:     bucketName,
			logger:         logger,
			encodeUserData: false,
			instanceTypes:  instanceTypes,
		}, p.execManager, eksTokenSource, decodedCA,
	)
	if err != nil {
		return err
	}

	// group node pools based on their profile e.g. master
	nodePoolGroups := groupNodePools(
		logger,
		cluster,
		caNodePoolProvisioner,
		karpenterProvisioner,
		p.tokenSource,
	)

	updater, err := p.updater(logger, awsAdapter, cluster)
	if err != nil {
		return err
	}

	// err = nodePoolGroups["masters"].provisionNodePoolGroup(ctx, values, updater, cluster, p.applyOnly)
	// if err != nil {
	// 	return err
	// }

	// TODO: EKS?
	err = nodePoolGroups["workers"].provisionNodePoolGroup(ctx, values, updater, cluster, p.applyOnly)
	if err != nil {
		return err
	}

	if karpenterProvisioner.isKarpenterEnabled() {
		err = p.apply(ctx, logger, decodedCA, eksTokenSource, cluster, deletions, manifests)
		if err != nil {
			return err
		}
	}

	if karpenterProvisioner.isKarpenterEnabled() {
		if err = nodePoolGroups["karpenterPools"].provisionNodePoolGroup(ctx, values, updater, cluster, p.applyOnly); err != nil {
			return err
		}
	}

	// clean up removed node pools
	err = caNodePoolProvisioner.Reconcile(ctx, updater)
	if err != nil {
		return err
	}
	if karpenterProvisioner.isKarpenterEnabled() {
		err = karpenterProvisioner.Reconcile(ctx, updater)
		if err != nil {
			return err
		}
	}

	if !karpenterProvisioner.isKarpenterEnabled() {
		err = p.apply(ctx, logger, decodedCA, eksTokenSource, cluster, deletions, manifests)
		if err != nil {
			return err
		}
	}
	return nil
}