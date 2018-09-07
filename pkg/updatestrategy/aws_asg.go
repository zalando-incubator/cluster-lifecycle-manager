package updatestrategy

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	"github.com/cenkalti/backoff"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

const (
	clusterIDTagPrefix          = "kubernetes.io/cluster/"
	resourceLifecycleOwned      = "owned"
	kubeAutoScalerEnabledTagKey = "k8s.io/cluster-autoscaler/enabled"
	nodePoolTag                 = "NodePool"
	userDataAttribute           = "userData"
	instanceTypeAttribute       = "instanceType"
	instanceIdFilter            = "instance-id"
	instanceHealthStatusHealthy = "Healthy"
	ec2AutoscalingGroupTagKey   = "aws:autoscaling:groupName"
)

const (
	outdatedNodeGeneration = iota
	currentNodeGeneration
)

type asgLaunchParameters struct {
	launchTemplateName    string
	launchTemplateVersion string

	launchConfigurationInstanceType string
	launchConfigurationAMI          string
	launchConfigurationSpotPrice    string
	launchConfigurationUserData     string
	instanceAMIs                    map[string]string
}

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
	asgs, err := n.getNodePoolASGs(nodePool)
	if err != nil {
		return nil, err
	}

	nodes := make([]*Node, 0)
	minSize := 0
	maxSize := 0
	desiredCapacity := 0
	for _, asg := range asgs {
		minSize += int(aws.Int64Value(asg.MinSize))
		maxSize += int(aws.Int64Value(asg.MaxSize))
		desiredCapacity += int(aws.Int64Value(asg.DesiredCapacity))

		oldInstances, err := n.getInstancesToUpdate(asg)
		if err != nil {
			return nil, err
		}

		lbInstances, err := n.getLoadBalancerAttachedInstancesReadiness(asg)
		if err != nil {
			return nil, err
		}

		// TODO: also lookup target groups for ALBs attached to the ASG (for Ingress)

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
	}

	return &NodePool{
		Min:        minSize,
		Max:        maxSize,
		Desired:    desiredCapacity,
		Current:    len(nodes),
		Generation: currentNodeGeneration,
		Nodes:      nodes,
	}, nil
}

// Scale sets the desired capacity of the ASGs to the number of replicas.
// If the node pool is backed by multiple ASGs the scale operation will try to
// balance the increment/decrement of nodes over all the ASGs.
func (n *ASGNodePoolsBackend) Scale(nodePool *api.NodePool, replicas int) error {
	asgs, err := n.getNodePoolASGs(nodePool)
	if err != nil {
		return err
	}

	desired := 0
	for _, asg := range asgs {
		desired += int(aws.Int64Value(asg.DesiredCapacity))
	}

	diff := replicas - desired

	if diff == 0 {
		// nothing to change
		return nil
	}

	// add nodes to smallest non-empty asgs
	if diff > 0 {
		sort.Slice(asgs, func(i, j int) bool {
			iCap := aws.Int64Value(asgs[i].DesiredCapacity)
			jCap := aws.Int64Value(asgs[j].DesiredCapacity)

			if iCap == 0 {
				return false
			}
			if jCap == 0 {
				return true
			}
			return iCap < jCap
		})

	LoopIncrement:
		for {
			for _, asg := range asgs {
				if diff <= 0 {
					break LoopIncrement
				}
				asg.DesiredCapacity = aws.Int64(aws.Int64Value(asg.DesiredCapacity) + 1)
				diff--
			}
		}
	} else if diff < 0 { // remove nodes from biggest asgs
		sort.Slice(asgs, func(i, j int) bool {
			return aws.Int64Value(asgs[i].DesiredCapacity) > aws.Int64Value(asgs[j].DesiredCapacity)
		})

	LoopDecrement:
		for {
			for _, asg := range asgs {
				if diff >= 0 {
					break LoopDecrement
				}
				asg.DesiredCapacity = aws.Int64(aws.Int64Value(asg.DesiredCapacity) - 1)
				diff++
			}
		}
	}

	for _, asg := range asgs {
		min := int64(math.Min(float64(aws.Int64Value(asg.DesiredCapacity)), float64(aws.Int64Value(asg.MinSize))))
		max := int64(math.Max(float64(aws.Int64Value(asg.DesiredCapacity)), float64(aws.Int64Value(asg.MaxSize))))

		params := &autoscaling.UpdateAutoScalingGroupInput{
			AutoScalingGroupName: asg.AutoScalingGroupName,
			DesiredCapacity:      asg.DesiredCapacity,
			MinSize:              aws.Int64(min),
			MaxSize:              aws.Int64(max),
		}

		_, err := n.asgClient.UpdateAutoScalingGroup(params)
		if err != nil {
			return err
		}
	}

	return nil
}

// SuspendAutoscaling suspends autoscaling of the node pool if it was enabled.
// The implementation assumes the kubernetes cluster-autoscaler is used so it
// just removes a tag.
func (n *ASGNodePoolsBackend) SuspendAutoscaling(nodePool *api.NodePool) error {
	tags := map[string]string{
		kubeAutoScalerEnabledTagKey: "",
	}
	return n.deleteTags(nodePool, tags)
}

// deleteTags deletes the specified tags from the node pool ASGs.
func (n *ASGNodePoolsBackend) deleteTags(nodePool *api.NodePool, tags map[string]string) error {
	asgs, err := n.getNodePoolASGs(nodePool)
	if err != nil {
		return err
	}

	for _, asg := range asgs {
		asgTags := make([]*autoscaling.Tag, 0, len(tags))

		for key, val := range tags {
			tag := &autoscaling.Tag{
				Key:          aws.String(key),
				Value:        aws.String(val),
				ResourceId:   asg.AutoScalingGroupName,
				ResourceType: aws.String("auto-scaling-group"),
			}
			asgTags = append(asgTags, tag)
		}

		params := &autoscaling.DeleteTagsInput{
			Tags: asgTags,
		}

		_, err := n.asgClient.DeleteTags(params)
		if err != nil {
			return err
		}
	}

	return err
}

// Terminate terminates an instance from the ASG and optionally decrements the
// DesiredCapacity. By default the desired capacity will not be decremented.
// In case the new desired capacity is less then the current min size of the
// ASG, it will also decrease the ASG minSize.
// This function will not return until the instance has been terminated in AWS.
func (n *ASGNodePoolsBackend) Terminate(node *Node, decrementDesired bool) error {
	instanceId := instanceIDFromProviderID(node.ProviderID, node.FailureDomain)

	// if desired should be decremented check if we also need to decrement
	// the minSize of the ASG.
	if decrementDesired {
		// lookup ASG name in the EC2 tags of the instance
		var asgName string
		params := &ec2.DescribeTagsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("resource-id"),
					Values: []*string{aws.String(instanceId)},
				},
				{
					Name:   aws.String("key"),
					Values: []*string{aws.String(ec2AutoscalingGroupTagKey)},
				},
			},
		}
		err := n.ec2Client.DescribeTagsPages(params, func(resp *ec2.DescribeTagsOutput, lastPage bool) bool {
			for _, tag := range resp.Tags {
				if aws.StringValue(tag.Key) == ec2AutoscalingGroupTagKey {
					asgName = aws.StringValue(tag.Value)
					return false
				}
			}
			return true
		})
		if err != nil {
			return err
		}

		if asgName == "" {
			return fmt.Errorf("failed to get Autoscaling Group name from EC2 tags of instance '%s'", instanceId)
		}

		// get current sizes in the ASG
		asgParams := &autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []*string{aws.String(asgName)},
		}

		resp, err := n.asgClient.DescribeAutoScalingGroups(asgParams)
		if err != nil {
			return err
		}

		if len(resp.AutoScalingGroups) == 0 {
			return fmt.Errorf("failed to find ASG '%s'", asgName)
		}

		asg := resp.AutoScalingGroups[0]

		newDesired := aws.Int64Value(asg.DesiredCapacity) - 1
		minSize := aws.Int64Value(asg.MinSize)

		if 0 <= newDesired && 0 < minSize && newDesired < minSize {
			// decrement min size of ASG
			params := &autoscaling.UpdateAutoScalingGroupInput{
				AutoScalingGroupName: asg.AutoScalingGroupName,
				MinSize:              aws.Int64(newDesired),
			}

			_, err = n.asgClient.UpdateAutoScalingGroup(params)
			if err != nil {
				return err
			}
		}
	}

	params := &autoscaling.TerminateInstanceInAutoScalingGroupInput{
		InstanceId:                     aws.String(instanceId),
		ShouldDecrementDesiredCapacity: aws.Bool(decrementDesired),
	}

	terminateAsgInstance := func() error {
		state, err := n.instanceState(instanceId)
		if err != nil {
			return backoff.Permanent(err)
		}
		switch state {
		case ec2.InstanceStateNameRunning:
			_, err = n.asgClient.TerminateInstanceInAutoScalingGroup(params)
			if err != nil {
				return backoff.Permanent(err)
			}
			return errors.New("signalled instance to shutdown")
		case ec2.InstanceStateNameShuttingDown, ec2.InstanceStateNameStopping:
			return errors.New("instance shutting down")
		case ec2.InstanceStateNameTerminated, ec2.InstanceStateNameStopped:
			return nil
		default:
			return fmt.Errorf("unexpected instance state '%s'", state)
		}
	}
	// wait for the instance to be terminated/stopped
	backoffCfg := backoff.NewExponentialBackOff()
	backoffCfg.MaxElapsedTime = time.Duration(15) * time.Minute
	return backoff.Retry(terminateAsgInstance, backoffCfg)
}

// instanceState returns the current state of the instance e.g. 'terminated'.
// If no state is found it's assumed to be 'terminated'.
func (n *ASGNodePoolsBackend) instanceState(instanceId string) (string, error) {
	status, err := n.ec2Client.DescribeInstanceStatus(&ec2.DescribeInstanceStatusInput{
		IncludeAllInstances: aws.Bool(true),
		InstanceIds:         []*string{aws.String(instanceId)},
	})
	if err != nil {
		return "", err
	}

	// if we didn't find any instance status consider the instance
	// terminated
	if len(status.InstanceStatuses) == 0 {
		return ec2.InstanceStateNameTerminated, nil
	}

	return aws.StringValue(status.InstanceStatuses[0].InstanceState.Name), nil
}

// instanceIDFromProviderID extracts the EC2 instanceID from a Kubernetes
// ProviderID.
func instanceIDFromProviderID(providerID, az string) string {
	return strings.TrimPrefix(providerID, "aws:///"+az+"/")
}

// getNodePoolASGs returns a list of ASGs mapping to the specified node pool.
func (n *ASGNodePoolsBackend) getNodePoolASGs(nodePool *api.NodePool) ([]*autoscaling.Group, error) {
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

	var asgs []*autoscaling.Group
	err := n.asgClient.DescribeAutoScalingGroupsPages(params, func(resp *autoscaling.DescribeAutoScalingGroupsOutput, lastPage bool) bool {
		for _, group := range resp.AutoScalingGroups {
			if asgHasAllTags(expectedTags, group.Tags) {
				asgs = append(asgs, group)
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	if len(asgs) == 0 {
		return nil, fmt.Errorf("failed to find any ASGs for node pool '%s'", nodePool.Name)
	}

	return asgs, nil
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
	// return early if the ASG is empty
	if len(asg.Instances) == 0 {
		return nil, nil
	}

	launchParams := &asgLaunchParameters{}

	if asg.LaunchTemplate != nil && aws.StringValue(asg.LaunchTemplate.LaunchTemplateName) != "" {
		version := aws.StringValue(asg.LaunchTemplate.Version)

		// don't allow dynamic versions like $Default/$Latest
		if version == "" || strings.HasPrefix(version, "$") {
			return nil, fmt.Errorf("unsupported launch template version for ASG %s: %s", aws.StringValue(asg.AutoScalingGroupName), version)
		}
		launchParams.launchTemplateName = aws.StringValue(asg.LaunchTemplate.LaunchTemplateName)
		launchParams.launchTemplateVersion = version
	} else {
		launchConfig, err := n.getLaunchConfiguration(asg)
		if err != nil {
			return nil, err
		}

		launchParams.launchConfigurationInstanceType = aws.StringValue(launchConfig.InstanceType)
		launchParams.launchConfigurationAMI = aws.StringValue(launchConfig.ImageId)
		launchParams.launchConfigurationSpotPrice = aws.StringValue(launchConfig.SpotPrice)
		launchParams.launchConfigurationUserData = aws.StringValue(launchConfig.UserData)
		launchParams.instanceAMIs = make(map[string]string)

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
					launchParams.instanceAMIs[aws.StringValue(instance.InstanceId)] = aws.StringValue(instance.ImageId)
				}
			}
			return true
		})
		if err != nil {
			return nil, err
		}

	}

	oldInstances := make(map[string]bool)

	for _, instance := range asg.Instances {
		old, err := n.instancePendingUpgrade(launchParams, instance)
		if err != nil {
			return nil, err
		}

		if old {
			oldInstances[aws.StringValue(instance.InstanceId)] = true
		}
	}

	return oldInstances, nil
}

func (n *ASGNodePoolsBackend) instancePendingUpgrade(launchParams *asgLaunchParameters, instance *autoscaling.Instance) (bool, error) {
	if launchParams.launchTemplateName != "" {
		old := instance.LaunchTemplate == nil ||
			aws.StringValue(instance.LaunchTemplate.LaunchTemplateName) != launchParams.launchTemplateName ||
			aws.StringValue(instance.LaunchTemplate.Version) != launchParams.launchTemplateVersion
		return old, nil
	} else {
		params := &ec2.DescribeInstanceAttributeInput{
			Attribute:  aws.String(userDataAttribute),
			InstanceId: instance.InstanceId,
		}
		userDataResp, err := n.ec2Client.DescribeInstanceAttribute(params)
		if err != nil {
			return false, err
		}

		params.Attribute = aws.String(instanceTypeAttribute)
		instanceTypeResp, err := n.ec2Client.DescribeInstanceAttribute(params)
		if err != nil {
			return false, err
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
			return false, err
		}
		if len(spotPriceResp.SpotInstanceRequests) != 0 {
			instanceSpotPrice = spotPriceResp.SpotInstanceRequests[0].SpotPrice
		}

		spotPricesMatch, err := compareSpotPrices(launchParams.launchConfigurationSpotPrice, instanceSpotPrice)
		if err != nil {
			return false, err
		}

		// an instance is considered old when userdata, instance type
		// or AMI does not match what is in the Launch Configuration
		// for the ASG.
		old := aws.StringValue(userDataResp.UserData.Value) != launchParams.launchConfigurationUserData ||
			aws.StringValue(instanceTypeResp.InstanceType.Value) != launchParams.launchConfigurationInstanceType ||
			launchParams.instanceAMIs[aws.StringValue(instance.InstanceId)] != launchParams.launchConfigurationAMI ||
			!spotPricesMatch
		return old, nil
	}
}

func parseSpotPrice(spotPrice string) (float64, error) {
	if spotPrice == "" {
		return 0, nil
	}

	return strconv.ParseFloat(spotPrice, 64)
}

// compareSpotPrices returns true if spot prices are identical (either both are absent or both are present and equal
// in value. it's needed because AWS munges the spot price in some places, e.g. price on the launch configuration turns
// from 0.12 into 0.1200000 when read back from the API, but the same doesn't apply to the DescribeSpotInstanceRequests
// API
func compareSpotPrices(oldSpotPrice string, newSpotPrice *string) (bool, error) {
	parsedOld, err := parseSpotPrice(oldSpotPrice)
	if err != nil {
		return false, err
	}

	parsedNew, err := parseSpotPrice(aws.StringValue(newSpotPrice))
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
