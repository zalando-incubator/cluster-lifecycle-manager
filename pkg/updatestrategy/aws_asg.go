package updatestrategy

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

const (
	clusterIDTagPrefix          = "kubernetes.io/cluster/"
	resourceLifecycleOwned      = "owned"
	nodePoolTag                 = "NodePool"
	userDataAttribute           = "userData"
	instanceTypeAttribute       = "instanceType"
	instanceIdFilter            = "instance-id"
	instanceHealthStatusHealthy = "Healthy"
)

const (
	outdatedNodeGeneration int = iota
	currentNodeGeneration
)

// ASGNodePoolsBackend defines a node pool backed by an AWS Auto Scaling Group.
type ASGNodePoolsBackend struct {
	asgClient autoscalingiface.AutoScalingAPI
	ec2Client ec2iface.EC2API
	elbClient elbiface.ELBAPI
	clusterID string
}

// NewASGNodePoolsBackend initializes a new ASGNodePoolsBackend for the given clusterID and AWS
// session and.
func NewASGNodePoolsBackend(clusterID string, sess *session.Session) *ASGNodePoolsBackend {
	return &ASGNodePoolsBackend{
		asgClient: autoscaling.New(sess),
		ec2Client: ec2.New(sess),
		elbClient: elb.New(sess),
		clusterID: clusterID,
	}
}

// Get gets the ASG matching to the node pool and gets all instances from the
// ASG. The node generation is set to 'current' for nodes with the latest
// launch configuration and 'outdated' for nodes with an older launch
// configuration.
func (n *ASGNodePoolsBackend) Get(nodePool *api.NodePool) (*NodePool, error) {
	asg, err := n.getNodePoolASG(nodePool)
	if err != nil {
		return nil, err
	}

	oldInstances, err := n.getInstancesToUpdate(asg)
	if err != nil {
		return nil, err
	}

	lbInstances, err := n.getLoadBalancerAttachedInstancesReadiness(asg)
	if err != nil {
		return nil, err
	}

	// TODO: also lookup target groups for ALBs attached to the ASG (for Ingress)

	nodes := make([]*Node, 0, len(asg.Instances))

	for _, instance := range asg.Instances {
		instanceID := aws.StringValue(instance.InstanceId)
		node := &Node{
			ProviderID:    fmt.Sprintf("aws:///%s/%s", aws.StringValue(instance.AvailabilityZone), instanceID),
			FailureDomain: aws.StringValue(instance.AvailabilityZone),
			Generation:    currentNodeGeneration,
			Ready:         aws.StringValue(instance.HealthStatus) == instanceHealthStatusHealthy && aws.StringValue(instance.LifecycleState) == autoscaling.LifecycleStateInService,
		}

		if oldInstances[instanceID] {
			node.Generation = outdatedNodeGeneration
		}

		// if the node is ready from the ASG point of view, check if
		// the instance is registered in a LoadBalancer and set the
		// readiness based on the load balancer instance state.
		if ready, ok := lbInstances[instanceID]; node.Ready && ok {
			node.Ready = ready
		}

		nodes = append(nodes, node)
	}

	return &NodePool{
		Min:        int(aws.Int64Value(asg.MinSize)),
		Max:        int(aws.Int64Value(asg.MaxSize)),
		Desired:    int(aws.Int64Value(asg.DesiredCapacity)),
		Current:    len(nodes),
		Generation: currentNodeGeneration,
		Nodes:      nodes,
	}, nil
}

// Scale sets the desired capacity of the ASG to the number of replicas.
func (n *ASGNodePoolsBackend) Scale(nodePool *api.NodePool, replicas int) error {
	asg, err := n.getNodePoolASG(nodePool)
	if err != nil {
		return err
	}

	min := int64(math.Min(float64(replicas), float64(aws.Int64Value(asg.MinSize))))
	max := int64(math.Max(float64(replicas), float64(aws.Int64Value(asg.MaxSize))))

	params := &autoscaling.UpdateAutoScalingGroupInput{
		AutoScalingGroupName: asg.AutoScalingGroupName,
		DesiredCapacity:      aws.Int64(int64(replicas)),
		MinSize:              aws.Int64(min),
		MaxSize:              aws.Int64(max),
	}

	_, err = n.asgClient.UpdateAutoScalingGroup(params)
	return err
}

// Terminate terminates a node from the ASG and optionally decrements the
// DesiredCapacity. By default the desired capacity will not be decremented.
func (n *ASGNodePoolsBackend) Terminate(node *Node, decrementDesired bool) error {
	instanceId := aws.String(instanceIDFromProviderID(node.ProviderID, node.FailureDomain))

	params := &autoscaling.TerminateInstanceInAutoScalingGroupInput{
		InstanceId:                     instanceId,
		ShouldDecrementDesiredCapacity: aws.Bool(decrementDesired),
	}

	_, err := n.asgClient.TerminateInstanceInAutoScalingGroup(params)

	if err != nil {
		// could be because the instance is already being terminated, check for that
		status, serr := n.ec2Client.DescribeInstanceStatus(&ec2.DescribeInstanceStatusInput{
			IncludeAllInstances: aws.Bool(true),
			InstanceIds:         []*string{instanceId},
		})

		if serr != nil || len(status.InstanceStatuses) == 0 {
			return err
		}

		switch aws.StringValue(status.InstanceStatuses[0].InstanceState.Name) {
		case ec2.InstanceStateNameShuttingDown, ec2.InstanceStateNameStopping, ec2.InstanceStateNameTerminated, ec2.InstanceStateNameStopped:
			return nil
		default:
			return err
		}
	}

	return nil
}

// instanceIDFromProviderID extracts the EC2 instanceID from a Kubernetes
// ProviderID.
func instanceIDFromProviderID(providerID, az string) string {
	return strings.TrimPrefix(providerID, "aws:///"+az+"/")
}

// getNodePoolASG returns the ASG mapping to the specified node pool.
func (n *ASGNodePoolsBackend) getNodePoolASG(nodePool *api.NodePool) (*autoscaling.Group, error) {
	params := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{},
	}

	expectedTags := []*autoscaling.TagDescription{
		{
			Key:   aws.String(clusterIDTagPrefix + n.clusterID),
			Value: aws.String(resourceLifecycleOwned),
		},
		{
			Key:   aws.String(nodePoolTag),
			Value: aws.String(nodePool.Name),
		},
	}

	var asg *autoscaling.Group
	err := n.asgClient.DescribeAutoScalingGroupsPages(params, func(resp *autoscaling.DescribeAutoScalingGroupsOutput, lastPage bool) bool {
		for _, group := range resp.AutoScalingGroups {
			if asgHasAllTags(expectedTags, group.Tags) {
				asg = group
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	if asg == nil {
		return nil, fmt.Errorf("failed to find ASG for node pool '%s'", nodePool.Name)
	}

	return asg, nil
}

// getLaunchConfiguration gets the launch configuration of an ASG.
func (n *ASGNodePoolsBackend) getLaunchConfiguration(asg *autoscaling.Group) (*autoscaling.LaunchConfiguration, error) {
	params := &autoscaling.DescribeLaunchConfigurationsInput{
		LaunchConfigurationNames: []*string{
			asg.LaunchConfigurationName,
		},
	}

	resp, err := n.asgClient.DescribeLaunchConfigurations(params)
	if err != nil {
		return nil, err
	}

	if len(resp.LaunchConfigurations) != 1 {
		return nil, fmt.Errorf("expected 1 launch configuration, got %d", len(resp.LaunchConfigurations))
	}

	lc := resp.LaunchConfigurations[0]

	return lc, nil
}

// getInstancesToUpdate returns a list of instances with outdated userData.
func (n *ASGNodePoolsBackend) getInstancesToUpdate(asg *autoscaling.Group) (map[string]bool, error) {
	launchConfig, err := n.getLaunchConfiguration(asg)
	if err != nil {
		return nil, err
	}

	oldInstances := make(map[string]bool)

	instancesAMIs := make(map[string]string)

	instanceIds := make([]*string, 0, len(asg.Instances))
	for _, instance := range asg.Instances {
		instanceIds = append(instanceIds, instance.InstanceId)
	}

	params := &ec2.DescribeInstancesInput{
		InstanceIds: instanceIds,
	}

	err = n.ec2Client.DescribeInstancesPages(params, func(resp *ec2.DescribeInstancesOutput, lastPage bool) bool {
		for _, reservation := range resp.Reservations {
			for _, instance := range reservation.Instances {
				instancesAMIs[aws.StringValue(instance.InstanceId)] = aws.StringValue(instance.ImageId)
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	for _, instance := range asg.Instances {
		params := &ec2.DescribeInstanceAttributeInput{
			Attribute:  aws.String(userDataAttribute),
			InstanceId: instance.InstanceId,
		}
		userDataResp, err := n.ec2Client.DescribeInstanceAttribute(params)
		if err != nil {
			return nil, err
		}

		params.Attribute = aws.String(instanceTypeAttribute)
		instanceTypeResp, err := n.ec2Client.DescribeInstanceAttribute(params)
		if err != nil {
			return nil, err
		}

		var instanceSpotPrice *string
		spotPriceResp, err := n.ec2Client.DescribeSpotInstanceRequests(&ec2.DescribeSpotInstanceRequestsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String(instanceIdFilter),
					Values: []*string{instance.InstanceId},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		if len(spotPriceResp.SpotInstanceRequests) != 0 {
			instanceSpotPrice = spotPriceResp.SpotInstanceRequests[0].SpotPrice
		}

		spotPricesMatch, err := compareSpotPrices(launchConfig.SpotPrice, instanceSpotPrice)
		if err != nil {
			return nil, err
		}

		// an instance is considered old when userdata, instance type
		// or AMI does not match what is in the Launch Configuration
		// for the ASG.
		if aws.StringValue(userDataResp.UserData.Value) != aws.StringValue(launchConfig.UserData) ||
			aws.StringValue(instanceTypeResp.InstanceType.Value) != aws.StringValue(launchConfig.InstanceType) ||
			instancesAMIs[aws.StringValue(instance.InstanceId)] != aws.StringValue(launchConfig.ImageId) ||
			!spotPricesMatch {
			oldInstances[aws.StringValue(instance.InstanceId)] = true
		}
	}

	return oldInstances, nil
}

func parseSpotPrice(spotPrice *string) (float64, error) {
	if aws.StringValue(spotPrice) == "" {
		return 0, nil
	}

	return strconv.ParseFloat(aws.StringValue(spotPrice), 64)
}

// compareSpotPrices returns true if spot prices are identical (either both are absent or both are present and equal
// in value. it's needed because AWS munges the spot price in some places, e.g. price on the launch configuration turns
// from 0.12 into 0.1200000 when read back from the API, but the same doesn't apply to the DescribeSpotInstanceRequests
// API
func compareSpotPrices(oldSpotPrice *string, newSpotPrice *string) (bool, error) {
	parsedOld, err := parseSpotPrice(oldSpotPrice)
	if err != nil {
		return false, err
	}

	parsedNew, err := parseSpotPrice(newSpotPrice)
	if err != nil {
		return false, err
	}

	return parsedOld == parsedNew, err
}

// getLoadBalancerAttachedInstancesReadiness returns a mapping of instanceId ->
// readiness based on the instance state in any load balancers attached to the
// ASG. The instance must be 'InService' in all load balancers that it is
// attached to, to be considered ready.
func (n *ASGNodePoolsBackend) getLoadBalancerAttachedInstancesReadiness(asg *autoscaling.Group) (map[string]bool, error) {
	params := &autoscaling.DescribeLoadBalancersInput{
		AutoScalingGroupName: asg.AutoScalingGroupName,
	}

	resp, err := n.asgClient.DescribeLoadBalancers(params)
	if err != nil {
		return nil, err
	}

	instanceReadiness := make(map[string]bool)
	for _, lbState := range resp.LoadBalancers {
		// TODO: optimize calls based on state
		params := &elb.DescribeLoadBalancersInput{
			LoadBalancerNames: []*string{lbState.LoadBalancerName},
		}

		resp, err := n.elbClient.DescribeLoadBalancers(params)
		if err != nil {
			return nil, err
		}

		if len(resp.LoadBalancerDescriptions) != 1 {
			return nil, fmt.Errorf("expected 1 load balancer, found %d", len(resp.LoadBalancerDescriptions))
		}

		healthParams := &elb.DescribeInstanceHealthInput{
			LoadBalancerName: lbState.LoadBalancerName,
			Instances:        resp.LoadBalancerDescriptions[0].Instances,
		}

		healthResp, err := n.elbClient.DescribeInstanceHealth(healthParams)
		if err != nil {
			return nil, err
		}

		for _, state := range healthResp.InstanceStates {
			inService := aws.StringValue(state.State) == autoscaling.LifecycleStateInService
			if ready, ok := instanceReadiness[aws.StringValue(state.InstanceId)]; ok {
				if ready {
					instanceReadiness[aws.StringValue(state.InstanceId)] = inService
				}
			} else {
				instanceReadiness[aws.StringValue(state.InstanceId)] = inService
			}
		}
	}

	return instanceReadiness, nil
}

// asgHasAllTags returns true if the asg tags matches the expected tags.
// autoscaling tag keys are unique
func asgHasAllTags(expected, tags []*autoscaling.TagDescription) bool {
	if len(expected) > len(tags) {
		return false
	}

	matching := 0

	for _, e := range expected {
		for _, tag := range tags {
			if aws.StringValue(e.Key) == aws.StringValue(tag.Key) &&
				aws.StringValue(e.Value) == aws.StringValue(tag.Value) {
				matching++
			}
		}
	}

	return matching == len(expected)
}
