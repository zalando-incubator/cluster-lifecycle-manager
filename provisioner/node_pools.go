package provisioner

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/mitchellh/copystructure"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	awsExt "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/updatestrategy"
)

const (
	userDataFileName      = "userdata.clc.yaml"
	stackFileName         = "stack.yaml"
	nodePoolTagKeyLegacy  = "NodePool"
	nodePoolTagKey        = "kubernetes.io/node-pool"
	nodePoolRoleTagKey    = "kubernetes.io/role/node-pool"
	nodePoolProfileTagKey = "kubernetes.io/node-pool/profile"
)

// NodePoolProvisioner is able to provision node pools for a cluster.
type NodePoolProvisioner interface {
	Provision(values map[string]string) error
	Reconcile() error
}

// AWSNodePoolProvisioner is a node provisioner able to provision node pools
// in AWS via cloudformation.
// TODO: move AWS specific implementation to a separate file/package.
type AWSNodePoolProvisioner struct {
	awsAdapter      *awsAdapter
	nodePoolManager updatestrategy.NodePoolManager
	bucketName      string
	cfgBaseDir      string
	Cluster         *api.Cluster
	logger          *log.Entry
}

// stackParams defined the parameters expected by a node pool stack template.
type stackParams struct {
	Cluster  *api.Cluster
	NodePool *api.NodePool
	UserData string
	Values   map[string]interface{}
}

type userDataParams struct {
	Cluster  *api.Cluster
	NodePool *api.NodePool
	Values   map[string]interface{}
}

func (p *AWSNodePoolProvisioner) generateNodePoolStackTemplate(nodePool *api.NodePool, values map[string]interface{}) (string, error) {
	nodePoolProfilesPath := path.Join(p.cfgBaseDir, nodePool.Profile)
	fi, err := os.Stat(nodePoolProfilesPath)
	if err != nil {
		return "", err
	}

	if !fi.IsDir() {
		return "", fmt.Errorf("failed to find configuration for node pool profile '%s'", nodePool.Profile)
	}

	userDataParams := &userDataParams{
		Cluster:  p.Cluster,
		NodePool: nodePool,
		Values:   values,
	}

	userDataPath := path.Join(nodePoolProfilesPath, userDataFileName)
	renderedUserData, err := p.prepareUserData(nodePoolProfilesPath, userDataPath, userDataParams)
	if err != nil {
		return "", err
	}

	params := &stackParams{
		Cluster:  p.Cluster,
		NodePool: nodePool,
		UserData: renderedUserData,
		Values:   values,
	}

	stackFilePath := path.Join(nodePoolProfilesPath, stackFileName)
	return renderTemplate(newTemplateContext(nodePoolProfilesPath), stackFilePath, params)
}

// Provision provisions node pools of the cluster.
func (p *AWSNodePoolProvisioner) Provision(values map[string]interface{}) error {
	// create S3 bucket if it doesn't exist
	// the bucket is used for storing the ignition userdata for the node
	// pools.
	err := p.awsAdapter.createS3Bucket(p.bucketName)
	if err != nil {
		return err
	}

	// TODO(tech-depth): remove non-legacy node pool filter
	nodePools := getNonLegacyNodePools(p.Cluster)
	errorsc := make(chan error, len(nodePools))

	// provision node pools in parallel
	for _, nodePool := range nodePools {
		poolValuesCopy, err := copystructure.Copy(values)
		if err != nil {
			return err
		}

		poolValues, ok := poolValuesCopy.(map[string]interface{})
		if !ok {
			return fmt.Errorf("unable to copy values for node pool %s", nodePool.Name)
		}

		go func(nodePool api.NodePool, errorsc chan error) {
			err := p.provisionNodePool(&nodePool, poolValues)
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

// provisionNodePool provisions a single node pool.
func (p *AWSNodePoolProvisioner) provisionNodePool(nodePool *api.NodePool, values map[string]interface{}) error {
	values["supports_t2_unlimited"] = strings.HasPrefix(nodePool.InstanceType, "t2")
	values["spot_price"] = ""

	switch nodePool.DiscountStrategy {
	case discountStrategyNone:
		break
	case discountStrategySpotMaxPrice:
		instanceInfo, err := awsExt.InstanceInfo(nodePool.InstanceType)
		if err != nil {
			return err
		}

		onDemandPrice, ok := instanceInfo.Pricing[p.Cluster.Region]
		if !ok {
			return fmt.Errorf("no price data for region %s, instance type %s", p.Cluster.Region, nodePool.InstanceType)
		}

		values["spot_price"] = onDemandPrice
	default:
		return fmt.Errorf("unsupported node pool discount_strategy %s", nodePool.DiscountStrategy)
	}

	template, err := p.generateNodePoolStackTemplate(nodePool, values)
	if err != nil {
		return err
	}

	// TODO: stackname pattern
	stackName := fmt.Sprintf("nodepool-%s-%s", nodePool.Name, strings.Replace(p.Cluster.ID, ":", "-", -1))

	tags := []*cloudformation.Tag{
		{
			Key:   aws.String(tagNameKubernetesClusterPrefix + p.Cluster.ID),
			Value: aws.String(resourceLifecycleOwned),
		},
		{
			Key:   aws.String(nodePoolRoleTagKey),
			Value: aws.String("true"),
		},
		{
			Key:   aws.String(nodePoolTagKey),
			Value: aws.String(nodePool.Name),
		},
		{
			Key:   aws.String(nodePoolTagKeyLegacy),
			Value: aws.String(nodePool.Name),
		},
		{
			Key:   aws.String(nodePoolProfileTagKey),
			Value: aws.String(nodePool.Profile),
		},
	}

	err = p.awsAdapter.applyStack(stackName, template, "", tags, true)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), maxWaitTimeout)
	defer cancel()
	err = p.awsAdapter.waitForStack(ctx, waitTime, stackName)
	if err != nil {
		return err
	}

	return nil
}

// Reconcile finds all orphaned node pool stacks and decommission the node
// pools by scaling them down gracefully and deleting the corresponding stacks.
func (p *AWSNodePoolProvisioner) Reconcile(ctx context.Context) error {
	// decommission orphaned node pools
	tags := map[string]string{
		tagNameKubernetesClusterPrefix + p.Cluster.ID: resourceLifecycleOwned,
		nodePoolRoleTagKey:                            "true",
	}

	nodePoolStacks, err := p.awsAdapter.ListStacks(tags)
	if err != nil {
		return err
	}

	// find orphaned by comparing node pool stacks to node pools defined for cluster
	orphaned := orphanedNodePoolStacks(nodePoolStacks, p.Cluster.NodePools)

	if len(orphaned) > 0 {
		p.logger.Infof("Found %d node pool stacks to decommission", len(orphaned))
	}

	for _, stack := range orphaned {
		nodePool := nodePoolStackToNodePool(stack)

		// gracefully downscale node pool
		err := p.nodePoolManager.ScalePool(ctx, nodePool, 0)
		if err != nil {
			return err
		}

		// delete node pool stack
		err = p.awsAdapter.DeleteStack(ctx, aws.StringValue(stack.StackName))
		if err != nil {
			return err
		}
	}

	return nil
}

// prepareUserData prepares the user data by rendering the golang template
// and uploading the User Data to S3. A EC2 UserData ready base64 string will
// be returned.
func (p *AWSNodePoolProvisioner) prepareUserData(basedir, clcPath string, config interface{}) (string, error) {
	rendered, err := renderTemplate(newTemplateContext(basedir), clcPath, config)
	if err != nil {
		return "", err
	}

	// convert to ignition
	ignCfg, err := clcToIgnition([]byte(rendered))
	if err != nil {
		return "", fmt.Errorf("failed to parse config %s: %v", clcPath, err)
	}

	// upload to s3
	uri, err := p.uploadUserDataToS3(ignCfg, p.bucketName)
	if err != nil {
		return "", err
	}

	// create ignition config pulling from s3
	ignCfg = []byte(fmt.Sprintf(ignitionBaseTemplate, uri))

	return base64.StdEncoding.EncodeToString(ignCfg), nil
}

// uploadUserDataToS3 uploads the provided userData to the specified S3 bucket.
// The S3 object will be named by the sha512 hash of the data.
func (p *AWSNodePoolProvisioner) uploadUserDataToS3(userData []byte, bucketName string) (string, error) {
	// hash the userData to use as object name
	hasher := sha512.New()
	_, err := hasher.Write(userData)
	if err != nil {
		return "", err
	}
	sha := hex.EncodeToString(hasher.Sum(nil))

	objectName := fmt.Sprintf("%s.userdata", sha)

	// Upload the stack template to S3
	_, err = p.awsAdapter.s3Uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectName),
		Body:   bytes.NewReader(userData),
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("s3://%s/%s", bucketName, objectName), nil
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
