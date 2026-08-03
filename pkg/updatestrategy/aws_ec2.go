package updatestrategy

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	karpenterawsv1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	"github.com/cenkalti/backoff"
	"github.com/luci/go-render/render"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws/iface"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

type InstanceConfig struct {
	UserData string
	ImageID  string
	Tags     map[string]string
	// TODO: Karpenter supports more parameters https://karpenter.sh/preview/concepts/node-templates/
}

// InstanceConfigUpToDate compares current and desired InstanceConfig. It compares
// userdata, imageID and checks if the current config has all the desired tags.
// It does NOT check if the current config has too many EC2 tags as many tags are
// injected out of our control. This means removing a tag is not enough to
// make the configs unequal.
func InstanceConfigUpToDate(instanceConfig, poolConfig *InstanceConfig) bool {
	if instanceConfig.UserData != poolConfig.UserData {
		return false
	}

	if !util.Contains(strings.Split(poolConfig.ImageID, ","), instanceConfig.ImageID) {
		return false
	}

	for k, v := range poolConfig.Tags {
		if instanceValue, ok := instanceConfig.Tags[k]; !ok || v != instanceValue {
			return false
		}
	}
	return true
}

// EC2NodePoolBackend defines a node pool consisting of EC2 instances
// managed externally by some component e.g. Karpenter.
type EC2NodePoolBackend struct {
	karpenterClient *KarpenterNodePoolClient
	ec2Client       iface.EC2API
	cluster         *api.Cluster
}

// NewEC2NodePoolBackend initializes a new EC2NodePoolBackend for
// the given clusterID and AWS session and.
func NewEC2NodePoolBackend(cluster *api.Cluster, cfg aws.Config, karpenterClient *KarpenterNodePoolClient) *EC2NodePoolBackend {
	return &EC2NodePoolBackend{
		ec2Client:       ec2.NewFromConfig(cfg),
		cluster:         cluster,
		karpenterClient: karpenterClient,
	}
}

// Get gets the EC2 instances matching to the node pool by looking at node pool
// tag.
// The node generation is set to 'current' for nodes with up-to-date
// userData,ImageID and tags and 'outdated' for nodes with an outdated
// configuration.
func (n *EC2NodePoolBackend) Get(ctx context.Context, nodePool *api.NodePool) (*NodePool, error) {
	// scope the Karpenter Drift detection logic to only Karpenter node
	// pools that use Bottlerocket or AL2023 AMIs.
	// This is done to limit the change to the initial roll out as Drift
	// detection has a potential issue as outlined in:
	// https://github.com/aws/karpenter-provider-aws/pull/9083#issuecomment-5012830911
	if alias, ok := nodePool.ConfigItems["karpenter_ami_family_alias"]; ok && (strings.HasPrefix(alias, "bottlerocket") || strings.HasPrefix(alias, "al2023")) {
		return n.GetUsingKubernetesObjects(ctx, nodePool)
	}

	instances, err := n.getInstances(ctx, n.filterWithNodePool(nodePool))
	if err != nil {
		return nil, fmt.Errorf("failed to list EC2 instances of the node pool: %w", err)
	}

	nodes := make([]*Node, 0)
	nodePoolConfig, err := n.karpenterClient.NodePoolConfigGetter(ctx, nodePool) // in case of decommission nodePoolConfig is nil, and all nodes are deleted anyway
	if err != nil {
		return nil, fmt.Errorf("failed to get nodePool config for pool %q: %w", nodePool.Name, err)
	}
	for _, instance := range instances {
		instanceID := aws.ToString(instance.InstanceId)

		instanceConfig, err := n.getInstanceConfig(ctx, instance)
		if err != nil {
			return nil, fmt.Errorf("failed to get instance config for instance %q: %w", instanceID, err)
		}
		generation := currentNodeGeneration

		if !InstanceConfigUpToDate(instanceConfig, nodePoolConfig) {
			generation = outdatedNodeGeneration
		}

		node := &Node{
			ProviderID:    fmt.Sprintf("aws:///%s/%s", aws.ToString(instance.Placement.AvailabilityZone), instanceID),
			FailureDomain: aws.ToString(instance.Placement.AvailabilityZone),
			Generation:    generation,
			// not used in clc logic
			// Ready: true,
		}

		nodes = append(nodes, node)
	}

	// We only set Generation and Nodes as nothing else is needed by the
	// CLC strategy
	return &NodePool{
		Generation: currentNodeGeneration,
		Nodes:      nodes,
	}, nil
}

// GetUsingKubernetesObjects gets nodes for the node pool by looking at Kubernetes Node and
// Karpenter NodeClaim objects. Node generation is based on NodeClaim drift status.
func (n *EC2NodePoolBackend) GetUsingKubernetesObjects(ctx context.Context, nodePool *api.NodePool) (*NodePool, error) {
	nodePoolObj := &karpv1.NodePool{}
	err := n.karpenterClient.Get(ctx, client.ObjectKey{Name: nodePool.Name}, nodePoolObj, &client.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Karpenter NodePool %q: %w", nodePool.Name, err)
	}

	ec2NodeClassName := nodePoolObj.Spec.Template.Spec.NodeClassRef.Name
	if ec2NodeClassName == "" {
		// Keep compatibility with clusters where NodePool and NodeClass use the same name.
		ec2NodeClassName = nodePool.Name
	}

	ec2NodeClassObj := &karpenterawsv1.EC2NodeClass{}
	err = n.karpenterClient.Get(ctx, client.ObjectKey{Name: ec2NodeClassName}, ec2NodeClassObj, &client.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Karpenter EC2NodeClass %q: %w", ec2NodeClassName, err)
	}

	nodesList := &corev1.NodeList{}
	err = n.karpenterClient.List(ctx, nodesList, client.MatchingLabels{nodePoolTag: nodePool.Name})
	if err != nil {
		return nil, fmt.Errorf("failed to list Kubernetes Nodes of the node pool: %w", err)
	}

	nodeClaimsList := &karpv1.NodeClaimList{}
	err = n.karpenterClient.List(ctx, nodeClaimsList, client.MatchingLabels{karpv1.NodePoolLabelKey: nodePool.Name})
	if err != nil {
		return nil, fmt.Errorf("failed to list Karpenter NodeClaims of the node pool: %w", err)
	}

	driftByNodeClaimName, driftByNodeName := nodeClaimDriftMaps(nodeClaimsList.Items, nodePoolObj, ec2NodeClassObj.Annotations)

	nodes := make([]*Node, 0, len(nodesList.Items))
	for _, node := range nodesList.Items {
		if node.Spec.ProviderID == "" {
			continue
		}

		generation := currentNodeGeneration
		if driftByNodeName[node.Name] {
			generation = outdatedNodeGeneration
		} else if nodeClaimName, ok := nodeClaimNameFromOwnerReferences(node); ok && driftByNodeClaimName[nodeClaimName] {
			generation = outdatedNodeGeneration
		}

		nodes = append(nodes, &Node{
			ProviderID:    node.Spec.ProviderID,
			FailureDomain: nodeFailureDomain(node),
			Generation:    generation,
		})
	}

	return &NodePool{
		Generation: currentNodeGeneration,
		Nodes:      nodes,
	}, nil
}

func (n *EC2NodePoolBackend) filterWithNodePool(nodePool *api.NodePool) []ec2types.Filter {
	return []ec2types.Filter{
		{
			Name:   aws.String("tag:" + clusterIDTagPrefix + n.cluster.Name()),
			Values: []string{resourceLifecycleOwned},
		},
		{
			Name:   aws.String("tag:" + nodePoolTag),
			Values: []string{nodePool.Name},
		},
	}
}

// getInstances lists all running instances of the node pool.
func (n *EC2NodePoolBackend) getInstances(ctx context.Context, filters []ec2types.Filter) ([]ec2types.Instance, error) {
	params := &ec2.DescribeInstancesInput{
		Filters: filters,
	}

	instances := make([]ec2types.Instance, 0)
	paginator := ec2.NewDescribeInstancesPaginator(n.ec2Client, params)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				switch instance.State.Name {
				case ec2types.InstanceStateNameRunning, ec2types.InstanceStateNamePending, ec2types.InstanceStateNameStopped:
					instances = append(instances, instance)
				}
			}
		}
	}

	return instances, nil
}

func (n *EC2NodePoolBackend) getInstanceConfig(ctx context.Context, i ec2types.Instance) (*InstanceConfig, error) {
	// note: this make an extra http call to aws api for each node
	tags := make(map[string]string, len(i.Tags))
	for _, tag := range i.Tags {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	config := InstanceConfig{
		ImageID: aws.ToString(i.ImageId),
		Tags:    tags,
	}
	params := &ec2.DescribeInstanceAttributeInput{
		Attribute:  ec2types.InstanceAttributeNameUserData,
		DryRun:     aws.Bool(false),
		InstanceId: i.InstanceId,
	}
	op, err := n.ec2Client.DescribeInstanceAttribute(ctx, params)
	if err != nil {
		return nil, err
	}
	config.UserData = aws.ToString(op.UserData.Value)
	return &config, nil
}

func (n *EC2NodePoolBackend) MarkForDecommission(context.Context, *api.NodePool) error {
	return nil
}

func (n *EC2NodePoolBackend) Scale(context.Context, *api.NodePool, int) error {
	return nil
}

func (n *EC2NodePoolBackend) Terminate(ctx context.Context, pool *api.NodePool, node *Node, _ bool) error {
	// terminating the instance using AWS api, it will also trigger karpenter interruption controller to
	// delete the node and nodeClaim objects
	instanceID := instanceIDFromProviderID(node.ProviderID, node.FailureDomain)
	params := &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceID},
	}
	_, err := n.ec2Client.TerminateInstances(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to terminate EC2 instances of the node pool '%s': %w", render.Render(pool), err)
	}
	return nil
}

func (n *EC2NodePoolBackend) DecommissionNodePool(ctx context.Context, nodePool *api.NodePool) error {
	filters := n.filterWithNodePool(nodePool)
	return n.decommission(ctx, filters)
}

func (n *EC2NodePoolBackend) DecommissionKarpenterNodes(ctx context.Context) error {
	return n.decommission(ctx, []ec2types.Filter{
		{
			Name:   aws.String("tag:" + clusterIDTagPrefix + n.cluster.Name()),
			Values: []string{resourceLifecycleOwned},
		},
		{
			Name:   aws.String("tag-key"),
			Values: []string{karpv1.NodePoolLabelKey},
		},
	})
}

func (n *EC2NodePoolBackend) decommission(ctx context.Context, filters []ec2types.Filter) error {
	instances, err := n.getInstances(ctx, filters)
	if err != nil {
		return fmt.Errorf("failed to list EC2 instances of the node pool: %w", err)
	}

	if len(instances) == 0 {
		return nil
	}

	instanceIDs := make([]string, 0, len(instances))
	for _, instance := range instances {
		instanceIDs = append(instanceIDs, aws.ToString(instance.InstanceId))
	}

	params := &ec2.TerminateInstancesInput{
		InstanceIds: instanceIDs,
	}
	_, err = n.ec2Client.TerminateInstances(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to terminate EC2 instances of the filters '%v': %w", filters, err)
	}

	// wait for all instances to be terminated
	for {
		select {
		case <-time.After(15 * time.Second):
			instances, err := n.getInstances(ctx, filters)
			if err != nil {
				return fmt.Errorf("failed to list EC2 instances of the node pool: %w", err)
			}

			if len(instances) == 0 {
				return nil
			}
			// TODO: logging
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for instance termination: %w", ctx.Err())
		}
	}
}

type KarpenterNodePoolClient struct {
	client.Client
}

func NewKarpenterNodePoolClient(client client.Client) *KarpenterNodePoolClient {
	return &KarpenterNodePoolClient{
		Client: client,
	}
}

func (r *KarpenterNodePoolClient) getAMIsFromSpec(spec karpenterawsv1.EC2NodeClassSpec) string {
	var amis []string
	for _, amiSelectorTerm := range spec.AMISelectorTerms {
		if amiSelectorTerm.ID != "" {
			amis = append(amis, amiSelectorTerm.ID)
		}
	}
	return strings.Join(amis, ",")
}

func (r *KarpenterNodePoolClient) NodePoolConfigGetter(ctx context.Context, nodePool *api.NodePool) (*InstanceConfig, error) {
	ec2NodeClass := &karpenterawsv1.EC2NodeClass{}
	// CLM assumes that the node pool name is used for both the node-pool and the ec2 node class that it references
	getEC2NodeClass := func() error {
		err := r.Get(ctx, client.ObjectKey{Name: nodePool.Name}, ec2NodeClass, &client.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// the node pool have been deleted. thus returning nil nodePoolConfig will result in labeling all nodes for decommission
				return nil
			}
			return err
		}
		return nil
	}

	err := backoff.Retry(getEC2NodeClass, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 10))
	if err != nil {
		return nil, err
	}

	tags := make(map[string]string)
	for k, v := range ec2NodeClass.Spec.Tags {
		tags[k] = v
	}
	userData := ptr.Deref(ec2NodeClass.Spec.UserData, "")
	return &InstanceConfig{
		UserData: base64.StdEncoding.EncodeToString([]byte(userData)),
		ImageID:  r.getAMIsFromSpec(ec2NodeClass.Spec),
		Tags:     tags,
	}, nil
}

func nodeClaimDriftMaps(nodeClaims []karpv1.NodeClaim, nodePool *karpv1.NodePool, ec2NodeClassAnnotations map[string]string) (map[string]bool, map[string]bool) {
	driftByNodeClaimName := make(map[string]bool, len(nodeClaims))
	driftByNodeName := make(map[string]bool, len(nodeClaims))

	for _, nodeClaim := range nodeClaims {
		drifted := isNodeClaimDrifted(&nodeClaim)
		if !drifted && nodePool != nil {
			drifted = isNodeClaimBehindObservedTemplates(&nodeClaim, nodePool, ec2NodeClassAnnotations)
		}

		if !drifted {
			continue
		}

		driftByNodeClaimName[nodeClaim.GetName()] = true

		nodeName := nodeClaim.Status.NodeName
		if nodeName == "" {
			continue
		}
		driftByNodeName[nodeName] = true
	}

	return driftByNodeClaimName, driftByNodeName
}

func isNodeClaimDrifted(nodeClaim *karpv1.NodeClaim) bool {
	return nodeClaim.StatusConditions().Get(string(karpv1.ConditionTypeDrifted)).IsTrue()
}

func isNodeClaimBehindObservedTemplates(nodeClaim *karpv1.NodeClaim, nodePool *karpv1.NodePool, ec2NodeClassAnnotations map[string]string) bool {
	if hasHashMismatch(nodeClaim.Annotations, nodePool.Annotations, karpv1.NodePoolHashAnnotationKey, karpv1.NodePoolHashVersionAnnotationKey) {
		return true
	}

	if hasHashMismatch(nodeClaim.Annotations, ec2NodeClassAnnotations, karpenterawsv1.AnnotationEC2NodeClassHash, karpenterawsv1.AnnotationEC2NodeClassHashVersion) {
		return true
	}

	return false
}

func hasHashMismatch(nodeClaimAnnotations, sourceAnnotations map[string]string, hashKey, hashVersionKey string) bool {
	desiredHash, desiredHashFound := sourceAnnotations[hashKey]
	observedHash, observedHashFound := nodeClaimAnnotations[hashKey]

	// Missing hashes mean this signal is unavailable; caller should rely on other checks.
	if !desiredHashFound || !observedHashFound || desiredHash == "" || observedHash == "" {
		return false
	}

	if desiredHash != observedHash {
		return true
	}

	desiredHashVersion, desiredHashVersionFound := sourceAnnotations[hashVersionKey]
	observedHashVersion, observedHashVersionFound := nodeClaimAnnotations[hashVersionKey]
	if desiredHashVersionFound && observedHashVersionFound && desiredHashVersion != observedHashVersion {
		return true
	}

	return false
}

func nodeClaimNameFromOwnerReferences(node corev1.Node) (string, bool) {
	for _, ownerRef := range node.GetOwnerReferences() {
		if ownerRef.Kind == "NodeClaim" && ownerRef.Name != "" {
			return ownerRef.Name, true
		}
	}

	return "", false
}

func nodeFailureDomain(node corev1.Node) string {
	if failureDomain, ok := node.Labels[corev1.LabelTopologyZone]; ok {
		return failureDomain
	}

	if failureDomain, ok := node.Labels["failure-domain.beta.kubernetes.io/zone"]; ok {
		return failureDomain
	}

	return ""
}
