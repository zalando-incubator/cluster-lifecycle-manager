package provisioner

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
	"gopkg.in/yaml.v2"

	"golang.org/x/oauth2"

	"github.com/aws/aws-sdk-go/aws"
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
	versionFmt                     = "%s#%s"
	manifestsPath                  = "cluster/manifests"
	deletionsFile                  = "deletions.yaml"
	defaultNamespace               = "default"
	kubectlNotFound                = "(NotFound)"
	tagNameKubernetesClusterPrefix = "kubernetes.io/cluster/"
	resourceLifecycleShared        = "shared"
	maxApplyRetries                = 10
	configKeyUpdateStrategy        = "update_strategy"
	configKeyNodeMaxEvictTimeout   = "node_max_evict_timeout"
	updateStrategyRolling          = "rolling"
	defaultMaxRetryTime            = 5 * time.Minute
)

type clusterpyProvisioner struct {
	awsConfig      *aws.Config
	assumedRole    string
	dryRun         bool
	tokenSource    oauth2.TokenSource
	applyOnly      bool
	updateStrategy config.UpdateStrategy
	removeVolumes  bool
}

type applyContext struct {
	manifestData          map[string]string
	baseDir               string
	computingManifestHash bool
}

// NewClusterpyProvisioner returns a new ClusterPy provisioner by passing its location and and IAM role to use.
func NewClusterpyProvisioner(tokenSource oauth2.TokenSource, assumedRole string, awsConfig *aws.Config, options *Options) Provisioner {
	provisioner := &clusterpyProvisioner{
		awsConfig:   awsConfig,
		assumedRole: assumedRole,
		tokenSource: tokenSource,
	}

	if options != nil {
		provisioner.dryRun = options.DryRun
		provisioner.applyOnly = options.ApplyOnly
		provisioner.updateStrategy = options.UpdateStrategy
		provisioner.removeVolumes = options.RemoveVolumes
	}

	return provisioner
}

// Version returns the version derived from a sha1 hash of the cluster struct
// and the channel config version.
func (p *clusterpyProvisioner) Version(cluster *api.Cluster, channelConfig *channel.Config) (string, error) {
	if cluster.Provider != providerID {
		return "", ErrProviderNotSupported
	}

	state := new(bytes.Buffer)

	_, err := state.WriteString(cluster.ID)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.InfrastructureAccount)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.LocalID)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.APIServerURL)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.Channel)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.Environment)
	if err != nil {
		return "", err
	}
	err = binary.Write(state, binary.LittleEndian, cluster.CriticalityLevel)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.LifecycleStatus)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.Provider)
	if err != nil {
		return "", err
	}
	_, err = state.WriteString(cluster.Region)
	if err != nil {
		return "", err
	}

	// config items are sorted by key to produce a predictable string for
	// hashing.
	keys := make([]string, 0, len(cluster.ConfigItems))
	for key := range cluster.ConfigItems {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, err = state.WriteString(key)
		if err != nil {
			return "", err
		}
		_, err = state.WriteString(cluster.ConfigItems[key])
		if err != nil {
			return "", err
		}
	}

	// node pools
	for _, nodePool := range cluster.NodePools {
		_, err = state.WriteString(nodePool.Name)
		if err != nil {
			return "", err
		}
		_, err = state.WriteString(nodePool.Profile)
		if err != nil {
			return "", err
		}
		_, err = state.WriteString(nodePool.InstanceType)
		if err != nil {
			return "", err
		}
		_, err = state.WriteString(nodePool.DiscountStrategy)
		if err != nil {
			return "", err
		}
		err = binary.Write(state, binary.LittleEndian, nodePool.MinSize)
		if err != nil {
			return "", err
		}
		err = binary.Write(state, binary.LittleEndian, nodePool.MaxSize)
		if err != nil {
			return "", err
		}
	}

	// sha1 hash the cluster content
	hasher := sha1.New()
	_, err = hasher.Write(state.Bytes())
	if err != nil {
		return "", err
	}
	sha := base64.RawURLEncoding.EncodeToString(hasher.Sum(nil))

	return fmt.Sprintf(versionFmt, channelConfig.Version, sha), nil
}

// Provision provisions/updates a cluster on AWS. Provion is an idempotent
// operation for the same input.
func (p *clusterpyProvisioner) Provision(cluster *api.Cluster, channelConfig *channel.Config) error {
	logger := log.WithField("cluster", cluster.Alias)
	awsAdapter, updater, err := p.prepareProvision(logger, cluster, channelConfig)
	if err != nil {
		return err
	}

	// create etcd stack if needed.
	etcdStackDefinitionPath := path.Join(channelConfig.Path, "cluster", "etcd-cluster.yaml")

	err = awsAdapter.CreateOrUpdateEtcdStack("etcd-cluster-etcd", etcdStackDefinitionPath, cluster)
	if err != nil {
		return err
	}

	err = p.tagSubnets(awsAdapter, cluster)
	if err != nil {
		return err
	}

	stackDefinitionPath := path.Join(channelConfig.Path, "cluster", "senza-definition.yaml")

	// check if stack exists
	stack, err := awsAdapter.getStackByName(cluster.LocalID)
	if err != nil && !isDoesNotExistsErr(err) {
		return err
	}
	if stack != nil {
		// suspend scaling for all autoscaling worker groups
		for _, pool := range cluster.NodePools {
			asg, err := awsAdapter.getNodePoolASG(cluster.LocalID, pool.Name)
			if err != nil {
				return err
			}
			err = awsAdapter.suspendScaling(*asg.AutoScalingGroupName)
			if err != nil {
				return err
			}
			defer awsAdapter.resumeScaling(*asg.AutoScalingGroupName)
		}
	}

	out, err := awsAdapter.CreateOrUpdateClusterStack(cluster.LocalID, stackDefinitionPath, cluster)
	if err != nil {
		return err
	}
	cluster.Outputs = out

	// wait for API server to be ready
	err = waitForAPIServer(logger, cluster.APIServerURL, 15*time.Minute)
	if err != nil {
		return err
	}

	if !p.applyOnly {
		switch cluster.LifecycleStatus {
		case models.ClusterLifecycleStatusRequested, models.ClusterUpdateLifecycleStatusCreating:
			log.Warnf("New cluster (%s), skipping node pool update", cluster.LifecycleStatus)
		default:
			// update nodes
			sort.Sort(api.NodePools(cluster.NodePools))
			for _, nodePool := range cluster.NodePools {
				err := updater.Update(context.Background(), nodePool)
				if err != nil {
					return err
				}
			}
		}
	}

	return p.apply(logger, cluster, path.Join(channelConfig.Path, manifestsPath))
}

// Decommission decommissions a cluster provisioned in AWS.
func (p *clusterpyProvisioner) Decommission(cluster *api.Cluster, channelConfig *channel.Config) error {
	logger := log.WithField("cluster", cluster.Alias)
	awsAdapter, _, err := p.prepareProvision(logger, cluster, channelConfig)
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
		backoff.WithMaxTries(backoff.NewConstantBackOff(10*time.Second), 5))
	if err != nil {
		logger.Error("Unable to downscale the deployments, proceeding anyway: %s", err)
	}

	// delete all cluster infrastructure stacks
	err = p.deleteClusterStacks(awsAdapter, cluster)
	if err != nil {
		return err
	}

	// delete the main cluster stack
	err = awsAdapter.DeleteStack(cluster.LocalID)
	if err != nil {
		return err
	}

	err = p.untagSubnets(awsAdapter, cluster)
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
func (p *clusterpyProvisioner) prepareProvision(logger *log.Entry, cluster *api.Cluster, channelConfig *channel.Config) (*awsAdapter, updatestrategy.UpdateStrategy, error) {
	if cluster.Provider != providerID {
		return nil, nil, ErrProviderNotSupported
	}

	logger.Infof("clusterpy: Prepare for provisioning cluster %s (%s)..", cluster.ID, cluster.LifecycleStatus)

	infrastructureAccount := strings.Split(cluster.InfrastructureAccount, ":")
	if len(infrastructureAccount) != 2 {
		return nil, nil, fmt.Errorf("clusterpy: Unknown format for infrastructure account '%s", cluster.InfrastructureAccount)
	}

	if infrastructureAccount[0] != "aws" {
		return nil, nil, fmt.Errorf("clusterpy: Cannot work with cloud provider '%s", infrastructureAccount[0])
	}

	roleArn := p.assumedRole
	if roleArn != "" {
		roleArn = fmt.Sprintf("arn:aws:iam::%s:role/%s", infrastructureAccount[1], p.assumedRole)
	}

	sess, err := awsUtils.Session(p.awsConfig, roleArn)
	if err != nil {
		return nil, nil, err
	}

	adapter, err := newAWSAdapter(logger, cluster.APIServerURL, cluster.Region, sess, p.tokenSource, p.dryRun)
	if err != nil {
		return nil, nil, err
	}

	// allow clusters to override their update strategy.
	// use global update strategy if cluster doesn't define one.
	updateStrategy, ok := cluster.ConfigItems[configKeyUpdateStrategy]
	if !ok {
		updateStrategy = p.updateStrategy.Strategy
	}

	// allow clusters to override their max evict timeout
	// use global max evict timeout if cluster doesn't define one.
	maxEvictTimeout := p.updateStrategy.MaxEvictTimeout

	maxEvictTimeoutStr, ok := cluster.ConfigItems[configKeyNodeMaxEvictTimeout]
	if ok {
		maxEvictTimeout, err = time.ParseDuration(maxEvictTimeoutStr)
		if err != nil {
			return nil, nil, err
		}
	}

	var updater updatestrategy.UpdateStrategy
	switch updateStrategy {
	case updateStrategyRolling:
		client, err := kubernetes.NewKubeClientWithTokenSource(cluster.APIServerURL, p.tokenSource)
		if err != nil {
			return nil, nil, err
		}

		// setup updater
		poolBackend := updatestrategy.NewASGNodePoolsBackend(cluster.ID, sess)

		poolManager := updatestrategy.NewKubernetesNodePoolManager(logger, client, poolBackend, maxEvictTimeout)

		updater = updatestrategy.NewRollingUpdateStrategy(logger, poolManager, 3)
	default:
		return nil, nil, fmt.Errorf("unknown update strategy: %s", p.updateStrategy)
	}

	return adapter, updater, nil
}

// tagSubnets tags all subnets in the default VPC with the kubernetes cluster
// id tag.
func (p *clusterpyProvisioner) tagSubnets(awsAdapter *awsAdapter, cluster *api.Cluster) error {
	subnets, err := awsAdapter.GetSubnets()
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
func (p *clusterpyProvisioner) untagSubnets(awsAdapter *awsAdapter, cluster *api.Cluster) error {
	subnets, err := awsAdapter.GetSubnets()
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
func (p *clusterpyProvisioner) deleteClusterStacks(adapter *awsAdapter, cluster *api.Cluster) error {
	tags := map[string]string{
		"kubernetes.io/cluster/" + cluster.ID: "owned",
	}
	stacks, err := adapter.ListStacks(tags)
	if err != nil {
		return err
	}

	for _, stack := range stacks {
		deleteStack := func() error {
			err := adapter.DeleteStack(aws.StringValue(stack.StackName))
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
			return err
		}
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

// getNodePools returns the master and worker node pool for a cluster.
func getNodePools(cluster *api.Cluster) (*api.NodePool, *api.NodePool, error) {
	masterPools := make([]*api.NodePool, 0)
	workerPools := make([]*api.NodePool, 0)

	for _, np := range cluster.NodePools {
		if strings.HasPrefix(np.Profile, "master") {
			masterPools = append(masterPools, np)
		}
		if strings.HasPrefix(np.Profile, "worker") {
			workerPools = append(workerPools, np)
		}
	}

	if len(masterPools) != 1 {
		return nil, nil, fmt.Errorf("clusterpy: Unsupported number of master node pools for cluster '%s'. Should be 1 but is %d", cluster.ID, len(masterPools))
	}

	if len(workerPools) != 1 {
		return nil, nil, fmt.Errorf("clusterpy: Unsupported number of worker node pools for cluster '%s'. Should be 1 but is %d", cluster.ID, len(workerPools))
	}

	return masterPools[0], workerPools[0], nil
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

		err = command.Run(logger, cmd)
		if err != nil {
			// if kubectl failed because the resource didn't
			// exists, we don't treat it as an error since the
			// resource was already deleted.
			// We can only check this by inspecting the content of
			// Stderr (which is provided in the err).
			if strings.Contains(err.Error(), kubectlNotFound) {
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

func newApplyContext(baseDir string) *applyContext {
	return &applyContext{
		baseDir:      baseDir,
		manifestData: make(map[string]string),
	}
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

	applyContext := newApplyContext(manifestsPath)

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
			manifest, err := applyTemplate(applyContext, file, cluster)
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
					return command.Run(logger, cmd)
				}
				err = backoff.Retry(applyManifest, backoff.WithMaxTries(backoff.NewExponentialBackOff(), maxApplyRetries))
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

// getAWSAccountID is an utility function for the gotemplate that will remove
// the prefix "aws" from the infrastructure ID.
// TODO: get the real AWS account ID from the `external_id` field of the
// infrastructure account in the cluster registry.
func getAWSAccountID(ia string) string {
	return strings.Split(ia, ":")[1]
}

// base64Encode base64 encodes a string.
func base64Encode(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

func (context *applyContext) resetComputingManifestHash() {
	context.computingManifestHash = false
}

// manifestHash is a function for the templates that will return a hash of an interpolated sibling template
// file. returns an error if computing manifestHash calls manifestHash again, if interpolation of that template
// returns an error, or if the path is outside of the manifests folder.
func manifestHash(context *applyContext, file string, template string, cluster *api.Cluster) (string, error) {
	if context.computingManifestHash {
		return "", fmt.Errorf("manifestHash is not reentrant")
	}
	context.computingManifestHash = true
	defer context.resetComputingManifestHash()

	templateFile, err := filepath.Abs(path.Clean(path.Join(path.Dir(file), template)))
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(templateFile, context.baseDir) {
		return "", fmt.Errorf("invalid template path: %s", templateFile)
	}

	templateData, ok := context.manifestData[templateFile]
	if !ok {
		applied, err := applyTemplate(context, templateFile, cluster)
		if err != nil {
			return "", err
		}
		templateData = applied
	}

	return fmt.Sprintf("%x", sha256.Sum256([]byte(templateData))), nil
}

// applyTemplate takes a fileName of a template and the model to apply to it.
// returns the transformed template or an error if not successful
func applyTemplate(context *applyContext, file string, cluster *api.Cluster) (string, error) {
	funcMap := template.FuncMap{
		"getAWSAccountID": getAWSAccountID,
		"base64":          base64Encode,
		"manifestHash":    func(template string) (string, error) { return manifestHash(context, file, template, cluster) },
	}

	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	content, err := ioutil.ReadFile(f.Name())
	if err != nil {
		return "", err
	}
	t, err := template.New(f.Name()).Option("missingkey=error").Funcs(funcMap).Parse(string(content))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = t.Execute(&out, cluster)
	if err != nil {
		return "", err
	}

	templateData := out.String()
	context.manifestData[file] = templateData

	return templateData, nil
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
