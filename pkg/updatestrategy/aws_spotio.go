package updatestrategy

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	"github.com/cenkalti/backoff"
	spotio "github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

const (
	spotIOOceanIDTagKey = "spot.io/ocean-id"
)

// SpotIONodePoolsBackend defines a node pool consiting of externally managed
// EC2 instances.
type SpotIONodePoolsBackend struct {
	ec2Client    ec2iface.EC2API
	elbClient    elbiface.ELBAPI
	cfClient     cloudformationiface.CloudFormationAPI
	clusterID    string
	spotIOClient spotio.Service
}

// NewSpotIONodePoolsBackend initializes a new SpotIONodePoolsBackend for the given clusterID and AWS
// session and.
func NewSpotIONodePoolsBackend(clusterID string, sess *session.Session, spotIOClient spotio.Service) *SpotIONodePoolsBackend {
	return &SpotIONodePoolsBackend{
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
// The configuration is looked up in spot.io API.
func (n *SpotIONodePoolsBackend) Get(nodePool *api.NodePool) (*NodePool, error) {
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

		desiredInstanceConfig, err := n.getDesiredInstanceConfig(context.TODO(), instance, nodePool.Name)
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

func (n *SpotIONodePoolsBackend) getDesiredInstanceConfig(ctx context.Context, instance *ec2.Instance, nodePoolName string) (*InstanceConfig, error) {
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

	var resp *spotio.ListLaunchSpecsOutput
	err := backoff.Retry(
		func() (e error) {
			resp, e = n.spotIOClient.ListLaunchSpecs(ctx, params)
			return
		},
		// Spot.io has a transient error on its API currently being
		// investigated by their team. The following wraps the call on
		// a backoff of 1 second with max 5 attempts.
		backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 5))

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve spot.io's launch specs: %s", err)
	}

	// find launchspec from node pool name
	var spec *spotio.LaunchSpec
	for _, ls := range resp.LaunchSpecs {
		if aws.StringValue(ls.Name) == nodePoolName {
			spec = ls
			break
		}
	}

	if spec == nil {
		return nil, fmt.Errorf("failed get LaunchSpec '%s' for OceanID '%s'", nodePoolName, oceanID)
	}

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

func (n *SpotIONodePoolsBackend) MarkForDecommission(nodePool *api.NodePool) error {
	return nil
}

func (n *SpotIONodePoolsBackend) Scale(nodePool *api.NodePool, replicas int) error {
	return nil
}

func (n *SpotIONodePoolsBackend) Terminate(node *Node, decrementDesired bool) error {
	return nil
}
