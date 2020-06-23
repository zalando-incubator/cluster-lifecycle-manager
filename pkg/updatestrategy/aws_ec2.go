package updatestrategy

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	spotio "github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

const (
	spotIOOceanIDTagKey = "spot.io/ocean-id"
)

// EC2NodePoolsBackend defines a node pool consiting of externally managed EC2
// instances
type EC2NodePoolsBackend struct {
	ec2Client    ec2iface.EC2API
	elbClient    elbiface.ELBAPI
	cfClient     cloudformationiface.CloudFormationAPI
	clusterID    string
	spotIOClient spotio.Service // TODO: need another layer
}

// NewEC2NodePoolsBackend initializes a new EC2NodePoolsBackend for the given clusterID and AWS
// session and.
func NewEC2NodePoolsBackend(clusterID string, sess *session.Session, spotIOClient spotio.Service) *EC2NodePoolsBackend {
	return &EC2NodePoolsBackend{
		cfClient:     cloudformation.New(sess),
		ec2Client:    ec2.New(sess),
		elbClient:    elb.New(sess),
		clusterID:    clusterID,
		spotIOClient: spotIOClient,
	}
}

// Get gets the EC2 instances matching to the node pool by looking at node pool
// tag.
// The node generation is set to 'current' for nodes with up-to-date
// userData,ImageID and tags and 'outdated' for nodes with an outdated
// configuration.
// TODO: the configuration is looked up in spot.io
func (n *EC2NodePoolsBackend) Get(nodePool *api.NodePool) (*NodePool, error) {
	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
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
		},
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

	nodes := make([]*Node, 0)
	for _, instance := range instances {
		instanceID := aws.StringValue(instance.InstanceId)
		node := &Node{
			ProviderID:    fmt.Sprintf("aws:///%s/%s", aws.StringValue(instance.Placement.AvailabilityZone), instanceID),
			FailureDomain: aws.StringValue(instance.Placement.AvailabilityZone),
			Generation:    currentNodeGeneration,
			// not used in clc logic
			// Ready: true,
		}

		params := &ec2.DescribeInstanceAttributeInput{
			InstanceId: instance.InstanceId,
			Attribute:  aws.String("userData"),
		}

		resp, err := n.ec2Client.DescribeInstanceAttributeWithContext(context.TODO(), params)
		if err != nil {
			return nil, err
		}

		currentInstanceConfig := &InstanceConfig{
			UserData: aws.StringValue(resp.UserData.Value),
			ImageID:  aws.StringValue(instance.ImageId),
			Tags:     make(map[string]string, len(instance.Tags)),
		}

		for _, tag := range instance.Tags {
			currentInstanceConfig.Tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
		}

		desiredInstanceConfig, err := n.getDesiredInstanceConfig(context.TODO(), instance)
		if err != nil {
			return nil, err
		}

		if !instanceConfigEqual(currentInstanceConfig, desiredInstanceConfig) {
			node.Generation = outdatedNodeGeneration
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

type InstanceConfig struct {
	UserData string
	ImageID  string
	Tags     map[string]string
}

// instanceConfigEqual compares current and desired InstanceConfig. It compares
// userdata, imageID and checks if the current config has all the desired tags.
// It does NOT check if the current config has too many EC2 tags as many tags are
// injected out of our control. This means removing a tag is not enough to
// make the configs unequal.
func instanceConfigEqual(current, desired *InstanceConfig) bool {
	if current.UserData != desired.UserData {
		return false
	}

	if current.ImageID != desired.ImageID {
		return false
	}

	// TODO: explain
	for k, v := range desired.Tags {
		if currentValue, ok := current.Tags[k]; !ok || v != currentValue {
			return false
		}
	}
	return true
}

func (n *EC2NodePoolsBackend) getDesiredInstanceConfig(ctx context.Context, instance *ec2.Instance) (*InstanceConfig, error) {
	var oceanID string

	for _, tag := range instance.Tags {
		if aws.StringValue(tag.Key) == spotIOOceanIDTagKey {
			oceanID = aws.StringValue(tag.Value)
			break
		}
	}

	if oceanID == "" {
		return nil, fmt.Errorf("no ocean-id found on instance '%s'", aws.StringValue(instance.InstanceId))
	}

	params := &spotio.ListLaunchSpecsInput{
		OceanID: aws.String(oceanID),
	}

	resp, err := n.spotIOClient.ListLaunchSpecs(ctx, params)
	if err != nil {
		return nil, err
	}

	if len(resp.LaunchSpecs) != 1 {
		return nil, fmt.Errorf("Expectd to get 1 LaunchSpec for oceanID '%s', got %d", oceanID, len(resp.LaunchSpecs))
	}

	spec := resp.LaunchSpecs[0]

	tags := make(map[string]string, len(spec.Tags))
	for _, tag := range spec.Tags {
		tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
	}

	return &InstanceConfig{
		UserData: aws.StringValue(spec.UserData),
		ImageID:  aws.StringValue(spec.ImageID),
		Tags:     tags,
	}, nil
}

func (n *EC2NodePoolsBackend) MarkForDecommission(nodePool *api.NodePool) error {
	return nil
}

func (n *EC2NodePoolsBackend) Scale(nodePool *api.NodePool, replicas int) error {
	return nil
}

func (n *EC2NodePoolsBackend) Terminate(node *Node, decrementDesired bool) error {
	return nil
}
