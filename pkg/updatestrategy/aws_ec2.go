package updatestrategy

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/pkg/errors"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util"
)

const (
	karpenterProvisionerTag          = "karpenter.sh/provisioner-name"
	karpenterNodePoolTag             = "karpenter.sh/nodepool"
	karpenterProvisionerResource     = "provisioners.karpenter.sh"
	karpenterAWSNodeTemplateResource = "awsnodetemplates.karpenter.k8s.aws"
	karpenterNodePoolResource        = "nodepools.karpenter.sh"
	KarpenterEC2NodeClassResource    = "ec2nodeclasses.karpenter.k8s.aws"
	crd                              = "CustomResourceDefinition"
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
	crdResolver *util.LazyOf[*KarpenterCRDNameResolver]
	ec2Client   ec2iface.EC2API
	clusterID   string
}

// NewEC2NodePoolBackend initializes a new EC2NodePoolBackend for
// the given clusterID and AWS session and.
func NewEC2NodePoolBackend(clusterID string, sess *session.Session, crdResolverInitializer func() (*KarpenterCRDNameResolver, error)) *EC2NodePoolBackend {
	return &EC2NodePoolBackend{
		ec2Client:   ec2.New(sess),
		clusterID:   clusterID,
		crdResolver: util.NewLazyOf[*KarpenterCRDNameResolver](crdResolverInitializer),
	}
}

// Get gets the EC2 instances matching to the node pool by looking at node pool
// tag.
// The node generation is set to 'current' for nodes with up-to-date
// userData,ImageID and tags and 'outdated' for nodes with an outdated
// configuration.
func (n *EC2NodePoolBackend) Get(ctx context.Context, nodePool *api.NodePool) (*NodePool, error) {
	instances, err := n.getInstances(n.filterWithNodePool(nodePool))
	if err != nil {
		return nil, fmt.Errorf("failed to list EC2 instances of the node pool: %w", err)
	}

	crdResolver, err := n.crdResolver.Value()
	if err != nil {
		return nil, err
	}

	nodes := make([]*Node, 0)
	nodePoolConfig, err := crdResolver.NodePoolConfigGetter(ctx, nodePool) // in case of decommission nodePoolConfig is nil, and all nodes are deleted anyway
	if err != nil {
		return nil, err
	}
	for _, instance := range instances {
		instanceID := aws.StringValue(instance.InstanceId)

		instanceConfig, err := n.getInstanceConfig(instance)
		if err != nil {
			return nil, err
		}
		generation := currentNodeGeneration

		if !InstanceConfigUpToDate(instanceConfig, nodePoolConfig) {
			generation = outdatedNodeGeneration
		}

		node := &Node{
			ProviderID:    fmt.Sprintf("aws:///%s/%s", aws.StringValue(instance.Placement.AvailabilityZone), instanceID),
			FailureDomain: aws.StringValue(instance.Placement.AvailabilityZone),
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

func (n *EC2NodePoolBackend) filterWithNodePool(nodePool *api.NodePool) []*ec2.Filter {
	return []*ec2.Filter{
		{
			Name: aws.String("tag:" + clusterIDTagPrefix + n.clusterID),
			Values: []*string{
				aws.String(resourceLifecycleOwned),
			},
		},
		{
			Name: aws.String("tag:" + nodePoolTag),
			Values: []*string{
				aws.String(nodePool.Name),
			},
		},
	}
}

// getInstances lists all running instances of the node pool.
func (n *EC2NodePoolBackend) getInstances(filters []*ec2.Filter) ([]*ec2.Instance, error) {
	params := &ec2.DescribeInstancesInput{
		Filters: filters,
	}

	instances := make([]*ec2.Instance, 0)
	err := n.ec2Client.DescribeInstancesPagesWithContext(context.TODO(), params, func(output *ec2.DescribeInstancesOutput, lastPage bool) bool {
		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				switch aws.StringValue(instance.State.Name) {
				case ec2.InstanceStateNameRunning, ec2.InstanceStateNamePending, ec2.InstanceStateNameStopped:
					instances = append(instances, instance)
				}
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	return instances, nil
}

func (n *EC2NodePoolBackend) getInstanceConfig(i *ec2.Instance) (*InstanceConfig, error) {
	// note: this make an extra http call to aws api for each node
	tags := make(map[string]string, len(i.Tags))
	for _, tag := range i.Tags {
		tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
	}
	config := InstanceConfig{
		ImageID: aws.StringValue(i.ImageId),
		Tags:    tags,
	}
	params := &ec2.DescribeInstanceAttributeInput{
		Attribute:  aws.String("userData"),
		DryRun:     aws.Bool(false),
		InstanceId: i.InstanceId,
	}
	op, err := n.ec2Client.DescribeInstanceAttributeWithContext(context.TODO(), params)
	if err != nil {
		return nil, err
	}
	config.UserData = aws.StringValue(op.UserData.Value)
	return &config, nil
}

func (n *EC2NodePoolBackend) MarkForDecommission(context.Context, *api.NodePool) error {
	return nil
}

func (n *EC2NodePoolBackend) Scale(context.Context, *api.NodePool, int) error {
	return nil
}

func (n *EC2NodePoolBackend) Terminate(context.Context, *Node, bool) error {
	return nil
}

func (n *EC2NodePoolBackend) DecommissionNodePool(ctx context.Context, nodePool *api.NodePool) error {
	filters := n.filterWithNodePool(nodePool)
	return n.decommission(ctx, filters)
}

func (n *EC2NodePoolBackend) DecommissionKarpenterNodes(ctx context.Context) error {
	crdResolver, err := n.crdResolver.Value()
	if err != nil {
		return err
	}
	return n.decommission(ctx, []*ec2.Filter{
		{
			Name: aws.String("tag:" + clusterIDTagPrefix + n.clusterID),
			Values: []*string{
				aws.String(resourceLifecycleOwned),
			},
		},
		{
			Name: aws.String("tag-key"),
			Values: []*string{
				aws.String(crdResolver.getInstanceTag()),
			},
		},
	})
}

func (n *EC2NodePoolBackend) decommission(ctx context.Context, filters []*ec2.Filter) error {
	instances, err := n.getInstances(filters)
	if err != nil {
		return fmt.Errorf("failed to list EC2 instances of the node pool: %w", err)
	}

	if len(instances) == 0 {
		return nil
	}

	instanceIDs := make([]*string, 0, len(instances))
	for _, instance := range instances {
		instanceIDs = append(instanceIDs, instance.InstanceId)
	}

	params := &ec2.TerminateInstancesInput{
		InstanceIds: instanceIDs,
	}
	_, err = n.ec2Client.TerminateInstancesWithContext(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to terminate EC2 instances of the filters '%s': %w", filters, err)
	}

	// wait for all instances to be terminated
	for {
		select {
		case <-time.After(15 * time.Second):
			instances, err := n.getInstances(filters)
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

type KarpenterCRDNameResolver struct {
	NodePoolCRDName string
	k8sClients      *kubernetes.ClientsCollection
}

func NewKarpenterCRDResolver(ctx context.Context, k8sClients *kubernetes.ClientsCollection) (*KarpenterCRDNameResolver, error) {
	exists, err := k8sClients.Exists(ctx, crd, "", karpenterNodePoolResource, v1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if exists {
		return &KarpenterCRDNameResolver{
			NodePoolCRDName: karpenterNodePoolResource,
			k8sClients:      k8sClients,
		}, nil
	}
	return &KarpenterCRDNameResolver{
		NodePoolCRDName: karpenterProvisionerResource,
		k8sClients:      k8sClients,
	}, nil
}

func (r *KarpenterCRDNameResolver) NodeTemplateCRDName() string {
	switch r.NodePoolCRDName {
	case karpenterNodePoolResource:
		return KarpenterEC2NodeClassResource
	default:
		return karpenterAWSNodeTemplateResource
	}
}

func (r *KarpenterCRDNameResolver) getAMIsFromSpec(spec interface{}) string {
	switch r.NodePoolCRDName {
	case karpenterNodePoolResource:
		amiSelectorTerms := spec.(map[string]interface{})["amiSelectorTerms"].([]interface{})
		var amis []string
		for _, amiSelectorTerm := range amiSelectorTerms {
			if amiSelectorTerm.(map[string]interface{})["id"] != nil {
				amis = append(amis, amiSelectorTerm.(map[string]interface{})["id"].(string))
			}
		}
		return strings.Join(amis, ",")
	default:
		return spec.(map[string]interface{})["amiSelector"].(map[string]interface{})["aws-ids"].(string)
	}
}

func (r *KarpenterCRDNameResolver) getInstanceTag() string {
	switch r.NodePoolCRDName {
	case karpenterNodePoolResource:
		return karpenterNodePoolTag
	default:
		return karpenterProvisionerTag
	}
}

func (r *KarpenterCRDNameResolver) NodePoolConfigGetter(ctx context.Context, nodePool *api.NodePool) (*InstanceConfig, error) {
	// CLM assumes that the node pool name is used for both the node-pool and the node-template that it references
	NodeTemplate, err := r.k8sClients.Get(ctx, r.NodeTemplateCRDName(), "", nodePool.Name, v1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// the node pool have been deleted. thus returning nil nodePoolConfig will result in labeling all nodes for decommission
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	spec, ok := NodeTemplate.Object["spec"]
	if !ok {
		return nil, errors.New("could not find spec in the %s object" + r.NodeTemplateCRDName())
	}
	tags := make(map[string]string)
	for k, v := range spec.(map[string]interface{})["tags"].(map[string]interface{}) {
		tags[k] = v.(string)
	}
	return &InstanceConfig{
		UserData: base64.StdEncoding.EncodeToString([]byte(spec.(map[string]interface{})["userData"].(string))),
		ImageID:  r.getAMIsFromSpec(spec),
		Tags:     tags,
	}, nil
}
