package provisioner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
	"unicode"

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
	"github.com/zalando-incubator/kube-ingress-aws-controller/certs"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	providerID                         = "zalando-aws"
	etcdStackFileName                  = "etcd-cluster.yaml"
	clusterStackFileName               = "cluster.yaml"
	defaultNamespace                   = "default"
	kubectlNotFound                    = "(NotFound)"
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
)

type clusterpyProvisioner struct {
	awsConfig       *aws.Config
	execManager     *command.ExecManager
	secretDecrypter decrypter.Decrypter
	assumedRole     string
	dryRun          bool
	tokenSource     oauth2.TokenSource
	applyOnly       bool
	updateStrategy  config.UpdateStrategy
	removeVolumes   bool
}

type manifestPackage struct {
	name      string
	manifests []string
}

// NewClusterpyProvisioner returns a new ClusterPy provisioner by passing its location and and IAM role to use.
func NewClusterpyProvisioner(execManager *command.ExecManager, tokenSource oauth2.TokenSource, secretDecrypter decrypter.Decrypter, assumedRole string, awsConfig *aws.Config, options *Options) Provisioner {
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
	}

	return provisioner
}

func (p *clusterpyProvisioner) Supports(cluster *api.Cluster) bool {
	return cluster.Provider == providerID
}

func (p *clusterpyProvisioner) updateDefaults(cluster *api.Cluster, channelConfig channel.Config, adapter *awsAdapter) error {
	defaultsFiles, err := channelConfig.DefaultsManifests()
	if err != nil {
		return err
	}

	withoutConfigItems := *cluster
	withoutConfigItems.ConfigItems = make(map[string]string)

	allDefaults := make(map[string]string)

	for _, file := range defaultsFiles {
		result, err := renderSingleTemplate(file, &withoutConfigItems, nil, nil, adapter)
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
			return err
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

// Provision provisions/updates a cluster on AWS. Provision is an idempotent
// operation for the same input.
func (p *clusterpyProvisioner) Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, channelConfig channel.Config) error {
	awsAdapter, updater, nodePoolManager, err := p.prepareProvision(logger, cluster, channelConfig)
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

	err = p.tagSubnets(awsAdapter, vpcID, cluster)
	if err != nil {
		return err
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

	// if subnets are defined in the config items, filter the subnet list
	if subnetIds, ok := cluster.ConfigItems[subnetsConfigItemKey]; ok {
		subnets, err = filterSubnets(subnets, strings.Split(subnetIds, ","))
		if err != nil {
			return err
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
		// TODO(tech-debt): custom legacy value
		"node_labels": fmt.Sprintf("lifecycle-status=%s", lifecycleStatusReady),
		// TODO(tech-debt): custom legacy value
		"apiserver_count":           "1",
		subnetsValueKey:             azInfo.SubnetsByAZ(),
		availabilityZonesValueKey:   azInfo.AvailabilityZones(),
		"hosted_zone":               hostedZone,
		"load_balancer_certificate": loadBalancerCert.ID(),
		"vpc_ipv4_cidr":             aws.StringValue(vpc.CidrBlock),
	}

	// render the manifests to find out if they're valid
	deletions, err := parseDeletions(channelConfig, cluster, values, awsAdapter)
	if err != nil {
		return err
	}
	manifests, err := renderManifests(channelConfig, cluster, values, awsAdapter)
	if err != nil {
		return err
	}

	// create etcd stack if needed.
	etcdStackDefinition, err := channelConfig.StackManifest(etcdStackFileName)
	if err != nil {
		return err
	}

	err = awsAdapter.CreateOrUpdateEtcdStack(ctx, "etcd-cluster-etcd", etcdStackDefinition.Contents, aws.StringValue(vpc.CidrBlock), aws.StringValue(vpc.VpcId), cluster)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	// create bucket name with aws account ID to ensure uniqueness across
	// accounts.
	bucketName := fmt.Sprintf(clmCFBucketPattern, strings.TrimPrefix(cluster.InfrastructureAccount, "aws:"), cluster.Region)

	outputs, err := createOrUpdateClusterStack(ctx, channelConfig, cluster, values, awsAdapter, bucketName)
	if err != nil {
		return err
	}
	values[clusterStackOutputKey] = outputs

	if err = ctx.Err(); err != nil {
		return err
	}

	hostname, err := getHostname(cluster.APIServerURL)
	if err != nil {
		return err
	}
	provisioner, err := NewOpenIDProviderProvisioner(awsAdapter, hostname)
	if err != nil {
		return fmt.Errorf("failed to create oidc provisioner: %v", err)
	}
	err = provisioner.Provision()
	if err != nil {
		return fmt.Errorf("failed to reconcile openid-configuration: %v", err)
	}

	// provision node pools
	nodePoolProvisioner := &AWSNodePoolProvisioner{
		awsAdapter:      awsAdapter,
		nodePoolManager: nodePoolManager,
		bucketName:      bucketName,
		config:          channelConfig,
		cluster:         cluster,
		azInfo:          azInfo,
		logger:          logger,
	}

	// group node pools based on their profile e.g. master or spot.io
	nodePoolGroups := groupNodePools(
		logger,
		cluster,
		updater,
	)

	for _, g := range nodePoolGroups {
		err := nodePoolProvisioner.Provision(ctx, g.NodePools, values)
		if err != nil {
			return err
		}

		// custom function that checks if the node pools are "ready"
		err = g.ReadyFn()
		if err != nil {
			return err
		}

		if err = ctx.Err(); err != nil {
			return err
		}

		if !p.applyOnly {
			switch cluster.LifecycleStatus {
			case models.ClusterLifecycleStatusRequested, models.ClusterUpdateLifecycleStatusCreating:
				log.Warnf("New cluster (%s), skipping node pool update", cluster.LifecycleStatus)
			default:
				// update nodes
				for _, nodePool := range g.NodePools {
					err := g.Updater.Update(ctx, nodePool)
					if err != nil {
						return err
					}

					if err = ctx.Err(); err != nil {
						return err
					}
				}
			}
		}
	}

	// clean up removed node pools
	err = nodePoolProvisioner.Reconcile(ctx, updater)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	return p.apply(ctx, logger, cluster, deletions, manifests)
}

func createOrUpdateClusterStack(ctx context.Context, config channel.Config, cluster *api.Cluster, values map[string]interface{}, adapter *awsAdapter, bucketName string) (map[string]string, error) {
	template, err := config.StackManifest(clusterStackFileName)
	if err != nil {
		return nil, err
	}

	rendered, err := renderSingleTemplate(template, cluster, nil, values, adapter)
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

func filterSubnets(allSubnets []*ec2.Subnet, subnetIds []string) ([]*ec2.Subnet, error) {
	desiredSubnets := make(map[string]struct{})
	for _, id := range subnetIds {
		desiredSubnets[id] = struct{}{}
	}

	var result []*ec2.Subnet
	for _, subnet := range allSubnets {
		subnet := *subnet
		subnetID := aws.StringValue(subnet.SubnetId)
		_, ok := desiredSubnets[subnetID]
		if ok {
			result = append(result, &subnet)
			delete(desiredSubnets, subnetID)
		}
	}

	if len(desiredSubnets) > 0 {
		return nil, fmt.Errorf("invalid or unknown subnets: %s", desiredSubnets)
	}

	return result, nil
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
func (p *clusterpyProvisioner) Decommission(logger *log.Entry, cluster *api.Cluster) error {
	if cluster.Provider != providerID {
		return ErrProviderNotSupported
	}

	logger.Infof("Decommissioning cluster: %s (%s)", cluster.Alias, cluster.ID)

	awsAdapter, err := p.setupAWSAdapter(logger, cluster)
	if err != nil {
		return err
	}

	// scale down kube-system deployments
	// This is done to ensure controllers stop running so they don't
	// recreate resources we delete in the next step
	err = backoff.Retry(
		func() error {
			return p.downscaleDeployments(logger, cluster, "kube-system")
		},
		backoff.WithMaxRetries(backoff.NewConstantBackOff(10*time.Second), 5))
	if err != nil {
		logger.Errorf("Unable to downscale the deployments, proceeding anyway: %s", err)
	}

	// we don't support cancelling decommission operations yet
	ctx := context.Background()

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

	err = p.untagSubnets(awsAdapter, aws.StringValue(vpc.VpcId), cluster)
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

	providerHostname, err := getHostname(cluster.APIServerURL)
	if err != nil {
		return err
	}
	provisioner, err := NewOpenIDProviderProvisioner(awsAdapter, providerHostname)
	if err != nil {
		return fmt.Errorf("failed to create oidc provisioner: %v", err)
	}

	logger.Infof("Decommissioning openid connect providers: %s (%s)", cluster.Alias, cluster.ID)
	err = provisioner.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete openid provider: %v", err)
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
func waitForAPIServer(logger *log.Entry, server string, maxTimeout time.Duration) error {
	logger.Infof("Waiting for API Server to be reachable")
	client := &http.Client{}
	timeout := time.Now().UTC().Add(maxTimeout)

	for time.Now().UTC().Before(timeout) {
		resp, err := client.Get(server)
		if err == nil && resp.StatusCode < http.StatusInternalServerError {
			return nil
		}

		logger.Debugf("Waiting for API Server to be reachable")

		time.Sleep(15 * time.Second)
	}

	return fmt.Errorf("'%s' was not ready after %s", server, maxTimeout.String())
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

	sess, err := awsUtils.Session(p.awsConfig, roleArn)
	if err != nil {
		return nil, err
	}

	adapter, err := newAWSAdapter(logger, cluster.APIServerURL, cluster.Region, sess, p.tokenSource, p.dryRun)
	if err != nil {
		return nil, err
	}

	err = adapter.VerifyAccount(cluster.InfrastructureAccount)
	if err != nil {
		return nil, err
	}

	return adapter, nil
}

// prepareProvision checks that a cluster can be handled by the provisioner and
// prepares to provision a cluster by initializing the aws adapter.
// TODO: this is doing a lot of things to glue everything together, this should
// be refactored.
func (p *clusterpyProvisioner) prepareProvision(logger *log.Entry, cluster *api.Cluster, channelConfig channel.Config) (*awsAdapter, updatestrategy.UpdateStrategy, updatestrategy.NodePoolManager, error) {
	if cluster.Provider != providerID {
		return nil, nil, nil, ErrProviderNotSupported
	}

	logger.Infof("clusterpy: Prepare for provisioning cluster %s (%s)..", cluster.ID, cluster.LifecycleStatus)

	adapter, err := p.setupAWSAdapter(logger, cluster)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to setup AWS Adapter: %v", err)
	}

	err = p.updateDefaults(cluster, channelConfig, adapter)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to read configuration defaults: %v", err)
	}

	err = p.decryptConfigItems(cluster)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to decrypt config items: %v", err)
	}

	p.propagateConfigItemsToNodePools(cluster)

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

	client, err := kubernetes.NewKubeClientWithTokenSource(cluster.APIServerURL, p.tokenSource)
	if err != nil {
		return nil, nil, nil, err
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
				return nil, nil, nil, fmt.Errorf("invalid value for %s: %v", setting.key, err)
			}
			setting.fn(parsed)
		}
	}

	noScheduleTaint := false
	if v, _ := cluster.ConfigItems[decommissionNodeNoScheduleTaintKey]; v == "true" {
		noScheduleTaint = true
	}

	poolBackend := updatestrategy.NewASGNodePoolsBackend(cluster.ID, adapter.session)
	poolManager := updatestrategy.NewKubernetesNodePoolManager(logger, client, poolBackend, drainConfig, noScheduleTaint)

	var updater updatestrategy.UpdateStrategy
	switch updateStrategy {
	case updateStrategyRolling:
		updater = updatestrategy.NewRollingUpdateStrategy(logger, poolManager, 3)
	case updateStrategyCLC:
		updater = updatestrategy.NewCLCUpdateStrategy(logger, poolManager, clcPollingInterval)
	default:
		return nil, nil, nil, fmt.Errorf("unknown update strategy: %s", p.updateStrategy)
	}

	return adapter, updater, poolManager, nil
}

// tagSubnets tags all subnets in the default VPC with the kubernetes cluster
// id tag.
func (p *clusterpyProvisioner) tagSubnets(awsAdapter *awsAdapter, vpcID string, cluster *api.Cluster) error {
	subnets, err := awsAdapter.GetSubnets(vpcID)
	if err != nil {
		return err
	}

	tag := &ec2.Tag{
		Key:   aws.String(tagNameKubernetesClusterPrefix + cluster.ID),
		Value: aws.String(resourceLifecycleShared),
	}

	for _, subnet := range subnets {
		if !hasTag(subnet.Tags, tag) {
			err = awsAdapter.CreateTags(
				aws.StringValue(subnet.SubnetId),
				[]*ec2.Tag{tag},
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// untagSubnets removes the kubernetes cluster id tag from all subnets in the
// default vpc.
func (p *clusterpyProvisioner) untagSubnets(awsAdapter *awsAdapter, vpcID string, cluster *api.Cluster) error {
	subnets, err := awsAdapter.GetSubnets(vpcID)
	if err != nil {
		return err
	}

	tag := &ec2.Tag{
		Key:   aws.String(tagNameKubernetesClusterPrefix + cluster.ID),
		Value: aws.String(resourceLifecycleShared),
	}

	for _, subnet := range subnets {
		if hasTag(subnet.Tags, tag) {
			err = awsAdapter.DeleteTags(
				aws.StringValue(subnet.SubnetId),
				[]*ec2.Tag{tag},
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// downscaleDeployments scales down all deployments of a cluster in the
// specified namespace.
func (p *clusterpyProvisioner) downscaleDeployments(logger *log.Entry, cluster *api.Cluster, namespace string) error {
	client, err := kubernetes.NewKubeClientWithTokenSource(cluster.APIServerURL, p.tokenSource)
	if err != nil {
		return err
	}

	deployments, err := client.AppsV1beta1().Deployments(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, deployment := range deployments.Items {
		if int32Value(deployment.Spec.Replicas) == 0 {
			continue
		}

		logger.Infof("Scaling down deployment %s/%s", namespace, deployment.Name)
		deployment.Spec.Replicas = int32Ptr(0)
		_, err := client.AppsV1beta1().Deployments(namespace).Update(&deployment)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *clusterpyProvisioner) listClusterStacks(ctx context.Context, adapter *awsAdapter, cluster *api.Cluster) ([]*cloudformation.Stack, error) {
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

type labels map[string]string

// String returns a string representation of the labels map.
func (l labels) String() string {
	labels := make([]string, 0, len(l))
	for key, val := range l {
		labels = append(labels, fmt.Sprintf("%s=%s", key, val))
	}
	return strings.Join(labels, ",")
}

// resource defines a minimal difinition of a kubernetes resource.
type resource struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Kind      string `yaml:"kind"`
	Labels    labels `yaml:"labels"`
}

// deletions defines two list of resources to be deleted. One before applying
// all manifests and one after applying all manifests.
type deletions struct {
	PreApply  []*resource `yaml:"pre_apply"`
	PostApply []*resource `yaml:"post_apply"`
}

// Deletions uses kubectl delete to delete the provided kubernetes resources.
func (p *clusterpyProvisioner) Deletions(ctx context.Context, logger *log.Entry, cluster *api.Cluster, deletions []*resource) error {
	token, err := p.tokenSource.Token()
	if err != nil {
		return errors.Wrapf(err, "no valid token")
	}

	for _, deletion := range deletions {
		args := []string{
			"kubectl",
			fmt.Sprintf("--server=%s", cluster.APIServerURL),
			fmt.Sprintf("--token=%s", token.AccessToken),
			fmt.Sprintf("--namespace=%s", deletion.Namespace),
			"delete",
			deletion.Kind,
		}

		// indentify the resource to be deleted either by name or
		// labels. name AND labels cannot be defined at the same time,
		// but one of them MUST be defined.
		if deletion.Name != "" && len(deletion.Labels) > 0 {
			return fmt.Errorf("only one of 'name' or 'labels' must be specified")
		}

		if deletion.Name != "" {
			args = append(args, deletion.Name)
		} else if len(deletion.Labels) > 0 {
			args = append(args, fmt.Sprintf("--selector=%s", deletion.Labels))
		} else {
			return fmt.Errorf("either name or labels must be specified to identify a resource")
		}

		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = []string{}

		out, err := p.execManager.Run(ctx, logger, cmd)
		if err != nil {
			// if kubectl failed because the resource didn't
			// exists, we don't treat it as an error since the
			// resource was already deleted.
			// We can only check this by inspecting the content of
			// Stderr (which is provided in the err).
			if strings.Contains(out, kubectlNotFound) {
				continue
			}
			return errors.Wrap(err, "cannot run kubectl command")
		}
	}

	return nil
}

// parseDeletions reads and parses the deletions from the config.
func parseDeletions(config channel.Config, cluster *api.Cluster, values map[string]interface{}, adapter *awsAdapter) (*deletions, error) {
	result := &deletions{}

	deletionsFiles, err := config.DeletionsManifests()
	if err != nil {
		return nil, err
	}

	for _, deletionsFile := range deletionsFiles {
		res, err := renderSingleTemplate(deletionsFile, cluster, nil, values, adapter)
		if err != nil {
			return nil, err
		}

		var deletions deletions
		err = yaml.Unmarshal([]byte(res), &deletions)
		if err != nil {
			return nil, err
		}

		// ensure namespace is set, default to 'kube-system' if empty.
		for _, deletion := range deletions.PreApply {
			if deletion.Namespace == "" {
				deletion.Namespace = defaultNamespace
			}
		}

		for _, deletion := range deletions.PostApply {
			if deletion.Namespace == "" {
				deletion.Namespace = defaultNamespace
			}
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

	return string(result.Bytes()), nil
}

func renderManifests(config channel.Config, cluster *api.Cluster, values map[string]interface{}, adapter *awsAdapter) ([]manifestPackage, error) {
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
		ctx := newTemplateContext(fileData, cluster, nil, values, adapter)

		var renderedManifests []string

		for _, manifest := range component.Manifests {
			rendered, err := renderTemplate(ctx, manifest.Path)
			if err != nil {
				return nil, fmt.Errorf("error rendering template %s: %v", manifest.Path, err)
			}

			// If there's no content we skip the file.
			if stripWhitespace(rendered) == "" {
				log.Debugf("Skipping empty file: %s", manifest.Path)
				continue
			}

			// We remarshal the manifest here to get rid of references
			remarshaled, err := remarshalYAML(rendered)
			if err != nil {
				return nil, fmt.Errorf("error remarshaling manifest %s: %v", manifest.Path, err)
			}

			renderedManifests = append(renderedManifests, remarshaled)
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
func (p *clusterpyProvisioner) apply(ctx context.Context, logger *log.Entry, cluster *api.Cluster, deletions *deletions, renderedManifests []manifestPackage) error {
	logger.Debugf("Running PreApply deletions (%d)", len(deletions.PreApply))
	err := p.Deletions(ctx, logger, cluster, deletions.PreApply)
	if err != nil {
		return err
	}

	logger.Debugf("Starting Apply")

	//validating input
	if !strings.HasPrefix(cluster.InfrastructureAccount, "aws:") {
		return fmt.Errorf("wrong format for string InfrastructureAccount: %s", cluster.InfrastructureAccount)
	}

	token, err := p.tokenSource.Token()
	if err != nil {
		return errors.Wrapf(err, "no valid token")
	}

	for _, m := range renderedManifests {
		logger := logger.WithField("module", m.name)

		args := []string{
			"kubectl",
			"apply",
			fmt.Sprintf("--server=%s", cluster.APIServerURL),
			fmt.Sprintf("--token=%s", token.AccessToken),
			"-f",
			"-",
		}

		newApplyCommand := func() *exec.Cmd {
			cmd := exec.Command(args[0], args[1:]...)
			// prevent kubectl to find the in-cluster config
			cmd.Env = []string{}
			return cmd
		}

		if p.dryRun {
			logger.Debug(newApplyCommand())
		} else {
			applyManifest := func() error {
				cmd := newApplyCommand()
				cmd.Stdin = strings.NewReader(strings.Join(m.manifests, "---\n"))
				_, err := p.execManager.Run(ctx, logger, cmd)
				return err
			}
			err = backoff.Retry(applyManifest, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), maxApplyRetries))
			if err != nil {
				return errors.Wrapf(err, "kubectl apply failed for %s", m.name)
			}
		}
	}

	logger.Debugf("Running PostApply deletions (%d)", len(deletions.PostApply))
	err = p.Deletions(ctx, logger, cluster, deletions.PostApply)
	if err != nil {
		return err
	}

	return nil
}

func stripWhitespace(content string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, content)
}

func int32Ptr(i int32) *int32 { return &i }

func int32Value(v *int32) int32 {
	if v != nil {
		return *v
	}
	return 0
}

func groupNodePools(logger *log.Entry, cluster *api.Cluster, updater updatestrategy.UpdateStrategy) []nodePoolGroup {
	var masters, workers, spotIOWorkers []*api.NodePool
	var spotIOOceanWorker *api.NodePool
	for _, nodePool := range cluster.NodePools {
		if strings.HasPrefix(nodePool.Profile, "master") {
			masters = append(masters, nodePool)
			continue
		}

		if nodePool.Profile == "worker-spotio-ocean" {
			spotIOOceanWorker = nodePool
			continue
		}

		if nodePool.Profile == "worker-spotio" {
			spotIOWorkers = append(spotIOWorkers, nodePool)
			continue
		}

		workers = append(workers, nodePool)
	}

	return []nodePoolGroup{
		{
			NodePools: masters,
			Updater:   updater,
			ReadyFn: func() error {
				return waitForAPIServer(logger, cluster.APIServerURL, 15*time.Minute)
			},
		},
		{
			NodePools: workers,
			Updater:   updater,
			ReadyFn: func() error {
				return nil
			},
		},
		{
			NodePools: []*api.NodePool{spotIOOceanWorker},
			Updater:   spotIOUpdateStrategy{},
			ReadyFn: func() error {
				return nil
			},
		},
		{
			NodePools: spotIOWorkers,
			Updater:   spotIOUpdateStrategy{},
			ReadyFn: func() error {
				return nil
			},
		},
	}
}

type nodePoolGroup struct {
	NodePools []*api.NodePool
	Updater   updatestrategy.UpdateStrategy
	ReadyFn   func() error
}

type spotIOUpdateStrategy struct {
	updatestrategy.UpdateStrategy
}

func (s spotIOUpdateStrategy) Update(ctx context.Context, _ *api.NodePool) error {
	return nil
}
