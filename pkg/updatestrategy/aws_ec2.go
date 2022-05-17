package updatestrategy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

// EC2NodePoolBackend defines a node pool consiting of EC2 instances
// managed externally by some component e.g. Karpenter.
type EC2NodePoolBackend struct {
	ec2Client ec2iface.EC2API
	clusterID string
}

// NewEC2NodePoolBackend initializes a new EC2NodePoolBackend for
// the given clusterID and AWS session and.
func NewEC2NodePoolBackend(clusterID string, sess *session.Session) *EC2NodePoolBackend {
	return &EC2NodePoolBackend{
		ec2Client: ec2.New(sess),
		clusterID: clusterID,
	}
}

// Get gets the EC2 instances matching to the node pool by looking at node pool
// tag.
// The node generation is set to 'current' for nodes with up-to-date
// userData,ImageID and tags and 'outdated' for nodes with an outdated
// configuration.
func (n *EC2NodePoolBackend) Get(nodePool *api.NodePool) (*NodePool, error) {
	instances, err := n.getInstances(nodePool)
	if err != nil {
		return nil, fmt.Errorf("failed to list EC2 instances of the node pool: %w", err)
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

		// TODO: resolve template version and compare that

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

		ltParams := &ec2.DescribeLaunchTemplateVersionsInput{
			LaunchTemplateName: aws.String(awsValidID(n.clusterID) + "-" + nodePool.Name),
			Versions:           aws.StringSlice([]string{"$Latest"}),
		}

		ltResp, err := n.ec2Client.DescribeLaunchTemplateVersionsWithContext(context.TODO(), ltParams)
		if err != nil {
			return nil, err
		}

		if len(ltResp.LaunchTemplateVersions) != 1 {
			return nil, fmt.Errorf("expected one LaunchTemplateVersion, found %d", len(ltResp.LaunchTemplateVersions))
		}

		ltVersion := ltResp.LaunchTemplateVersions[0]

		desiredInstanceConfig := &InstanceConfig{
			UserData: aws.StringValue(ltVersion.LaunchTemplateData.UserData),
			ImageID:  aws.StringValue(ltVersion.LaunchTemplateData.ImageId),
			Tags:     make(map[string]string),
		}

		for _, tagSpec := range ltVersion.LaunchTemplateData.TagSpecifications {
			if aws.StringValue(tagSpec.ResourceType) == "instance" {
				for _, tag := range tagSpec.Tags {
					desiredInstanceConfig.Tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
				}

			}
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

// getInstances lists all running instances of the node pool.
func (n *EC2NodePoolBackend) getInstances(nodePool *api.NodePool) ([]*ec2.Instance, error) {
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

	return instances, nil
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

func (n *EC2NodePoolBackend) MarkForDecommission(nodePool *api.NodePool) error {
	return nil
}

func (n *EC2NodePoolBackend) Scale(nodePool *api.NodePool, replicas int) error {
	return nil
}

func (n *EC2NodePoolBackend) Terminate(node *Node, decrementDesired bool) error {
	return nil
}

func (n *EC2NodePoolBackend) Decommission(ctx context.Context, nodePool *api.NodePool) error {
	instances, err := n.getInstances(nodePool)
	if err != nil {
		return fmt.Errorf("failed to list EC2 instances of the node pool: %w", err)
	}

	instanceIds := make([]*string, 0, len(instances))
	for _, instance := range instances {
		instanceIds = append(instanceIds, instance.InstanceId)
	}

	params := &ec2.TerminateInstancesInput{
		InstanceIds: instanceIds,
	}
	_, err = n.ec2Client.TerminateInstancesWithContext(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to terminate EC2 instances of node pool '%s': %w", nodePool.Name, err)
	}

	// wait for all instances to be terminated
	for {
		select {
		case <-time.After(15 * time.Second):
			instances, err := n.getInstances(nodePool)
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

func awsValidID(id string) string {
	return strings.Replace(id, ":", "__", -1)
}
