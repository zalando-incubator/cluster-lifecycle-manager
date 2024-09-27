package provisioner

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/cenkalti/backoff"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	awsUtils "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/updatestrategy"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
	"github.com/zalando-incubator/kube-ingress-aws-controller/certs"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	etcdStackFileName                  = "stack.yaml"
	clusterStackFileName               = "cluster.yaml"
	etcdStackNameDefault               = "etcd-cluster-etcd"
	etcdStackNameConfigItemKey         = "etcd_stack_name"
	defaultNamespace                   = "default"
	tagNameKubernetesClusterPrefix     = "kubernetes.io/cluster/"
	subnetELBRoleTagName               = "kubernetes.io/role/elb"
	resourceLifecycleShared            = "shared"
	resourceLifecycleOwned             = "owned"
	mainStackTagKey                    = "cluster-lifecycle-controller.zalando.org/main-stack"
	stackTagValueTrue                  = "true"
	subnetsConfigItemKey               = "subnets"
	subnetsValueKey                    = "subnets"
	availabilityZonesConfigItemKey     = "availability_zones"
	availabilityZonesValueKey          = "availability_zones"
	vpcIDConfigItemKey                 = "vpc_id"
	subnetAllAZName                    = "*"
	maxApplyRetries                    = 10
	configKeyUpdateStrategy            = "update_strategy"
	updateStrategyRolling              = "rolling"
	updateStrategyCLC                  = "clc"
	defaultMaxRetryTime                = 5 * time.Minute
	clcPollingInterval                 = 10 * time.Second
	clusterStackOutputKey              = "ClusterStackOutputs"
	decommissionNodeNoScheduleTaintKey = "decommission_node_no_schedule_taint"
	customSubnetTag                    = "zalando.org/custom-subnet"
	etcdKMSKeyAlias                    = "alias/etcd-cluster"
	karpenterNodePoolProfile           = "worker-karpenter"
)

type clusterpyProvisioner struct {
	awsConfig         *aws.Config
	execManager       *command.ExecManager
	secretDecrypter   decrypter.Decrypter
	assumedRole       string
	dryRun            bool
	tokenSource       oauth2.TokenSource
	applyOnly         bool
	updateStrategy    config.UpdateStrategy
	clusterRegistry   registry.Registry
	removeVolumes     bool
	manageEtcdStack   bool
	manageMasterNodes bool
}

type manifestPackage struct {
	name      string
	manifests []string
}

func (p *clusterpyProvisioner) updateDefaults(cluster *api.Cluster, channelConfig channel.Config, adapter *awsAdapter, instanceTypes *awsUtils.InstanceTypes) error {
	defaultsFiles, err := channelConfig.DefaultsManifests()
	if err != nil {
		return err
	}

	withoutConfigItems := *cluster
	withoutConfigItems.ConfigItems = make(map[string]string)

	allDefaults := make(map[string]string)

	for _, file := range defaultsFiles {
		result, err := renderSingleTemplate(file, &withoutConfigItems, nil, nil, adapter, instanceTypes)
		if err != nil {
			return err
		}

		var defaults map[string]string
		err = yaml.Unmarshal([]byte(result), &defaults)
		if err != nil {
			return err
		}

		for k, v := range defaults {
			allDefaults[k] = v
		}
	}

	for k, v := range allDefaults {
		if _, ok := cluster.ConfigItems[k]; !ok {
			cluster.ConfigItems[k] = v
		}
	}

	return nil
}

// decryptConfigItems tries to decrypt encrypted config items in the cluster
// config and modifies the passed cluster config so encrypted items has been
// decrypted.
func (p *clusterpyProvisioner) decryptConfigItems(cluster *api.Cluster) error {
	for key, item := range cluster.ConfigItems {
		plaintext, err := p.secretDecrypter.Decrypt(item)
		if err != nil {
			return fmt.Errorf("failed to decrypt config-item: %s: %w", key, err)
		}
		cluster.ConfigItems[key] = plaintext
	}
	return nil
}

// propagateConfigItemsToNodePools propagates cluster-wide config items
// to each node pool unless the node pool defines its own value.
func (p *clusterpyProvisioner) propagateConfigItemsToNodePools(cluster *api.Cluster) {
	for _, nodePool := range cluster.NodePools {
		// If the node pool doesn't define any config items, we need to initialize it here.
		if nodePool.ConfigItems == nil {
			nodePool.ConfigItems = map[string]string{}
		}
		for name, value := range cluster.ConfigItems {
			if _, ok := nodePool.ConfigItems[name]; !ok {
				nodePool.ConfigItems[name] = value
			}
		}
	}
}

func (p *clusterpyProvisioner) provision(
	ctx context.Context,
	logger *log.Entry,
	awsAdapter *awsAdapter,
	tokenSource oauth2.TokenSource,
	cluster *api.Cluster,
	channelConfig channel.Config,
) error {
	// fetch instance data that will be used by all the render functions
	instanceTypes, err := awsUtils.NewInstanceTypesFromAWS(awsAdapter.ec2Client)
	if err != nil {
		return fmt.Errorf("failed to fetch instance types from AWS")
	}

	err = p.applyDefaultsToManifests(
		awsAdapter,
		cluster,
		channelConfig,
		instanceTypes,
	)
	if err != nil {
		return err
	}

	// get VPC information
	var vpc *ec2.Vpc
	vpcID, ok := cluster.ConfigItems[vpcIDConfigItemKey]
	if !ok { // if vpcID is not defined, autodiscover it
		vpc, err = awsAdapter.GetDefaultVPC()
		if err != nil {
			return err
		}
		vpcID = aws.StringValue(vpc.VpcId)
		cluster.ConfigItems[vpcIDConfigItemKey] = vpcID
	} else {
		vpc, err = awsAdapter.GetVPC(vpcID)
		if err != nil {
			return err
		}
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	subnets, err := awsAdapter.GetSubnets(vpcID)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	subnets = filterSubnets(subnets, subnetNot(isCustomSubnet))

	// if subnets are defined in the config items, filter the subnet list
	if subnetIDs, ok := cluster.ConfigItems[subnetsConfigItemKey]; ok {
		ids := strings.Split(subnetIDs, ",")
		subnets = filterSubnets(subnets, subnetIDIncluded(ids))
		if len(subnets) != len(ids) {
			return fmt.Errorf("invalid or unknown subnets; desired %v", ids)
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
		return err
	}

	// TODO: should this be done like this or via a config item?
	hostedZone, err := getHostedZone(cluster.APIServerURL)
	if err != nil {
		return err
	}

	certificates, err := awsAdapter.GetCertificates()
	if err != nil {
		return err
	}

	loadBalancerCert, err := certs.FindBestMatchingCertificate(certificates, apiURL.Host)
	if err != nil {
		return err
	}

	values := map[string]interface{}{
		subnetsValueKey:             azInfo.SubnetsByAZ(),
		availabilityZonesValueKey:   azInfo.AvailabilityZones(),
		"hosted_zone":               hostedZone,
		"load_balancer_certificate": loadBalancerCert.ID(),
		"vpc_ipv4_cidr":             aws.StringValue(vpc.CidrBlock),
	}

	// render the manifests to find out if they're valid
	deletions, err := parseDeletions(channelConfig, cluster, values, awsAdapter, instanceTypes)
	if err != nil {
		return err
	}

	// create S3 bucket with AWS account ID to ensure uniqueness across
	// accounts
	bucketName := fmt.Sprintf(clmCFBucketPattern, strings.TrimPrefix(cluster.InfrastructureAccount, "aws:"), cluster.Region)
	err = awsAdapter.createS3Bucket(bucketName)
	if err != nil {
		return err
	}

	// create or update the etcd stack
	if p.manageEtcdStack && cluster.Provider == string(ZalandoAWSProvider) {
		etcdKMSKeyARN, err := awsAdapter.resolveKeyID(etcdKMSKeyAlias)
		if err != nil {
			return err
		}
		values["etcd_kms_key_arn"] = etcdKMSKeyARN

		err = createOrUpdateEtcdStack(ctx, logger, channelConfig, cluster, values, etcdKMSKeyARN, awsAdapter, bucketName, instanceTypes)
		if err != nil {
			return err
		}
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	outputs, err := createOrUpdateClusterStack(ctx, channelConfig, cluster, values, awsAdapter, bucketName, instanceTypes)
	if err != nil {
		return err
	}
	values[clusterStackOutputKey] = outputs

	if err = ctx.Err(); err != nil {
		return err
	}

	postOptions := &HookResponse{}

	if cluster.Provider == string(ZalandoEKSProvider) {
		clusterDetails, err := awsAdapter.GetEKSClusterDetails(cluster)
		if err != nil {
			return err
		}
		decodedCA, err := base64.StdEncoding.DecodeString(
			clusterDetails.CertificateAuthority,
		)
		if err != nil {
			return err
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

		err = p.clusterRegistry.UpdateConfigItems(cluster, toUpdate)
		if err != nil {
			return err
		}

		postOptions.APIServerURL = clusterDetails.Endpoint
		postOptions.CAData = decodedCA

		subnets := map[string]string{}
		for key, az := range map[string]string{
			"EKSSubneta": "eu-central-1a",
			"EKSSubnetb": "eu-central-1b",
			"EKSSubnetc": "eu-central-1c",
		} {
			if v, ok := outputs[key]; ok {
				subnets[az] = v
			}
		}
		if len(subnets) > 0 {
			postOptions.AZInfo = &AZInfo{
				subnets: subnets,
			}
			postOptions.TemplateValues = map[string]interface{}{
				subnetsValueKey: subnets,
			}
		}

		if postOptions.APIServerURL != "" {
			cluster.APIServerURL = postOptions.APIServerURL
		}
		if postOptions.AZInfo != nil {
			azInfo = postOptions.AZInfo
		}
		for k, v := range postOptions.TemplateValues {
			values[k] = v
		}
	}

	// TODO: having it this late means late feedback on invalid manifests
	manifests, err := renderManifests(
		channelConfig,
		cluster,
		values,
		awsAdapter,
		instanceTypes,
	)
	if err != nil {
		return err
	}

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

	karpenterProvisioner, err := NewKarpenterNodePoolProvisioner(
		NodePoolTemplateRenderer{
			awsAdapter:     awsAdapter,
			config:         channelConfig,
			cluster:        cluster,
			bucketName:     bucketName,
			logger:         logger,
			encodeUserData: false,
			instanceTypes:  instanceTypes,
		},
		p.execManager,
		tokenSource,
		postOptions,
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
		tokenSource,
	)

	updater, err := p.updater(
		logger,
		awsAdapter,
		tokenSource,
		cluster,
		postOptions,
	)
	if err != nil {
		return err
	}

	if p.manageMasterNodes {
		err = nodePoolGroups["masters"].provisionNodePoolGroup(
			ctx,
			values,
			updater,
			cluster,
			p.applyOnly,
		)
		if err != nil {
			return err
		}
	}

	err = nodePoolGroups["workers"].provisionNodePoolGroup(ctx, values, updater, cluster, p.applyOnly)
	if err != nil {
		return err
	}

	if karpenterProvisioner.isKarpenterEnabled() {
		err = p.apply(
			ctx,
			logger,
			tokenSource,
			cluster,
			deletions,
			manifests,
			postOptions,
		)
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
		err = p.apply(
			ctx,
			logger,
			tokenSource,
			cluster,
			deletions,
			manifests,
			postOptions,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func createOrUpdateEtcdStack(
	ctx context.Context,
	logger *log.Entry,
	config channel.Config,
	cluster *api.Cluster,
	values map[string]interface{},
	etcdKmsKeyARN string,
	adapter *awsAdapter,
	bucketName string,
	instanceTypes *awsUtils.InstanceTypes,
) error {

	template, err := config.EtcdManifest(etcdStackFileName)
	if err != nil {
		return err
	}

	etcdStackName := etcdStackNameDefault

	if v, ok := cluster.ConfigItems[etcdStackNameConfigItemKey]; ok {
		etcdStackName = v
	}

	renderer := &FilesRenderer{
		awsAdapter:    adapter,
		cluster:       cluster,
		config:        config,
		directory:     "etcd",
		nodePool:      nil,
		instanceTypes: instanceTypes,
	}

	s3Path, err := renderer.RenderAndUploadFiles(values, bucketName, etcdKmsKeyARN)
	if err != nil {
		return err
	}

	logger.Debugf("Uploaded generated files to %s", s3Path)
	values[s3GeneratedFilesPathValuesKey] = s3Path

	rendered, err := renderSingleTemplate(template, cluster, nil, values, adapter, instanceTypes)
	if err != nil {
		return err
	}

	tags := map[string]string{
		applicationTagKey: "kubernetes",
		componentTagKey:   "etcd-cluster",
	}

	err = adapter.applyStack(etcdStackName, rendered, "", tags, true, &stackPolicy{
		Statements: []stackPolicyStatement{
			{
				Effect:    stackPolicyEffectAllow,
				Action:    []stackPolicyAction{stackPolicyActionUpdateAll},
				Principal: stackPolicyPrincipalAll,
				Resource:  []string{"*"},
			},
			{
				Effect:    stackPolicyEffectDeny,
				Action:    []stackPolicyAction{stackPolicyActionUpdateReplace, stackPolicyActionUpdateDelete},
				Principal: stackPolicyPrincipalAll,
				Condition: &stackPolicyCondition{
					StringEquals: stackPolicyConditionStringEquals{
						ResourceType: []string{"AWS::AutoScaling::AutoScalingGroup", "AWS::S3::Bucket", "AWS::IAM::Role"},
					},
				},
			},
		},
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, maxWaitTimeout)
	defer cancel()
	err = adapter.waitForStack(ctx, waitTime, etcdStackName)
	if err != nil {
		return err
	}

	return nil
}

func createOrUpdateClusterStack(ctx context.Context, config channel.Config, cluster *api.Cluster, values map[string]interface{}, adapter *awsAdapter, bucketName string, instanceTypes *awsUtils.InstanceTypes) (map[string]string, error) {
	template, err := config.StackManifest(clusterStackFileName)
	if err != nil {
		return nil, err
	}

	rendered, err := renderSingleTemplate(template, cluster, nil, values, adapter, instanceTypes)
	if err != nil {
		return nil, err
	}

	err = adapter.applyClusterStack(cluster.LocalID, rendered, cluster, bucketName)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, maxWaitTimeout)
	defer cancel()
	err = adapter.waitForStack(ctx, waitTime, cluster.LocalID)
	if err != nil {
		return nil, err
	}

	clusterStack, err := adapter.getStackByName(cluster.LocalID)
	if err != nil {
		return nil, err
	}

	outputs := map[string]string{}
	for _, o := range clusterStack.Outputs {
		outputs[aws.StringValue(o.OutputKey)] = aws.StringValue(o.OutputValue)
	}

	return outputs, nil
}

func filterSubnets(subnets []*ec2.Subnet, filter func(*ec2.Subnet) bool) []*ec2.Subnet {
	var filtered []*ec2.Subnet
	for _, subnet := range subnets {
		if filter(subnet) {
			filtered = append(filtered, subnet)
		}
	}

	return filtered
}

func subnetIDIncluded(ids []string) func(*ec2.Subnet) bool {
	return func(subnet *ec2.Subnet) bool {
		for _, id := range ids {
			if aws.StringValue(subnet.SubnetId) == id {
				return true
			}
		}

		return false
	}
}

func isCustomSubnet(subnet *ec2.Subnet) bool {
	for _, tag := range subnet.Tags {
		if aws.StringValue(tag.Key) == customSubnetTag {
			return true
		}
	}

	return false
}

func subnetNot(predicate func(*ec2.Subnet) bool) func(*ec2.Subnet) bool {
	return func(s *ec2.Subnet) bool {
		return !predicate(s)
	}
}

// selectSubnetIDs finds the best suiting subnets based on tags for each AZ.
//
// It follows almost the same logic for finding subnets as the
// kube-controller-manager when finding subnets for ELBs used for services of
// type LoadBalancer.
// https://github.com/kubernetes/kubernetes/blob/65efeee64f772e0f38037e91a677138a335a7570/pkg/cloudprovider/providers/aws/aws.go#L2949-L3027
func selectSubnetIDs(subnets []*ec2.Subnet) *AZInfo {
	subnetsByAZ := make(map[string]*ec2.Subnet)
	for _, subnet := range subnets {
		az := aws.StringValue(subnet.AvailabilityZone)

		existing, ok := subnetsByAZ[az]
		if !ok {
			subnetsByAZ[az] = subnet
			continue
		}

		// prefer subnet with an ELB role tag
		existingTags := tagsToMap(existing.Tags)
		subnetTags := tagsToMap(subnet.Tags)
		_, existingHasTag := existingTags[subnetELBRoleTagName]
		_, subnetHasTag := subnetTags[subnetELBRoleTagName]

		if existingHasTag != subnetHasTag {
			if subnetHasTag {
				subnetsByAZ[az] = subnet
			}
			continue
		}

		// If we have two subnets for the same AZ we arbitrarily choose
		// the one that is first lexicographically.
		if strings.Compare(aws.StringValue(existing.SubnetId), aws.StringValue(subnet.SubnetId)) > 0 {
			subnetsByAZ[az] = subnet
		}
	}

	result := make(map[string]string, len(subnetsByAZ))
	for az, subnet := range subnetsByAZ {
		result[az] = aws.StringValue(subnet.SubnetId)
	}

	return &AZInfo{subnets: result}
}

// Decommission decommissions a cluster provisioned in AWS.
func (p *clusterpyProvisioner) decommission(
	ctx context.Context,
	logger *log.Entry,
	awsAdapter *awsAdapter,
	tokenSource oauth2.TokenSource,
	cluster *api.Cluster,
	caData []byte,
) error {
	logger.Infof("Decommissioning cluster: %s (%s)", cluster.Alias, cluster.ID)

	// scale down kube-system deployments
	// This is done to ensure controllers stop running so they don't
	// recreate resources we delete in the next step
	err := backoff.Retry(
		func() error {
			err := p.downscaleDeployments(
				ctx,
				logger,
				cluster,
				tokenSource,
				caData,
				"kube-system",
			)
			if err != nil {
				logger.Debugf("Failed to downscale deployments, will retry: %s", err.Error())
			}
			return err
		},
		backoff.WithMaxRetries(backoff.NewConstantBackOff(10*time.Second), 5))
	if err != nil {
		logger.Errorf("Unable to downscale the deployments, proceeding anyway: %v", err)
	}

	// decommission karpenter node-pools, since karpenter controller is decommissioned. we need to clean up ec2 resources
	ec2Backend := updatestrategy.NewEC2NodePoolBackend(cluster.ID, awsAdapter.session, func() (*updatestrategy.KarpenterCRDNameResolver, error) {
		k8sClients, err := kubernetes.NewClientsCollection(
			cluster.APIServerURL,
			tokenSource,
			caData,
		)
		if err != nil {
			return nil, err
		}
		return updatestrategy.NewKarpenterCRDResolver(ctx, k8sClients)
	})
	err = ec2Backend.DecommissionKarpenterNodes(ctx)
	if err != nil {
		logger.Errorf("Unable to decommission karpenter node-pools, proceeding anyway: %v", err)
	}
	// make E2E tests and deletions less flaky
	// The problem is that we scale down kube-ingress-aws-controller deployment
	// and just after that we delete CF stacks, but if the pod
	// kube-ingress-aws-controller is running, then it breaks the CF deletion.
	numberOfStacks := 1
	for i := 0; i < maxApplyRetries && numberOfStacks != 0; i++ {
		// delete all cluster infrastructure stacks
		err = p.deleteClusterStacks(ctx, awsAdapter, cluster)
		cfstacks, err2 := p.listClusterStacks(ctx, awsAdapter, cluster)
		if err2 != nil {
			return err2
		}
		numberOfStacks = len(cfstacks)
	}
	if err != nil {
		return err
	}

	stack := &cloudformation.Stack{
		StackName: aws.String(cluster.LocalID),
	}

	// delete the main cluster stack
	err = awsAdapter.DeleteStack(ctx, stack)
	if err != nil {
		return err
	}

	if p.removeVolumes {
		backoffCfg := backoff.NewExponentialBackOff()
		backoffCfg.MaxElapsedTime = defaultMaxRetryTime
		err = backoff.Retry(
			func() error {
				return p.removeEBSVolumes(awsAdapter, cluster)
			},
			backoffCfg)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *clusterpyProvisioner) removeEBSVolumes(awsAdapter *awsAdapter, cluster *api.Cluster) error {
	clusterTag := fmt.Sprintf("kubernetes.io/cluster/%s", cluster.ID)
	volumes, err := awsAdapter.GetVolumes(map[string]string{clusterTag: "owned"})
	if err != nil {
		return err
	}

	for _, volume := range volumes {
		switch aws.StringValue(volume.State) {
		case ec2.VolumeStateDeleted, ec2.VolumeStateDeleting:
			// skip
		case ec2.VolumeStateAvailable:
			err := awsAdapter.DeleteVolume(aws.StringValue(volume.VolumeId))
			if err != nil {
				return fmt.Errorf("failed to delete EBS volume %s: %s", aws.StringValue(volume.VolumeId), err)
			}
		default:
			return fmt.Errorf("unable to delete EBS volume %s: volume in state %s", aws.StringValue(volume.VolumeId), aws.StringValue(volume.State))
		}
	}

	return nil
}

// waitForAPIServer waits a cluster API server to be ready. It's considered
// ready when it's reachable.
func waitForAPIServer(logger *log.Entry, cluster *api.Cluster, maxTimeout time.Duration, tokenSource oauth2.TokenSource) error {
	logger.Infof("Waiting for API Server to be reachable")
	client := &http.Client{}
	timeout := time.Now().UTC().Add(maxTimeout)

	for time.Now().UTC().Before(timeout) {
		req, err := http.NewRequest(http.MethodGet, cluster.APIServerURL+"/readyz", nil)
		if err != nil {
			return err
		}

		token, err := tokenSource.Token()
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			return nil
		}

		logger.Debugf("Waiting for API Server to be reachable")

		time.Sleep(15 * time.Second)
	}

	return fmt.Errorf("%q was not ready after %s", cluster.APIServerURL, maxTimeout.String())
}

// setupAWSAdapter sets up the AWS Adapter used for communicating with AWS.
func (p *clusterpyProvisioner) setupAWSAdapter(logger *log.Entry, cluster *api.Cluster) (*awsAdapter, error) {
	infrastructureAccount := strings.Split(cluster.InfrastructureAccount, ":")
	if len(infrastructureAccount) != 2 {
		return nil, fmt.Errorf("clusterpy: Unknown format for infrastructure account '%s", cluster.InfrastructureAccount)
	}

	if infrastructureAccount[0] != "aws" {
		return nil, fmt.Errorf("clusterpy: Cannot work with cloud provider '%s", infrastructureAccount[0])
	}

	roleArn := p.assumedRole
	if roleArn != "" {
		roleArn = fmt.Sprintf("arn:aws:iam::%s:role/%s", infrastructureAccount[1], p.assumedRole)
	}

	awsConfig := p.awsConfig.Copy()
	awsConfig.Region = aws.String(cluster.Region)
	sess, err := awsUtils.Session(awsConfig, roleArn)
	if err != nil {
		return nil, err
	}

	adapter := newAWSAdapter(logger, cluster.APIServerURL, cluster.Region, sess, p.dryRun)
	err = adapter.VerifyAccount(cluster.InfrastructureAccount)
	if err != nil {
		return nil, err
	}

	return adapter, nil
}

// applyDefaultsToManifests applies the default values to all manifests's Config
// Items.
func (p *clusterpyProvisioner) applyDefaultsToManifests(
	adapter *awsAdapter,
	cluster *api.Cluster,
	channelConfig channel.Config,
	instanceTypes *awsUtils.InstanceTypes,
) error {
	err := p.updateDefaults(cluster, channelConfig, adapter, instanceTypes)
	if err != nil {
		return fmt.Errorf("unable to read configuration defaults: %v", err)
	}

	err = p.decryptConfigItems(cluster)
	if err != nil {
		return fmt.Errorf("unable to decrypt config items: %v", err)
	}

	p.propagateConfigItemsToNodePools(cluster)

	return nil
}

func (p *clusterpyProvisioner) updater(
	logger *log.Entry,
	awsAdapter *awsAdapter,
	tokenSource oauth2.TokenSource,
	cluster *api.Cluster,
	options *HookResponse,
) (updatestrategy.UpdateStrategy, error) {

	// allow clusters to override their update strategy.
	// use global update strategy if cluster doesn't define one.
	updateStrategy, ok := cluster.ConfigItems[configKeyUpdateStrategy]
	if !ok {
		updateStrategy = p.updateStrategy.Strategy
	}

	drainConfig := &updatestrategy.DrainConfig{
		ForceEvictionGracePeriod:       p.updateStrategy.ForceEvictionGracePeriod,
		MinPodLifetime:                 p.updateStrategy.MinPodLifetime,
		MinHealthyPDBSiblingLifetime:   p.updateStrategy.MinHealthyPDBSiblingLifetime,
		MinUnhealthyPDBSiblingLifetime: p.updateStrategy.MinUnhealthyPDBSiblingLifetime,
		ForceEvictionInterval:          p.updateStrategy.ForceEvictionInterval,
		PollInterval:                   p.updateStrategy.PollInterval,
	}

	var decodedCA []byte
	if options != nil {
		decodedCA = options.CAData
	}
	client, err := kubernetes.NewClient(
		cluster.APIServerURL,
		tokenSource,
		decodedCA,
	)
	if err != nil {
		return nil, err
	}

	// setup updater

	// allow clusters to override their drain settings
	for _, setting := range []struct {
		key string
		fn  func(duration time.Duration)
	}{
		{"drain_grace_period", func(v time.Duration) { drainConfig.ForceEvictionGracePeriod = v }},
		{"drain_min_pod_lifetime", func(v time.Duration) { drainConfig.MinPodLifetime = v }},
		{"drain_min_healthy_sibling_lifetime", func(v time.Duration) { drainConfig.MinHealthyPDBSiblingLifetime = v }},
		{"drain_min_unhealthy_sibling_lifetime", func(v time.Duration) { drainConfig.MinUnhealthyPDBSiblingLifetime = v }},
		{"drain_force_evict_interval", func(v time.Duration) { drainConfig.ForceEvictionInterval = v }},
		{"drain_poll_interval", func(v time.Duration) { drainConfig.PollInterval = v }},
	} {
		if value, ok := cluster.ConfigItems[setting.key]; ok {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return nil, fmt.Errorf("invalid value for %s: %v", setting.key, err)
			}
			setting.fn(parsed)
		}
	}

	noScheduleTaint := false
	if v, _ := cluster.ConfigItems[decommissionNodeNoScheduleTaintKey]; v == "true" {
		noScheduleTaint = true
	}
	k8sClients, err := kubernetes.NewClientsCollection(
		cluster.APIServerURL,
		tokenSource,
		decodedCA,
	)
	if err != nil {
		return nil, err
	}

	additionalBackends := map[string]updatestrategy.ProviderNodePoolsBackend{
		karpenterNodePoolProfile: updatestrategy.NewEC2NodePoolBackend(cluster.ID, awsAdapter.session, func() (*updatestrategy.KarpenterCRDNameResolver, error) {
			return updatestrategy.NewKarpenterCRDResolver(context.Background(), k8sClients)
		}),
	}

	asgBackend := updatestrategy.NewASGNodePoolsBackend(cluster.ID, awsAdapter.session)
	poolBackend := updatestrategy.NewProfileNodePoolsBackend(asgBackend, additionalBackends)
	poolManager := updatestrategy.NewKubernetesNodePoolManager(logger, client, poolBackend, drainConfig, noScheduleTaint)

	var updater updatestrategy.UpdateStrategy
	switch updateStrategy {
	case updateStrategyRolling:
		updater = updatestrategy.NewRollingUpdateStrategy(logger, poolManager, 3)
	case updateStrategyCLC:
		updater = updatestrategy.NewCLCUpdateStrategy(logger, poolManager, clcPollingInterval)
	default:
		return nil, fmt.Errorf("unknown update strategy: %s", p.updateStrategy)
	}

	return updater, nil
}

// downscaleDeployments scales down all deployments of a cluster in the
// specified namespace.
func (p *clusterpyProvisioner) downscaleDeployments(
	ctx context.Context,
	logger *log.Entry,
	cluster *api.Cluster,
	tokenSource oauth2.TokenSource,
	caData []byte,
	namespace string,
) error {
	client, err := kubernetes.NewClient(
		cluster.APIServerURL,
		tokenSource,
		caData,
	)
	if err != nil {
		return err
	}

	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, deployment := range deployments.Items {
		if int32Value(deployment.Spec.Replicas) == 0 {
			continue
		}

		logger.Infof("Scaling down deployment %s/%s", namespace, deployment.Name)
		deployment.Spec.Replicas = int32Ptr(0)
		_, err := client.AppsV1().Deployments(namespace).Update(ctx, &deployment, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *clusterpyProvisioner) listClusterStacks(_ context.Context, adapter *awsAdapter, cluster *api.Cluster) ([]*cloudformation.Stack, error) {
	includeTags := map[string]string{
		tagNameKubernetesClusterPrefix + cluster.ID: resourceLifecycleOwned,
	}
	excludeTags := map[string]string{
		mainStackTagKey: stackTagValueTrue,
	}

	return adapter.ListStacks(includeTags, excludeTags)
}

// deleteClusterStacks deletes all stacks tagged by the cluster id.
func (p *clusterpyProvisioner) deleteClusterStacks(ctx context.Context, adapter *awsAdapter, cluster *api.Cluster) error {
	stacks, err := p.listClusterStacks(ctx, adapter, cluster)
	if err != nil {
		return err
	}
	errorsc := make(chan error, len(stacks))

	for _, stack := range stacks {
		go func(stack cloudformation.Stack, errorsc chan error) {
			deleteStack := func() error {
				err := adapter.DeleteStack(ctx, &stack)
				if err != nil {
					if isWrongStackStatusErr(err) {
						return err
					}
					return backoff.Permanent(err)
				}
				return nil
			}

			backoffCfg := backoff.NewExponentialBackOff()
			backoffCfg.MaxElapsedTime = defaultMaxRetryTime
			err := backoff.Retry(deleteStack, backoffCfg)
			if err != nil {
				err = fmt.Errorf("failed to delete stack %s: %s", aws.StringValue(stack.StackName), err)
			}
			errorsc <- err
		}(*stack, errorsc)
	}

	errorStrs := make([]string, 0, len(stacks))
	for i := 0; i < len(stacks); i++ {
		err := <-errorsc
		if err != nil {
			errorStrs = append(errorStrs, err.Error())
		}
	}

	if len(errorStrs) > 0 {
		return errors.New(strings.Join(errorStrs, ", "))
	}

	return nil
}

// hasTag returns true if tag is found in list of tags.
func hasTag(tags []*ec2.Tag, tag *ec2.Tag) bool {
	for _, t := range tags {
		if aws.StringValue(t.Key) == aws.StringValue(tag.Key) &&
			aws.StringValue(t.Value) == aws.StringValue(tag.Value) {
			return true
		}
	}
	return false
}

// deletions defines two list of resources to be deleted. One before applying
// all manifests and one after applying all manifests.
type deletions struct {
	PreApply  []*kubernetes.Resource `yaml:"pre_apply"`
	PostApply []*kubernetes.Resource `yaml:"post_apply"`
}

// Deletions deletes the provided kubernetes resources from the cluster.
func (p *clusterpyProvisioner) Deletions(
	ctx context.Context,
	logger *log.Entry,
	tokenSource oauth2.TokenSource,
	cluster *api.Cluster,
	deletions []*kubernetes.Resource,
	options *HookResponse,
) error {
	var caData []byte
	if options != nil {
		caData = options.CAData
	}
	k8sClients, err := kubernetes.NewClientsCollection(
		cluster.APIServerURL,
		tokenSource,
		caData,
	)
	if err != nil {
		return err
	}

	for _, deletion := range deletions {
		err := k8sClients.DeleteResource(ctx, logger, deletion)
		if err != nil {
			return err
		}
	}

	return nil
}

// parseDeletions reads and parses the deletions from the config.
func parseDeletions(config channel.Config, cluster *api.Cluster, values map[string]interface{}, adapter *awsAdapter, instanceTypes *awsUtils.InstanceTypes) (*deletions, error) {
	result := &deletions{}

	deletionsFiles, err := config.DeletionsManifests()
	if err != nil {
		return nil, err
	}

	for _, deletionsFile := range deletionsFiles {
		res, err := renderSingleTemplate(deletionsFile, cluster, nil, values, adapter, instanceTypes)
		if err != nil {
			return nil, err
		}

		var deletions deletions
		err = yaml.Unmarshal([]byte(res), &deletions)
		if err != nil {
			return nil, err
		}

		result.PreApply = append(result.PreApply, deletions.PreApply...)
		result.PostApply = append(result.PostApply, deletions.PostApply...)
	}

	return result, nil
}

func remarshalYAML(contents string) (string, error) {
	decoder := yaml.NewDecoder(strings.NewReader(contents))
	result := &bytes.Buffer{}
	encoder := yaml.NewEncoder(result)

	for {
		var obj interface{}
		err := decoder.Decode(&obj)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if obj == nil {
			continue
		}

		err = encoder.Encode(obj)
		if err != nil {
			return "", err
		}
	}

	return result.String(), nil
}

func renderManifests(config channel.Config, cluster *api.Cluster, values map[string]interface{}, adapter *awsAdapter, instanceTypes *awsUtils.InstanceTypes) ([]manifestPackage, error) {
	var result []manifestPackage

	components, err := config.Components()
	if err != nil {
		return nil, err
	}

	for _, component := range components {
		fileData := make(map[string][]byte)
		for _, manifest := range component.Manifests {
			fileData[manifest.Path] = manifest.Contents
		}
		ctx := newTemplateContext(fileData, cluster, nil, values, adapter, instanceTypes)

		var renderedManifests []string

		for _, manifest := range component.Manifests {
			rendered, err := renderTemplate(ctx, manifest.Path)
			if err != nil {
				return nil, fmt.Errorf("error rendering template %s: %v", manifest.Path, err)
			}

			// We remarshal the manifest here to get rid of references
			remarshaled, err := remarshalYAML(rendered)
			if err != nil {
				return nil, fmt.Errorf("error remarshaling manifest %s: %v", manifest.Path, err)
			}

			// If there's no content we skip the manifest
			if remarshaled == "" {
				log.Debugf("Skipping empty file: %s", manifest.Path)
			} else {
				renderedManifests = append(renderedManifests, remarshaled)
			}
		}

		if len(renderedManifests) > 0 {
			result = append(result, manifestPackage{
				name:      component.Name,
				manifests: renderedManifests,
			})
		}
	}

	return result, nil
}

// apply runs pre-apply deletions, applies pre-rendered manifests and then runs post-apply deletions
func (p *clusterpyProvisioner) apply(
	ctx context.Context,
	logger *log.Entry,
	tokenSource oauth2.TokenSource,
	cluster *api.Cluster,
	deletions *deletions,
	renderedManifests []manifestPackage,
	options *HookResponse,
) error {
	logger.Debugf("Running PreApply deletions (%d)", len(deletions.PreApply))
	err := p.Deletions(
		ctx,
		logger,
		tokenSource,
		cluster,
		deletions.PreApply,
		options,
	)
	if err != nil {
		return err
	}

	logger.Debugf("Starting Apply")

	//validating input
	if !strings.HasPrefix(cluster.InfrastructureAccount, "aws:") {
		return fmt.Errorf("wrong format for string InfrastructureAccount: %s", cluster.InfrastructureAccount)
	}

	var caData []byte
	if options != nil {
		caData = options.CAData
	}
	for _, m := range renderedManifests {
		logger := logger.WithField("module", m.name)
		kubectlRunner := kubernetes.NewKubeCTLRunner(
			p.execManager,
			tokenSource,
			logger,
			cluster.APIServerURL,
			maxApplyRetries,
			caData,
		)
		_, err := kubectlRunner.KubectlExecute(ctx, []string{"apply"}, strings.Join(m.manifests, "---\n"), p.dryRun)
		if err != nil {
			return errors.Wrapf(err, "kubectl apply failed for %s", m.name)
		}
	}

	logger.Debugf("Running PostApply deletions (%d)", len(deletions.PostApply))
	err = p.Deletions(
		ctx,
		logger,
		tokenSource,
		cluster,
		deletions.PostApply,
		options,
	)
	if err != nil {
		return err
	}

	return nil
}

func int32Ptr(i int32) *int32 { return &i }

func int32Value(v *int32) int32 {
	if v != nil {
		return *v
	}
	return 0
}

type nodePoolGroup struct {
	NodePools   []*api.NodePool
	Provisioner NodePoolProvisioner
	ReadyFn     func() error
}

func groupNodePools(logger *log.Entry, cluster *api.Cluster, caProvisioner *AWSNodePoolProvisioner, karProvisioner *KarpenterNodePoolProvisioner, tokenSource oauth2.TokenSource) map[string]*nodePoolGroup {

	var masters, workers, karpenterPools []*api.NodePool
	for _, nodePool := range cluster.NodePools {
		if nodePool.IsMaster() {
			masters = append(masters, nodePool)
			continue
		}
		if nodePool.IsKarpenter() {
			karpenterPools = append(karpenterPools, nodePool)
			continue
		}

		workers = append(workers, nodePool)
	}

	return map[string]*nodePoolGroup{
		"masters": {
			NodePools:   masters,
			Provisioner: caProvisioner,
			ReadyFn: func() error {
				return waitForAPIServer(
					logger,
					cluster,
					15*time.Minute,
					tokenSource,
				)
			},
		},
		"workers": {
			NodePools:   workers,
			Provisioner: caProvisioner,
			ReadyFn: func() error {
				// TODO: EKS wait for at least one node?
				return nil
			},
		},
		"karpenterPools": {
			NodePools:   karpenterPools,
			Provisioner: karProvisioner,
			ReadyFn: func() error {
				return nil
			},
		},
	}
}

func (npg *nodePoolGroup) provisionNodePoolGroup(ctx context.Context, values map[string]interface{}, updater updatestrategy.UpdateStrategy, cluster *api.Cluster, applyOnly bool) error {
	err := npg.Provisioner.Provision(ctx, npg.NodePools, values)
	if err != nil {
		return err
	}

	// custom function that checks if the node pools are "ready"
	err = npg.ReadyFn()
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	if !applyOnly {
		switch cluster.LifecycleStatus {
		case models.ClusterLifecycleStatusRequested, models.ClusterUpdateLifecycleStatusCreating:
			log.Warnf("New cluster (%s), skipping node pool update", cluster.LifecycleStatus)
		default:
			// update nodes
			for _, nodePool := range npg.NodePools {
				err := updater.Update(ctx, nodePool)
				if err != nil {
					return err
				}

				if err = ctx.Err(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
