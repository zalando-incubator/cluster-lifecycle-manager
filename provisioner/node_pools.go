package provisioner

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	awsUtils "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/updatestrategy"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"golang.org/x/oauth2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type NodePoolTemplateRenderer struct {
	awsAdapter     *awsAdapter
	config         channel.Config
	cluster        *api.Cluster
	bucketName     string
	logger         *log.Entry
	encodeUserData bool
	instanceTypes  *awsUtils.InstanceTypes
}

// prepareCloudInit prepares the user data by rendering the golang template.
// It also uploads the dynamically generated files needed for the nodes in the pool
// A EC2 UserData ready base64 string will be returned.
func (r *NodePoolTemplateRenderer) prepareCloudInit(nodePool *api.NodePool, values map[string]interface{}) (string, error) {
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

	kmsKey, err := getPKIKMSKey(r.awsAdapter, r.cluster.LocalID, keyName)
	if err != nil {
		return "", err
	}

	renderer := &FilesRenderer{
		awsAdapter:    r.awsAdapter,
		cluster:       r.cluster,
		config:        r.config,
		directory:     poolKind,
		nodePool:      nodePool,
		instanceTypes: r.instanceTypes,
	}

	s3Path, err := renderer.RenderAndUploadFiles(values, r.bucketName, kmsKey)
	if err != nil {
		return "", err
	}

	r.logger.Debugf("Uploaded generated files to %s", s3Path)
	values[s3GeneratedFilesPathValuesKey] = s3Path

	cloudInitContents, err := r.config.NodePoolManifest(nodePool.Profile, cloudInitFileName)
	if err != nil {
		return "", err
	}
	rendered, err := renderSingleTemplate(cloudInitContents, r.cluster, nodePool, values, r.awsAdapter, r.instanceTypes)
	if err != nil {
		return "", err
	}

	if !r.encodeUserData {
		return rendered, nil
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

func (r *NodePoolTemplateRenderer) generateNodePoolTemplate(nodePool *api.NodePool, values map[string]interface{}, templateFileName string) (string, error) {
	var renderedUserData string
	var err error

	renderedUserData, err = r.prepareCloudInit(nodePool, values)
	if err != nil {
		return "", err
	}

	values[userDataValuesKey] = renderedUserData

	if templateFileName == "" {
		templateFileName = stackFileName
	}
	stackManifest, err := r.config.NodePoolManifest(nodePool.Profile, templateFileName)
	if err != nil {
		return "", err
	}
	return renderSingleTemplate(stackManifest, r.cluster, nodePool, values, r.awsAdapter, r.instanceTypes)
}

type KarpenterNodePoolProvisioner struct {
	NodePoolTemplateRenderer
	*kubernetes.KubeCTLRunner
	k8sClients *kubernetes.ClientsCollection
}

func NewKarpenterNodePoolProvisioner(
	n NodePoolTemplateRenderer,
	e *command.ExecManager,
	ts oauth2.TokenSource,
	options *HookResponse,
) (*KarpenterNodePoolProvisioner, error) {
	var caData []byte
	if options != nil && len(options.CAData) > 0 {
		caData = options.CAData
	}
	c, err := kubernetes.NewClientsCollection(n.cluster.APIServerURL, ts, caData)
	if err != nil {
		return nil, err
	}
	k := kubernetes.NewKubeCTLRunner(
		e,
		ts,
		n.logger,
		n.cluster.APIServerURL,
		maxApplyRetries,
		caData,
	)
	return &KarpenterNodePoolProvisioner{
		NodePoolTemplateRenderer: n,
		k8sClients:               c,
		KubeCTLRunner:            k,
	}, nil
}

func (p *KarpenterNodePoolProvisioner) Provision(ctx context.Context, nodePools []*api.NodePool, values map[string]interface{}) error {
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

func (p *KarpenterNodePoolProvisioner) provisionNodePool(ctx context.Context, nodePool *api.NodePool, values map[string]interface{}) error {
	template, err := p.generateNodePoolTemplate(nodePool, values, provisionersFileName)
	if err != nil {
		return err
	}
	if template == "" {
		return nil
	}
	_, err = p.KubectlExecute(ctx, []string{"apply"}, template, false)
	if err != nil {
		return errors.Wrapf(err, "kubectl apply failed for node pool %s", nodePool.Name)
	}
	return nil
}

func (p *KarpenterNodePoolProvisioner) isKarpenterEnabled() bool {
	if v, found := p.cluster.ConfigItems["karpenter_pools_enabled"]; found && v == "true" {
		return true
	}
	return false
}

func (p *KarpenterNodePoolProvisioner) Reconcile(ctx context.Context, updater updatestrategy.UpdateStrategy) error {
	karpenterPools := p.cluster.KarpenterPools()

	crdResolver, err := updatestrategy.NewKarpenterCRDResolver(ctx, p.k8sClients)
	if err != nil {
		return err
	}
	existingProvisioners, err := p.k8sClients.List(ctx, crdResolver.NodePoolCRDName, "", metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pr := range existingProvisioners.Items {
		nodePool := api.NodePool{Name: pr.GetName(), Profile: karpenterNodePoolProfile}
		if !inNodePoolList(&nodePool, karpenterPools) {
			err := p.k8sClients.Delete(ctx, crdResolver.NodePoolCRDName, "", pr.GetName(), metav1.DeleteOptions{})
			if err != nil {
				return err
			}
		}
	}

	nodes, err := p.k8sClients.List(ctx, "nodes", "", metav1.ListOptions{
		LabelSelector: "karpenter.sh/nodepool",
	})
	if err != nil {
		return err
	}

	// also engage the updateStrategy mechanisms (rolling update, CLC) to speed up the decommissioning process,
	// enforcing node force decommissioning SLAs since karpenter will wait indefinitely for the pod disruption budgets
	// the list of obsolete node pools is derived from nodes and not relying on karpenter CRDs
	// in case the CRD objects are deleted already
	var obsoleteNodePools []*api.NodePool
	checkedNodePools := make(map[string]interface{})
	for _, node := range nodes.Items {
		nodePoolName := node.GetLabels()["karpenter.sh/nodepool"]
		if _, ok := checkedNodePools[nodePoolName]; ok {
			continue
		}
		checkedNodePools[nodePoolName] = nil
		nodePool := api.NodePool{Name: nodePoolName, Profile: karpenterNodePoolProfile}
		if !inNodePoolList(&nodePool, karpenterPools) {
			obsoleteNodePools = append(obsoleteNodePools, &nodePool)
		}
	}
	for _, nodePool := range obsoleteNodePools {
		err := updater.PrepareForRemoval(ctx, nodePool)
		if err != nil {
			return err
		}
	}

	existingNodeTemplates, err := p.k8sClients.List(ctx, crdResolver.NodeTemplateCRDName(), "", metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, pr := range existingNodeTemplates.Items {
		if !inNodePoolList(&api.NodePool{Name: pr.GetName()}, karpenterPools) {
			err = p.k8sClients.Delete(ctx, crdResolver.NodeTemplateCRDName(), "", pr.GetName(), metav1.DeleteOptions{})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// AWSNodePoolProvisioner is a node provisioner able to provision node pools
// in AWS via cloudformation.
// TODO: move AWS specific implementation to a separate file/package.
type AWSNodePoolProvisioner struct {
	NodePoolTemplateRenderer
	azInfo *AZInfo
}

// Provision provisions node pools of the cluster.
func (p *AWSNodePoolProvisioner) Provision(ctx context.Context, nodePools []*api.NodePool, values map[string]interface{}) error {
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

// provisionNodePool provisions a single node pool.
func (p *AWSNodePoolProvisioner) provisionNodePool(ctx context.Context, nodePool *api.NodePool, values map[string]interface{}) error {
	values["supports_t2_unlimited"] = supportsT2Unlimited(nodePool.InstanceTypes)

	instanceInfo, err := p.NodePoolTemplateRenderer.instanceTypes.SyntheticInstanceInfo(nodePool.InstanceTypes)
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
	orphaned := orphanedNodePoolStacks(nodePoolStacks, p.cluster.ASGBackedPools())

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

// supportsT2Unlimited used to configure CPU credits in the CF template if a pool only has t2 instances
func supportsT2Unlimited(instanceTypes []string) bool {
	for _, instanceType := range instanceTypes {
		if !strings.HasPrefix(instanceType, "t2.") {
			return false
		}
	}
	return true
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
