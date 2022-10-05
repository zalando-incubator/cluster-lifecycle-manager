package provisioner

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/cenkalti/backoff"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	awsUtils "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/updatestrategy"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"golang.org/x/oauth2"
)

const (
	cloudInitFileName     = "userdata.yaml"
	stackFileName         = "stack.yaml"
	provisionersFileName  = "provisioners.yaml"
	filesTemplateName     = "files.yaml"
	nodePoolTagKeyLegacy  = "NodePool"
	nodePoolTagKey        = "kubernetes.io/node-pool"
	nodePoolRoleTagKey    = "kubernetes.io/role/node-pool"
	nodePoolProfileTagKey = "kubernetes.io/node-pool/profile"

	masterFilesKMSKey = "MasterFilesEncryptionKey"
	workerFilesKMSKey = "WorkerFilesEncryptionKey"

	userDataValuesKey             = "UserData"
	s3GeneratedFilesPathValuesKey = "S3GeneratedFilesPath"
	instanceInfoKey               = "InstanceInfo"
)

// NodePoolProvisioner is able to provision node pools for a cluster.
type NodePoolProvisioner interface {
	Provision(ctx context.Context, nodePools []*api.NodePool, values map[string]interface{}) error
	Reconcile(ctx context.Context, updater updatestrategy.UpdateStrategy) error
}

type remoteData struct {
	Files []struct {
		Path        string `yaml:"path" json:"path"`
		Data        string `yaml:"data" json:"data"`
		Encrypted   bool   `yaml:"encrypted" json:"encrypted"`
		Permissions int64  `yaml:"permissions" json:"permissions"`
	} `yaml:"files" json:"files"`
}

// AWSNodePoolProvisioner is a node provisioner able to provision node pools
// in AWS via cloudformation.
// TODO: move AWS specific implementation to a separate file/package.
type AWSNodePoolProvisioner struct {
	awsAdapter      *awsAdapter
	execManager     *command.ExecManager
	instanceTypes   *awsUtils.InstanceTypes
	nodePoolManager updatestrategy.NodePoolManager
	bucketName      string
	config          channel.Config
	cluster         *api.Cluster
	tokenSource     oauth2.TokenSource
	azInfo          *AZInfo
	templateContext *templateContext
	logger          *log.Entry
}

func (p *AWSNodePoolProvisioner) generateNodePoolTemplate(nodePool *api.NodePool, values map[string]interface{}, templateFileName string) (string, error) {
	var renderedUserData string
	var err error

	if nodePool.IsKarpenter() {
		renderedUserData, err = p.renderCloudInit(nodePool, values)
	} else {
		renderedUserData, err = p.prepareCloudInit(nodePool, values)
	}
	if err != nil {
		return "", err
	}

	values[userDataValuesKey] = renderedUserData

	if templateFileName == "" {
		templateFileName = stackFileName
	}
	stackManifest, err := p.config.NodePoolManifest(nodePool.Profile, templateFileName)
	if err != nil {
		return "", err
	}
	return renderSingleTemplate(stackManifest, p.cluster, nodePool, values, p.awsAdapter)
}

// Provision provisions node pools of the cluster.
func (p *AWSNodePoolProvisioner) Provision(ctx context.Context, nodePools []*api.NodePool, values map[string]interface{}) error {
	// create S3 bucket if it doesn't exist
	// the bucket is used for storing the ignition userdata for the node
	// pools.
	err := p.awsAdapter.createS3Bucket(p.bucketName)
	if err != nil {
		return err
	}

	errorsc := make(chan error, len(nodePools))

	// provision node pools in parallel
	for _, nodePool := range nodePools {
		poolValues := util.CopyValues(values)

		go func(nodePool api.NodePool, errorsc chan error) {
			err := p.provisionNodePool(ctx, &nodePool, poolValues)
			if err != nil {
				err = fmt.Errorf("failed to provision node pool %s: %s", nodePool.Name, err)
			}
			errorsc <- err
		}(*nodePool, errorsc)
	}

	errorStrs := make([]string, 0, len(nodePools))
	for i := 0; i < len(nodePools); i++ {
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

func supportsT2Unlimited(instanceTypes []string) bool {
	for _, instanceType := range instanceTypes {
		if !strings.HasPrefix(instanceType, "t2.") {
			return false
		}
	}
	return true
}

// provisionNodePool provisions a single node pool.
func (p *AWSNodePoolProvisioner) provisionNodePool(ctx context.Context, nodePool *api.NodePool, values map[string]interface{}) error {
	values["supports_t2_unlimited"] = supportsT2Unlimited(nodePool.InstanceTypes)

	instanceInfo, err := p.instanceTypes.SyntheticInstanceInfo(nodePool.InstanceTypes)
	if err != nil {
		return err
	}
	values[instanceInfoKey] = instanceInfo

	// handle AZ overrides for the node pool
	if azNames, ok := nodePool.ConfigItems[availabilityZonesConfigItemKey]; ok {
		azInfo := p.azInfo.RestrictAZs(strings.Split(azNames, ","))
		values[subnetsValueKey] = azInfo.SubnetsByAZ()
		values[availabilityZonesValueKey] = azInfo.AvailabilityZones()
	}

	template, err := p.generateNodePoolTemplate(nodePool, values, stackFileName)
	if err != nil {
		return err
	}

	// TODO: stackname pattern
	stackName := fmt.Sprintf("nodepool-%s-%s", nodePool.Name, strings.Replace(p.cluster.ID, ":", "-", -1))

	tags := map[string]string{
		tagNameKubernetesClusterPrefix + p.cluster.ID: resourceLifecycleOwned,
		nodePoolRoleTagKey:                            "true",
		nodePoolTagKey:                                nodePool.Name,
		nodePoolTagKeyLegacy:                          nodePool.Name,
		nodePoolProfileTagKey:                         nodePool.Profile,
	}

	err = p.awsAdapter.applyStack(stackName, template, "", tags, true, nil)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, maxWaitTimeout)
	defer cancel()
	err = p.awsAdapter.waitForStack(ctx, waitTime, stackName)
	if err != nil {
		return err
	}
	if nodePool.IsKarpenter() {
		template, err = p.generateNodePoolTemplate(nodePool, values, provisionersFileName)
		if err != nil {
			return err
		}
		token, err := p.tokenSource.Token()
		if err != nil {
			return err
		}

		args := []string{
			"kubectl",
			"apply",
			fmt.Sprintf("--server=%s", p.cluster.APIServerURL),
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

		p.logger.Debug(newApplyCommand())
		applyManifest := func() error {
			cmd := newApplyCommand()
			cmd.Stdin = strings.NewReader(template)
			_, err := p.execManager.Run(ctx, p.logger, cmd)
			return err
		}
		err = backoff.Retry(applyManifest, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), maxApplyRetries))
		if err != nil {
			return errors.Wrapf(err, "kubectl apply failed for node pool %s", nodePool.Name)
		}
	}

	return nil
}

// Reconcile finds all orphaned node pool stacks and decommission the node
// pools by scaling them down gracefully and deleting the corresponding stacks.
func (p *AWSNodePoolProvisioner) Reconcile(ctx context.Context, updater updatestrategy.UpdateStrategy) error {
	// decommission orphaned node pools
	tags := map[string]string{
		tagNameKubernetesClusterPrefix + p.cluster.ID: resourceLifecycleOwned,
		nodePoolRoleTagKey:                            "true",
	}

	nodePoolStacks, err := p.awsAdapter.ListStacks(tags, nil)
	if err != nil {
		return err
	}

	// find orphaned by comparing node pool stacks to node pools defined for cluster
	orphaned := orphanedNodePoolStacks(nodePoolStacks, p.cluster.NodePools)

	if len(orphaned) > 0 {
		p.logger.Infof("Found %d node pool stacks to decommission", len(orphaned))
	}

	for _, stack := range orphaned {
		nodePool := nodePoolStackToNodePool(stack)

		if nodePool.IsKarpenter() {
			token, err := p.tokenSource.Token()
			if err != nil {
				return err
			}
			args := []string{
				"kubectl",
				"delete",
				fmt.Sprintf("--server=%s", p.cluster.APIServerURL),
				fmt.Sprintf("--token=%s", token.AccessToken),
				fmt.Sprintf("awsnodetemplate.karpenter.k8s.aws/%s-template", nodePool.Name),
				fmt.Sprintf("provisioner.karpenter.sh/%s", nodePool.Name),
			}

			newApplyCommand := func() *exec.Cmd {
				cmd := exec.Command(args[0], args[1:]...)
				// prevent kubectl to find the in-cluster config
				cmd.Env = []string{}
				return cmd
			}

			p.logger.Debug(newApplyCommand())
			deleteResources := func() error {
				cmd := newApplyCommand()
				_, err := p.execManager.Run(ctx, p.logger, cmd)
				return err
			}
			// TODO: handle the NotFound error
			err = backoff.Retry(deleteResources, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), maxApplyRetries))
			if err != nil {
				return errors.Wrapf(err, "kubectl delete failed for node pool %s", nodePool.Name)
			}

			// TODO: not sure if we need this, karpenter controller should be able to clean them up when the provisioner is deleted
			nodePoolBackend := updatestrategy.NewEC2NodePoolBackend(p.cluster.ID, p.awsAdapter.session)
			err = nodePoolBackend.Decommission(ctx, nodePool)
			if err != nil {
				return err
			}
		} else {
			err := updater.PrepareForRemoval(ctx, nodePool)
			if err != nil {
				return err
			}
		}

		// delete node pool stack
		err = p.awsAdapter.DeleteStack(ctx, stack)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *AWSNodePoolProvisioner) renderCloudInit(nodePool *api.NodePool, values map[string]interface{}) (string, error) {
	var (
		keyName, poolKind string
	)
	if nodePool.IsMaster() {
		keyName = masterFilesKMSKey
		poolKind = "master"
	} else {
		keyName = workerFilesKMSKey
		poolKind = "worker"
	}

	kmsKey, err := getPKIKMSKey(p.awsAdapter, p.cluster.LocalID, keyName)
	if err != nil {
		return "", err
	}

	renderer := &FilesRenderer{
		awsAdapter: p.awsAdapter,
		cluster:    p.cluster,
		config:     p.config,
		directory:  poolKind,
		nodePool:   nodePool,
	}

	s3Path, err := renderer.RenderAndUploadFiles(values, p.bucketName, kmsKey)
	if err != nil {
		return "", err
	}

	p.logger.Debugf("Uploaded generated files to %s", s3Path)
	values[s3GeneratedFilesPathValuesKey] = s3Path

	cloudInitContents, err := p.config.NodePoolManifest(nodePool.Profile, cloudInitFileName)
	if err != nil {
		return "", err
	}
	rendered, err := renderSingleTemplate(cloudInitContents, p.cluster, nodePool, values, p.awsAdapter)
	if err != nil {
		return "", err
	}
	return rendered, nil
}

// prepareCloudInit prepares the user data by rendering the golang template.
// It also uploads the dynamically generated files needed for the nodes in the pool
// A EC2 UserData ready base64 string will be returned.
func (p *AWSNodePoolProvisioner) prepareCloudInit(nodePool *api.NodePool, values map[string]interface{}) (string, error) {
	rendered, err := p.renderCloudInit(nodePool, values)
	if err != nil {
		return "", err
	}
	var gzipBuffer bytes.Buffer
	writer := gzip.NewWriter(&gzipBuffer)
	defer writer.Close()
	_, err = writer.Write([]byte(rendered))
	if err != nil {
		return "", err
	}
	err = writer.Close()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gzipBuffer.Bytes()), nil
}

func generateDataHash(userData []byte) (string, error) {
	// hash the userData to use as object name
	hasher := sha512.New()
	_, err := hasher.Write(userData)
	if err != nil {
		return "", err
	}
	sha := hex.EncodeToString(hasher.Sum(nil))
	return fmt.Sprintf("%s.userdata", sha), nil
}

func getPKIKMSKey(adapter *awsAdapter, clusterID string, keyName string) (string, error) {
	clusterStack, err := adapter.getStackByName(clusterID)
	if err != nil {
		return "", err
	}
	for _, o := range clusterStack.Outputs {
		if *o.OutputKey == keyName {
			return *o.OutputValue, nil
		}
	}
	return "", fmt.Errorf("failed to find the encryption key: %s", keyName)
}

func orphanedNodePoolStacks(nodePoolStacks []*cloudformation.Stack, nodePools []*api.NodePool) []*cloudformation.Stack {
	orphaned := make([]*cloudformation.Stack, 0, len(nodePoolStacks))
	for _, stack := range nodePoolStacks {
		np := nodePoolStackToNodePool(stack)
		if !inNodePoolList(np, nodePools) {
			orphaned = append(orphaned, stack)
		}
	}
	return orphaned
}

func inNodePoolList(nodePool *api.NodePool, nodePools []*api.NodePool) bool {
	for _, np := range nodePools {
		if np.Name == nodePool.Name {
			return true
		}
	}
	return false
}

func nodePoolStackToNodePool(stack *cloudformation.Stack) *api.NodePool {
	nodePool := &api.NodePool{}

	for _, tag := range stack.Tags {
		if aws.StringValue(tag.Key) == nodePoolTagKey {
			nodePool.Name = aws.StringValue(tag.Value)
		}

		if aws.StringValue(tag.Key) == nodePoolProfileTagKey {
			nodePool.Profile = aws.StringValue(tag.Value)
		}
	}
	return nodePool
}
