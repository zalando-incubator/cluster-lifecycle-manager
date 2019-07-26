package provisioner

import (
	"bytes"
	"compress/gzip"
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
	clcFileName           = "userdata.clc.yaml"
	cloudInitFileName     = "userdata.yaml"
	stackFileName         = "stack.yaml"
	filesTemplateName     = "files.yaml"
	nodePoolTagKeyLegacy  = "NodePool"
	nodePoolTagKey        = "kubernetes.io/node-pool"
	nodePoolRoleTagKey    = "kubernetes.io/role/node-pool"
	nodePoolProfileTagKey = "kubernetes.io/node-pool/profile"
	remoteFilesKMSKey     = "RemoteFilesEncryptionKey"
)

// NodePoolProvisioner is able to provision node pools for a cluster.
type NodePoolProvisioner interface {
	Provision(values map[string]string) error
	Reconcile() error
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
	nodePoolManager updatestrategy.NodePoolManager
	bucketName      string
	cfgBaseDir      string
	Cluster         *api.Cluster
	azInfo          *AZInfo
	templateContext *templateContext
	logger          *log.Entry
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

	templateCtx := p.templateContext.Copy(nodePool, values, p.templateContext.userData)
	renderedUserData, err := p.prepareUserData(templateCtx, nodePoolProfilesPath)
	if err != nil {
		return "", err
	}

	templateCtx = templateCtx.Copy(nodePool, values, renderedUserData)
	stackFilePath := path.Join(nodePoolProfilesPath, stackFileName)
	return renderTemplate(templateCtx, stackFilePath)
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

	nodePools := p.Cluster.NodePools
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

func supportsT2Unlimited(instanceTypes []string) bool {
	for _, instanceType := range instanceTypes {
		if !strings.HasPrefix(instanceType, "t2.") {
			return false
		}
	}
	return true
}

// provisionNodePool provisions a single node pool.
func (p *AWSNodePoolProvisioner) provisionNodePool(nodePool *api.NodePool, values map[string]interface{}) error {
	values["supports_t2_unlimited"] = supportsT2Unlimited(nodePool.InstanceTypes)

	instanceInfo, err := awsExt.SyntheticInstanceInfo(nodePool.InstanceTypes)
	if err != nil {
		return err
	}
	values["instance_info"] = instanceInfo

	// handle AZ overrides for the node pool
	if azNames, ok := nodePool.ConfigItems[availabilityZonesConfigItemKey]; ok {
		azInfo := p.azInfo.RestrictAZs(strings.Split(azNames, ","))
		values[subnetsValueKey] = azInfo.SubnetsByAZ()
		values[availabilityZonesValueKey] = azInfo.AvailabilityZones()
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
func (p *AWSNodePoolProvisioner) Reconcile(ctx context.Context, updater updatestrategy.UpdateStrategy) error {
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

		err := updater.PrepareForRemoval(ctx, nodePool)
		if err != nil {
			return err
		}

		// delete node pool stack
		err = p.awsAdapter.DeleteStack(ctx, stack)
		if err != nil {
			return err
		}
	}

	return nil
}

// prepareUserData provisions and returns the UserData for a given node pool path.
// It detects whether CloudInit or Container Linux Config (CLC) is used:
// * CloudInit is rendered and returned.
// * CLC is rendered, converted to Ignition, uploadeded to S3 and an Ignition file referencing the
// uploaded file is returned.
func (p *AWSNodePoolProvisioner) prepareUserData(templateCtx *templateContext, nodePoolProfilesPath string) (string, error) {
	clcPath := path.Join(nodePoolProfilesPath, clcFileName)
	if _, err := os.Stat(clcPath); err == nil {
		return p.prepareCLC(templateCtx, clcPath)
	}

	cloudInitPath := path.Join(nodePoolProfilesPath, cloudInitFileName)
	if _, err := os.Stat(cloudInitPath); err == nil {
		return p.prepareCloudInit(nodePoolProfilesPath, templateCtx, cloudInitPath)
	}

	return "", fmt.Errorf("no userdata file at '%s' nor '%s' found", clcPath, cloudInitPath)
}

// prepareCloudInit prepares the user data by rendering the golang template.
// It also uploads the dynamically generated files needed for the nodes in the pool
// A EC2 UserData ready base64 string will be returned.
func (p *AWSNodePoolProvisioner) prepareCloudInit(nodePoolProfilesPath string, templateCtx *templateContext, cloudInitPath string) (string, error) {
	s3Path, err := p.renderUploadGeneratedFiles(templateCtx, nodePoolProfilesPath)
	if err != nil {
		return "", err
	}
	templateCtx.s3GeneratedFilesPath = s3Path
	p.logger.Debugf("Uploaded generated files to %s", s3Path)
	rendered, err := renderTemplate(templateCtx, cloudInitPath)
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

// prepareCLC prepares the user data by rendering the golang template
// and uploading the User Data to S3. A EC2 UserData ready base64 string will
// be returned.
func (p *AWSNodePoolProvisioner) prepareCLC(templateCtx *templateContext, clcPath string) (string, error) {
	rendered, err := renderTemplate(templateCtx, clcPath)
	if err != nil {
		return "", err
	}

	// convert to ignition
	ignCfg, err := clcToIgnition([]byte(rendered))
	if err != nil {
		return "", fmt.Errorf("failed to parse config %s: %v", clcPath, err)
	}
	userDataHash, err := generateDataHash(ignCfg)
	if err != nil {
		return "", fmt.Errorf("failed to generate hash of userdata: %v", err)
	}
	// upload to s3
	uri, err := p.uploadUserDataToS3(userDataHash, ignCfg, p.bucketName)
	if err != nil {
		return "", err
	}

	// create ignition config pulling from s3
	ignCfg = []byte(fmt.Sprintf(ignitionBaseTemplate, uri))

	return base64.StdEncoding.EncodeToString(ignCfg), nil
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

// uploadUserDataToS3 uploads the provided userData to the specified S3 bucket.
// The S3 object will be named by the sha512 hash of the data.
func (p *AWSNodePoolProvisioner) uploadUserDataToS3(userDataHash string, userData []byte, bucketName string) (string, error) {
	// Upload the stack template to S3
	_, err := p.awsAdapter.s3Uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(userDataHash),
		Body:   bytes.NewReader(userData),
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("s3://%s/%s", bucketName, userDataHash), nil
}

func getPKIKMSKey(adapter *awsAdapter, clusterID string) (string, error) {
	clusterStack, err := adapter.getStackByName(clusterID)
	if err != nil {
		return "", err
	}
	for _, o := range clusterStack.Outputs {
		if *o.OutputKey == remoteFilesKMSKey {
			return *o.OutputValue, nil
		}
	}
	return "", fmt.Errorf("failed to find the encryption key: %s", remoteFilesKMSKey)
}

// renderUploadGeneratedFiles renders a yaml file which is mapping of file names and it's contents. A gzipped tar archive of these
// files is then uploaded to S3 and the generated path is added to the template context.
func (p *AWSNodePoolProvisioner) renderUploadGeneratedFiles(templateCtx *templateContext, nodePoolProfilePath string) (string, error) {
	filesTemplatePath := path.Join(nodePoolProfilePath, filesTemplateName)
	filesRendered, err := renderTemplate(templateCtx, filesTemplatePath)
	if err != nil {
		return "", err
	}
	kmsKey, err := getPKIKMSKey(p.awsAdapter, p.Cluster.LocalID)
	if err != nil {
		return "", err
	}
	archive, err := makeArchive(filesRendered, kmsKey, p.awsAdapter.kmsClient)
	if err != nil {
		return "", err
	}
	userDataHash, err := generateDataHash([]byte(filesRendered))
	if err != nil {
		return "", fmt.Errorf("failed to generate hash of userdata: %v", err)
	}
	return p.uploadUserDataToS3(userDataHash, archive, p.bucketName)
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
