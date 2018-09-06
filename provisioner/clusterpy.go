package provisioner

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
	"github.com/zalando-incubator/kube-ingress-aws-controller/certs"
	"gopkg.in/yaml.v2"

	"golang.org/x/oauth2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/cenkalti/backoff"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	awsUtils "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/updatestrategy"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
)

const (
	providerID                     = "zalando-aws"
	manifestsPath                  = "cluster/manifests"
	deletionsFile                  = "deletions.yaml"
	clusterStackFileName           = "cluster.yaml"
	defaultsFile                   = "cluster/config-defaults.yaml"
	defaultNamespace               = "default"
	kubectlNotFound                = "(NotFound)"
	tagNameKubernetesClusterPrefix = "kubernetes.io/cluster/"
	subnetELBRoleTagName           = "kubernetes.io/role/elb"
	resourceLifecycleShared        = "shared"
	resourceLifecycleOwned         = "owned"
	subnetsConfigItemKey           = "subnets"
	vpcIDConfigItemKey             = "vpc_id"
	vpcIPv4CIDRBlockConfigItemKey  = "vpc_ipv4_cidr"
	subnetAllAZName                = "*"
	maxApplyRetries                = 10
	configKeyUpdateStrategy        = "update_strategy"
	updateStrategyRolling          = "rolling"
	defaultMaxRetryTime            = 5 * time.Minute
)

type clusterpyProvisioner struct {
	awsConfig       *aws.Config
	secretDecrypter decrypter.Decrypter
	assumedRole     string
	dryRun          bool
	tokenSource     oauth2.TokenSource
	applyOnly       bool
	updateStrategy  config.UpdateStrategy
	removeVolumes   bool
}

// NewClusterpyProvisioner returns a new ClusterPy provisioner by passing its location and and IAM role to use.
func NewClusterpyProvisioner(tokenSource oauth2.TokenSource, secretDecrypter decrypter.Decrypter, assumedRole string, awsConfig *aws.Config, options *Options) Provisioner {
	provisioner := &clusterpyProvisioner{
		awsConfig:       awsConfig,
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

func (p *clusterpyProvisioner) updateDefaults(cluster *api.Cluster, channelConfig *channel.Config) error {
	defaultsFile := path.Join(channelConfig.Path, defaultsFile)

	withoutConfigItems := *cluster
	withoutConfigItems.ConfigItems = make(map[string]string)

	result, err := renderTemplate(newTemplateContext(channelConfig.Path), defaultsFile, &withoutConfigItems)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var defaults map[string]string
	err = yaml.Unmarshal([]byte(result), &defaults)
	if err != nil {
		return err
	}

	for k, v := range defaults {
		_, ok := cluster.ConfigItems[k]
		if !ok {
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

// Provision provisions/updates a cluster on AWS. Provision is an idempotent
// operation for the same input.
func (p *clusterpyProvisioner) Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, channelConfig *channel.Config) error {
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
	subnetsPerZone := selectSubnetIDs(subnets)

	// build a subnet list for the virtual '*' AZ
	for az, subnet := range subnetsPerZone {
		if az == subnetAllAZName {
			continue
		}
		if existing, ok := subnetsPerZone[subnetAllAZName]; ok {
			subnetsPerZone[subnetAllAZName] = existing + "," + subnet
		} else {
			subnetsPerZone[subnetAllAZName] = subnet
		}
	}

	// TODO legacy, remove once we switch to Values in all clusters
	if _, ok := cluster.ConfigItems[subnetsConfigItemKey]; !ok {
		cluster.ConfigItems[subnetsConfigItemKey] = subnetsPerZone[subnetAllAZName]
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
		"subnets":                   subnetsPerZone,
		"hosted_zone":               hostedZone,
		"load_balancer_certificate": loadBalancerCert.ID(),
		"vpc_ipv4_cidr":             aws.StringValue(vpc.CidrBlock),
	}

	// create etcd stack if needed.
	etcdStackDefinitionPath := path.Join(channelConfig.Path, "cluster", "etcd-cluster.yaml")

	err = awsAdapter.CreateOrUpdateEtcdStack(ctx, "etcd-cluster-etcd", etcdStackDefinitionPath, aws.StringValue(vpc.CidrBlock), aws.StringValue(vpc.VpcId), cluster)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	cfgBasePath := path.Join(channelConfig.Path, "cluster")

	// create bucket name with aws account ID to ensure uniqueness across
	// accounts.
	bucketName := fmt.Sprintf(clmCFBucketPattern, strings.TrimPrefix(cluster.InfrastructureAccount, "aws:"), cluster.Region)

	err = createOrUpdateClusterStack(awsAdapter, ctx, cfgBasePath, cluster, values, bucketName)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	cfgNodePoolBaseDir := path.Join(cfgBasePath, "node-pools")

	// provision node pools
	nodePoolProvisioner := &AWSNodePoolProvisioner{
		awsAdapter:      awsAdapter,
		nodePoolManager: nodePoolManager,
		bucketName:      bucketName,
		cfgBaseDir:      cfgNodePoolBaseDir,
		Cluster:         cluster,
		logger:          logger,
	}

	err = nodePoolProvisioner.Provision(values)
	if err != nil {
		return err
	}

	// wait for API server to be ready
	err = waitForAPIServer(logger, cluster.APIServerURL, 15*time.Minute)
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
			nodePools := cluster.NodePools

			sort.Sort(api.NodePools(nodePools))
			for _, nodePool := range nodePools {
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

	// clean up removed node pools
	err = nodePoolProvisioner.Reconcile(ctx)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	return p.apply(logger, cluster, path.Join(channelConfig.Path, manifestsPath))
}

type clusterStackParams struct {
	Cluster *api.Cluster
	Values  map[string]interface{}
}

func createOrUpdateClusterStack(awsAdapter *awsAdapter, ctx context.Context, baseDir string, cluster *api.Cluster, values map[string]interface{}, bucketName string) error {
	params := &clusterStackParams{
		Cluster: cluster,
		Values:  values,
	}

	stackFilePath := path.Join(baseDir, clusterStackFileName)
	output, err := renderTemplate(newTemplateContext(baseDir), stackFilePath, params)
	if err != nil {
		return err
	}

	err = awsAdapter.applyClusterStack(cluster.LocalID, output, cluster, bucketName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, maxWaitTimeout)
	defer cancel()
	err = awsAdapter.waitForStack(ctx, waitTime, cluster.LocalID)
	if err != nil {
		return err
	}
	return nil
}

func filterSubnets(allSubnets []*ec2.Subnet, subnetIds []string) ([]*ec2.Subnet, error) {
	desiredSubnets := make(map[string]struct{})
	for _, id := range subnetIds {
		desiredSubnets[id] = struct{}{}
	}

	var result []*ec2.Subnet
	for _, subnet := range allSubnets {
		subnet := aws.StringValue(subnet.SubnetId)
		_, ok := desiredSubnets[subnet]
		if ok {
			result = append(result)
			delete(desiredSubnets, subnet)
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
func selectSubnetIDs(subnets []*ec2.Subnet) map[string]string {
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

	return result
}

// Decommission decommissions a cluster provisioned in AWS.
func (p *clusterpyProvisioner) Decommission(logger *log.Entry, cluster *api.Cluster, channelConfig *channel.Config) error {
	awsAdapter, _, _, err := p.prepareProvision(logger, cluster, channelConfig)
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

	// delete all cluster infrastructure stacks
	err = p.deleteClusterStacks(ctx, awsAdapter, cluster)
	if err != nil {
		return err
	}

	// delete the main cluster stack
	err = awsAdapter.DeleteStack(ctx, cluster.LocalID)
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

// prepareProvision checks that a cluster can be handled by the provisioner and
// prepares to provision a cluster by initializing the aws adapter.
// TODO: this is doing a lot of things to glue everything together, this should
// be refactored.
func (p *clusterpyProvisioner) prepareProvision(logger *log.Entry, cluster *api.Cluster, channelConfig *channel.Config) (*awsAdapter, updatestrategy.UpdateStrategy, updatestrategy.NodePoolManager, error) {
	if cluster.Provider != providerID {
		return nil, nil, nil, ErrProviderNotSupported
	}

	logger.Infof("clusterpy: Prepare for provisioning cluster %s (%s)..", cluster.ID, cluster.LifecycleStatus)

	infrastructureAccount := strings.Split(cluster.InfrastructureAccount, ":")
	if len(infrastructureAccount) != 2 {
		return nil, nil, nil, fmt.Errorf("clusterpy: Unknown format for infrastructure account '%s", cluster.InfrastructureAccount)
	}

	if infrastructureAccount[0] != "aws" {
		return nil, nil, nil, fmt.Errorf("clusterpy: Cannot work with cloud provider '%s", infrastructureAccount[0])
	}

	roleArn := p.assumedRole
	if roleArn != "" {
		roleArn = fmt.Sprintf("arn:aws:iam::%s:role/%s", infrastructureAccount[1], p.assumedRole)
	}

	sess, err := awsUtils.Session(p.awsConfig, roleArn)
	if err != nil {
		return nil, nil, nil, err
	}

	adapter, err := newAWSAdapter(logger, cluster.APIServerURL, cluster.Region, sess, p.tokenSource, p.dryRun)
	if err != nil {
		return nil, nil, nil, err
	}

	err = p.updateDefaults(cluster, channelConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to read configuration defaults: %v", err)
	}

	err = p.decryptConfigItems(cluster)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to decrypt config items: %v", err)
	}

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

	// allow clusters to override their drain settings
	handleDurationItem(cluster.ConfigItems, "drain_grace_period", func(v time.Duration) { drainConfig.ForceEvictionGracePeriod = v })
	handleDurationItem(cluster.ConfigItems, "drain_min_pod_lifetime", func(v time.Duration) { drainConfig.MinPodLifetime = v })
	handleDurationItem(cluster.ConfigItems, "drain_min_healthy_sibling_lifetime", func(v time.Duration) { drainConfig.MinHealthyPDBSiblingLifetime = v })
	handleDurationItem(cluster.ConfigItems, "drain_min_unhealthy_sibling_lifetime", func(v time.Duration) { drainConfig.MinUnhealthyPDBSiblingLifetime = v })
	handleDurationItem(cluster.ConfigItems, "drain_force_evict_interval", func(v time.Duration) { drainConfig.ForceEvictionInterval = v })
	handleDurationItem(cluster.ConfigItems, "drain_poll_interval", func(v time.Duration) { drainConfig.PollInterval = v })

	var updater updatestrategy.UpdateStrategy
	var poolManager updatestrategy.NodePoolManager
	switch updateStrategy {
	case updateStrategyRolling:
		client, err := kubernetes.NewKubeClientWithTokenSource(cluster.APIServerURL, p.tokenSource)
		if err != nil {
			return nil, nil, nil, err
		}

		// setup updater
		poolBackend := updatestrategy.NewASGNodePoolsBackend(cluster.ID, sess)

		poolManager = updatestrategy.NewKubernetesNodePoolManager(logger, client, poolBackend, drainConfig)

		updater = updatestrategy.NewRollingUpdateStrategy(logger, poolManager, 3)
	default:
		return nil, nil, nil, fmt.Errorf("unknown update strategy: %s", p.updateStrategy)
	}

	return adapter, updater, poolManager, nil
}

func handleDurationItem(configItems map[string]string, key string, set func(duration time.Duration)) error {
	if value, ok := configItems[key]; ok {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid value for %s: %v", key, err)
		}
		set(parsed)
	}
	return nil
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

// deleteClusterStacks deletes all stacks tagged by the cluster id.
func (p *clusterpyProvisioner) deleteClusterStacks(ctx context.Context, adapter *awsAdapter, cluster *api.Cluster) error {
	tags := map[string]string{
		tagNameKubernetesClusterPrefix + cluster.ID: resourceLifecycleOwned,
	}
	stacks, err := adapter.ListStacks(tags)
	if err != nil {
		return err
	}

	errorsc := make(chan error, len(stacks))

	for _, stack := range stacks {
		go func(stack cloudformation.Stack, errorsc chan error) {
			deleteStack := func() error {
				err := adapter.DeleteStack(ctx, aws.StringValue(stack.StackName))
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
func (p *clusterpyProvisioner) Deletions(logger *log.Entry, cluster *api.Cluster, deletions []*resource) error {
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

		out, err := command.Run(logger, cmd)
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

// parseDeletions reads and parses the deletions.yaml.
func parseDeletions(manifestsPath string) (*deletions, error) {
	file := path.Join(manifestsPath, deletionsFile)

	d, err := ioutil.ReadFile(file)
	if err != nil {
		// if the file doesn't exist we just treat it as if it was
		// empty.
		if os.IsNotExist(err) {
			return &deletions{}, nil
		}
		return nil, err
	}

	var deletions deletions
	err = yaml.Unmarshal(d, &deletions)
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

	return &deletions, nil
}

// apply calls kubectl apply for all the manifests in manifestsPath.
func (p *clusterpyProvisioner) apply(logger *log.Entry, cluster *api.Cluster, manifestsPath string) error {
	logger.Debugf("Checking for deletions.yaml")
	deletions, err := parseDeletions(manifestsPath)
	if err != nil {
		return err
	}

	logger.Debugf("Running PreApply deletions (%d)", len(deletions.PreApply))
	err = p.Deletions(logger, cluster, deletions.PreApply)
	if err != nil {
		return err
	}

	logger.Debugf("Starting Apply")

	//validating input
	if !strings.HasPrefix(cluster.InfrastructureAccount, "aws:") {
		return fmt.Errorf("Wrong format for string InfrastructureAccount: %s", cluster.InfrastructureAccount)
	}

	components, err := ioutil.ReadDir(manifestsPath)
	if err != nil {
		return errors.Wrapf(err, "cannot read directory")
	}

	token, err := p.tokenSource.Token()
	if err != nil {
		return errors.Wrapf(err, "no valid token")
	}

	applyContext := newTemplateContext(manifestsPath)

	for _, c := range components {
		// skip deletions.yaml if found
		if c.Name() == deletionsFile {
			continue
		}

		// we only apply yaml files
		if !c.IsDir() {
			continue
		}
		componentFolder := path.Join(manifestsPath, c.Name())
		files, err := ioutil.ReadDir(componentFolder)
		if err != nil {
			return errors.Wrapf(err, "cannot read directory")
		}

		for _, f := range files {
			// Workaround for CRD issue in Kubernetes <v1.8.4
			// https://github.bus.zalan.do/teapot/issues/issues/772
			// TODO: Remove after v1.8.4 is rolled out to all
			// clusters.
			allowFailure := f.Name() == "credentials.yaml"

			file := path.Join(componentFolder, f.Name())
			manifest, err := renderTemplate(applyContext, file, cluster)
			if err != nil {
				logger.Errorf("Error applying template %v", err)
			}

			// If there's no content we skip the file.
			if stripWhitespace(manifest) == "" {
				log.Debugf("Skipping empty file: %s", file)
				continue
			}

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
					cmd.Stdin = strings.NewReader(manifest)
					_, err := command.Run(logger, cmd)
					return err
				}
				err = backoff.Retry(applyManifest, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), maxApplyRetries))
				if err != nil && !allowFailure {
					return errors.Wrapf(err, "run kubectl failed")
				}
			}
		}
	}

	logger.Debugf("Running PostApply deletions (%d)", len(deletions.PostApply))
	err = p.Deletions(logger, cluster, deletions.PostApply)
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
