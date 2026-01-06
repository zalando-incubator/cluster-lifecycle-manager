package updatestrategy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	"github.com/cenkalti/backoff"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws/iface"
)

const (
	clusterIDTagPrefix               = "kubernetes.io/cluster/"
	resourceLifecycleOwned           = "owned"
	kubeAutoScalerEnabledTagKey      = "k8s.io/cluster-autoscaler/enabled"
	nodePoolTagLegacy                = "NodePool"
	nodePoolTag                      = "node.kubernetes.io/node-pool"
	instanceHealthStatusHealthy      = "Healthy"
	ec2AutoscalingGroupTagKey        = "aws:autoscaling:groupName"
	instanceTerminationRetryDuration = time.Duration(15) * time.Minute
)

const (
	outdatedNodeGeneration = iota
	currentNodeGeneration
)

type asgLaunchParameters struct {
	launchTemplateName    string
	launchTemplateVersion string
	instanceTypes         map[string]struct{}
	spot                  bool
}

// ASGNodePoolsBackend defines a node pool backed by an AWS Auto Scaling Group.
type ASGNodePoolsBackend struct {
	asgClient iface.AutoScalingAPI
	ec2Client iface.EC2API
	elbClient iface.ELBAPI
	cluster   *api.Cluster
}

// NewASGNodePoolsBackend initializes a new ASGNodePoolsBackend for the given cluster and AWS
// session and.
func NewASGNodePoolsBackend(cluster *api.Cluster, cfg aws.Config) *ASGNodePoolsBackend {
	return &ASGNodePoolsBackend{
		asgClient: autoscaling.NewFromConfig(cfg),
		ec2Client: ec2.NewFromConfig(cfg),
		elbClient: elasticloadbalancing.NewFromConfig(cfg),
		cluster:   cluster,
	}
}

// Get gets the ASG matching to the node pool and gets all instances from the
// ASG. The node generation is set to 'current' for nodes with the latest
// launch configuration and 'outdated' for nodes with an older launch
// configuration.
func (n *ASGNodePoolsBackend) Get(ctx context.Context, nodePool *api.NodePool) (*NodePool, error) {
	asgs, err := n.getNodePoolASGs(ctx, nodePool)
	if err != nil {
		return nil, err
	}

	nodes := make([]*Node, 0)
	minSize := 0
	maxSize := 0
	desiredCapacity := 0
	for _, asg := range asgs {
		minSize += int(aws.ToInt32(asg.MinSize))
		maxSize += int(aws.ToInt32(asg.MaxSize))
		desiredCapacity += int(aws.ToInt32(asg.DesiredCapacity))

		oldInstances, err := n.getInstancesToUpdate(ctx, asg)
		if err != nil {
			return nil, err
		}

		lbInstances, err := n.getLoadBalancerAttachedInstancesReadiness(ctx, asg)
		if err != nil {
			return nil, err
		}

		// TODO: also lookup target groups for ALBs attached to the ASG (for Ingress)

		for _, instance := range asg.Instances {
			instanceID := aws.ToString(instance.InstanceId)
			node := &Node{
				ProviderID:    fmt.Sprintf("aws:///%s/%s", aws.ToString(instance.AvailabilityZone), instanceID),
				FailureDomain: aws.ToString(instance.AvailabilityZone),
				Generation:    currentNodeGeneration,
				Ready:         aws.ToString(instance.HealthStatus) == instanceHealthStatusHealthy && instance.LifecycleState == autoscalingtypes.LifecycleStateInService,
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
func (n *ASGNodePoolsBackend) Scale(ctx context.Context, nodePool *api.NodePool, replicas int) error {
	asgs, err := n.getNodePoolASGs(ctx, nodePool)
	if err != nil {
		return err
	}

	desired := 0
	for _, asg := range asgs {
		desired += int(aws.ToInt32(asg.DesiredCapacity))
	}

	diff := replicas - desired

	if diff == 0 {
		// nothing to change
		return nil
	}

	// add nodes to smallest non-empty asgs
	if diff > 0 {
		sort.Slice(asgs, func(i, j int) bool {
			iCap := aws.ToInt32(asgs[i].DesiredCapacity)
			jCap := aws.ToInt32(asgs[j].DesiredCapacity)

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
				asg.DesiredCapacity = aws.Int32(aws.ToInt32(asg.DesiredCapacity) + 1)
				diff--
			}
		}
	} else if diff < 0 { // remove nodes from biggest asgs
		sort.Slice(asgs, func(i, j int) bool {
			return aws.ToInt32(asgs[i].DesiredCapacity) > aws.ToInt32(asgs[j].DesiredCapacity)
		})

	LoopDecrement:
		for {
			for _, asg := range asgs {
				if diff >= 0 {
					break LoopDecrement
				}
				asg.DesiredCapacity = aws.Int32(aws.ToInt32(asg.DesiredCapacity) - 1)
				diff++
			}
		}
	}

	for _, asg := range asgs {
		minSize := int32(math.Min(float64(aws.ToInt32(asg.DesiredCapacity)), float64(aws.ToInt32(asg.MinSize))))
		maxSize := int32(math.Max(float64(aws.ToInt32(asg.DesiredCapacity)), float64(aws.ToInt32(asg.MaxSize))))

		params := &autoscaling.UpdateAutoScalingGroupInput{
			AutoScalingGroupName: asg.AutoScalingGroupName,
			DesiredCapacity:      asg.DesiredCapacity,
			MinSize:              aws.Int32(minSize),
			MaxSize:              aws.Int32(maxSize),
		}

		_, err := n.asgClient.UpdateAutoScalingGroup(ctx, params)
		if err != nil {
			return err
		}
	}

	return nil
}

// MarkForDecommission suspends autoscaling of the node pool if it was enabled and makes sure that the pool can be
// scaled down to 0.
// The implementation assumes the kubernetes cluster-autoscaler is used so it just removes a tag.
func (n *ASGNodePoolsBackend) MarkForDecommission(ctx context.Context, nodePool *api.NodePool) error {
	tags := map[string]string{
		kubeAutoScalerEnabledTagKey: "",
	}
	err := n.deleteTags(ctx, nodePool, tags)
	if err != nil {
		return err
	}

	asgs, err := n.getNodePoolASGs(ctx, nodePool)
	if err != nil {
		return err
	}

	for _, asg := range asgs {
		if aws.ToInt32(asg.MinSize) > 0 {
			params := &autoscaling.UpdateAutoScalingGroupInput{
				AutoScalingGroupName: asg.AutoScalingGroupName,
				MinSize:              aws.Int32(0),
			}

			_, err := n.asgClient.UpdateAutoScalingGroup(ctx, params)
			if err != nil {
				return err
			}

		}
	}

	return nil
}

// deleteTags deletes the specified tags from the node pool ASGs.
func (n *ASGNodePoolsBackend) deleteTags(ctx context.Context, nodePool *api.NodePool, tags map[string]string) error {
	asgs, err := n.getNodePoolASGs(ctx, nodePool)
	if err != nil {
		return err
	}

	for _, asg := range asgs {
		asgTags := make([]autoscalingtypes.Tag, 0, len(tags))

		for key, val := range tags {
			tag := autoscalingtypes.Tag{
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

		_, err := n.asgClient.DeleteTags(ctx, params)
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
func (n *ASGNodePoolsBackend) Terminate(ctx context.Context, _ *api.NodePool, node *Node, decrementDesired bool) error {
	instanceID := instanceIDFromProviderID(node.ProviderID, node.FailureDomain)

	// if desired should be decremented check if we also need to decrement
	// the minSize of the ASG.
	if decrementDesired {
		// lookup ASG name in the EC2 tags of the instance
		var asgName string
		params := &ec2.DescribeTagsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("resource-id"),
					Values: []string{instanceID},
				},
				{
					Name:   aws.String("key"),
					Values: []string{ec2AutoscalingGroupTagKey},
				},
			},
		}
		paginator := ec2.NewDescribeTagsPaginator(n.ec2Client, params)
		for paginator.HasMorePages() {
			resp, err := paginator.NextPage(ctx)
			if err != nil {
				return err
			}
			for _, tag := range resp.Tags {
				if aws.ToString(tag.Key) == ec2AutoscalingGroupTagKey {
					asgName = aws.ToString(tag.Value)
					break
				}
			}
		}

		if asgName == "" {
			return fmt.Errorf("failed to get Autoscaling Group name from EC2 tags of instance '%s'", instanceID)
		}

		// get current sizes in the ASG
		asgParams := &autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []string{asgName},
		}

		resp, err := n.asgClient.DescribeAutoScalingGroups(ctx, asgParams)
		if err != nil {
			return err
		}

		if len(resp.AutoScalingGroups) == 0 {
			return fmt.Errorf("failed to find ASG '%s'", asgName)
		}

		asg := resp.AutoScalingGroups[0]

		newDesired := aws.ToInt32(asg.DesiredCapacity) - 1
		minSize := aws.ToInt32(asg.MinSize)

		if 0 <= newDesired && 0 < minSize && newDesired < minSize {
			// decrement min size of ASG
			params := &autoscaling.UpdateAutoScalingGroupInput{
				AutoScalingGroupName: asg.AutoScalingGroupName,
				MinSize:              aws.Int32(newDesired),
			}

			_, err = n.asgClient.UpdateAutoScalingGroup(ctx, params)
			if err != nil {
				return err
			}
		}
	}

	params := &autoscaling.TerminateInstanceInAutoScalingGroupInput{
		InstanceId:                     aws.String(instanceID),
		ShouldDecrementDesiredCapacity: aws.Bool(decrementDesired),
	}

	terminateAsgInstance := func() error {
		state, err := n.instanceState(ctx, instanceID)
		if err != nil {
			return backoff.Permanent(err)
		}
		switch state {
		// if instance is running terminate it
		case ec2types.InstanceStateNameRunning:
			_, err = n.asgClient.TerminateInstanceInAutoScalingGroup(ctx, params)
			if err != nil {
				// TODO:
				// if aerr, ok := err.(awserr.Error); ok {
				// 	switch aerr.Code() {
				// 	// if an operation is in progress then retry later.
				// 	case autoscaling.ErrCodeScalingActivityInProgressFault, autoscaling.ErrCodeResourceContentionFault:
				// 		return errors.New("waiting for AWS to complete operations")
				// 		// otherwise fail
				// 	default:
				// 		return backoff.Permanent(err)
				// 	}
				// }
				// call to API failed. probably transient error.
				return err
			}
			return errors.New("signalled instance to shutdown")
		case ec2types.InstanceStateNameShuttingDown, ec2types.InstanceStateNameStopping:
			return errors.New("instance shutting down")
		case ec2types.InstanceStateNameTerminated, ec2types.InstanceStateNameStopped:
			return nil
		default:
			return fmt.Errorf("unexpected instance state '%s'", state)
		}
	}
	// wait for the instance to be terminated/stopped
	backoffCfg := backoff.NewExponentialBackOff()
	backoffCfg.MaxElapsedTime = instanceTerminationRetryDuration
	return backoff.Retry(terminateAsgInstance, backoffCfg)
}

// instanceState returns the current state of the instance e.g. 'terminated'.
// If no state is found it's assumed to be 'terminated'.
func (n *ASGNodePoolsBackend) instanceState(ctx context.Context, instanceID string) (ec2types.InstanceStateName, error) {
	status, err := n.ec2Client.DescribeInstanceStatus(ctx, &ec2.DescribeInstanceStatusInput{
		IncludeAllInstances: aws.Bool(true),
		InstanceIds:         []string{instanceID},
	})
	if err != nil {
		return "", err
	}

	// if we didn't find any instance status consider the instance
	// terminated
	if len(status.InstanceStatuses) == 0 {
		return ec2types.InstanceStateNameTerminated, nil
	}

	return status.InstanceStatuses[0].InstanceState.Name, nil
}

// instanceIDFromProviderID extracts the EC2 instanceID from a Kubernetes
// ProviderID.
func instanceIDFromProviderID(providerID, az string) string {
	return strings.TrimPrefix(providerID, "aws:///"+az+"/")
}

// getNodePoolASGs returns a list of ASGs mapping to the specified node pool.
func (n *ASGNodePoolsBackend) getNodePoolASGs(ctx context.Context, nodePool *api.NodePool) ([]autoscalingtypes.AutoScalingGroup, error) {
	params := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{},
	}

	expectedTags := []autoscalingtypes.TagDescription{
		{
			Key:   aws.String(clusterIDTagPrefix + n.cluster.Name()),
			Value: aws.String(resourceLifecycleOwned),
		},
		{
			Key:   aws.String(nodePoolTagLegacy),
			Value: aws.String(nodePool.Name),
		},
	}

	var asgs []autoscalingtypes.AutoScalingGroup
	paginator := autoscaling.NewDescribeAutoScalingGroupsPaginator(n.asgClient, params)
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, group := range resp.AutoScalingGroups {
			if asgHasAllTags(expectedTags, group.Tags) {
				asgs = append(asgs, group)
			}
		}
	}

	if len(asgs) == 0 {
		return nil, fmt.Errorf("failed to find any ASGs for node pool '%s'", nodePool.Name)
	}

	return asgs, nil
}

// getInstancesToUpdate returns a list of instances with outdated userData.
func (n *ASGNodePoolsBackend) getInstancesToUpdate(ctx context.Context, asg autoscalingtypes.AutoScalingGroup) (map[string]bool, error) {
	// return early if the ASG is empty
	if len(asg.Instances) == 0 {
		return nil, nil
	}

	launchParams := &asgLaunchParameters{
		instanceTypes: make(map[string]struct{}),
	}

	if asg.MixedInstancesPolicy != nil {
		launchTemplateSpecification := asg.MixedInstancesPolicy.LaunchTemplate.LaunchTemplateSpecification
		launchParams.launchTemplateName = aws.ToString(launchTemplateSpecification.LaunchTemplateName)
		launchParams.launchTemplateVersion = aws.ToString(launchTemplateSpecification.Version)

		distribution := asg.MixedInstancesPolicy.InstancesDistribution
		if aws.ToInt32(distribution.OnDemandBaseCapacity) != 0 {
			return nil, fmt.Errorf("invalid MixedInstancesPolicy for ASG %s: InstancesDistribution.OnDemandBasedCapacity cannot be 0", aws.ToString(asg.AutoScalingGroupName))
		}

		switch aws.ToInt32(asg.MixedInstancesPolicy.InstancesDistribution.OnDemandPercentageAboveBaseCapacity) {
		case 0:
			launchParams.spot = true
		case 100:
			launchParams.spot = false
		default:
			return nil, fmt.Errorf("invalid MixedInstancesPolicy for ASG %s: InstancesDistribution.OnDemandPercentageAboveBaseCapacity can be either 0 or 100", aws.ToString(asg.AutoScalingGroupName))
		}

		for _, override := range asg.MixedInstancesPolicy.LaunchTemplate.Overrides {
			launchParams.instanceTypes[aws.ToString(override.InstanceType)] = struct{}{}
		}
	} else if asg.LaunchTemplate != nil {
		launchParams.launchTemplateName = aws.ToString(asg.LaunchTemplate.LaunchTemplateName)
		launchParams.launchTemplateVersion = aws.ToString(asg.LaunchTemplate.Version)

		resp, err := n.ec2Client.DescribeLaunchTemplateVersions(ctx, &ec2.DescribeLaunchTemplateVersionsInput{
			LaunchTemplateName: aws.String(launchParams.launchTemplateName),
			Versions:           []string{launchParams.launchTemplateVersion},
		})
		if err != nil {
			return nil, fmt.Errorf("unable to fetch launch template version %s for ASG %s: %v", launchParams.launchTemplateVersion, aws.ToString(asg.AutoScalingGroupName), err)
		}
		if len(resp.LaunchTemplateVersions) == 0 {
			return nil, fmt.Errorf("unable to find launch template version %s for ASG %s", launchParams.launchTemplateVersion, aws.ToString(asg.AutoScalingGroupName))
		}
		version := resp.LaunchTemplateVersions[0]

		launchParams.instanceTypes[string(version.LaunchTemplateData.InstanceType)] = struct{}{}
		launchParams.spot = version.LaunchTemplateData.InstanceMarketOptions != nil && version.LaunchTemplateData.InstanceMarketOptions.MarketType == ec2types.MarketTypeSpot
	}

	if launchParams.launchTemplateName == "" {
		return nil, fmt.Errorf("unable to determine launch template for ASG %s", aws.ToString(asg.AutoScalingGroupName))
	}

	// don't allow dynamic versions like $Default/$Latest
	if launchParams.launchTemplateVersion == "" || strings.HasPrefix(launchParams.launchTemplateVersion, "$") {
		return nil, fmt.Errorf("unsupported launch template version for ASG %s: %s", aws.ToString(asg.AutoScalingGroupName), launchParams.launchTemplateVersion)
	}

	oldInstances := make(map[string]bool)

	describeParams := &ec2.DescribeInstancesInput{}
	for _, asgInstance := range asg.Instances {
		if aws.ToString(asgInstance.LaunchTemplate.LaunchTemplateName) != launchParams.launchTemplateName || aws.ToString(asgInstance.LaunchTemplate.Version) != launchParams.launchTemplateVersion {
			oldInstances[aws.ToString(asgInstance.InstanceId)] = true
		}

		describeParams.InstanceIds = append(describeParams.InstanceIds, aws.ToString(asgInstance.InstanceId))
	}

	// figure out if instance types or spotness changed
	paginator := ec2.NewDescribeInstancesPaginator(n.ec2Client, describeParams)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to fetch instance information for ASG %s: %v", aws.ToString(asg.AutoScalingGroupName), err)
		}
		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				_, typeValid := launchParams.instanceTypes[string(instance.InstanceType)]
				spotInstance := instance.SpotInstanceRequestId != nil
				if !typeValid || spotInstance != launchParams.spot {
					oldInstances[aws.ToString(instance.InstanceId)] = true
				}
			}
		}
	}

	return oldInstances, nil
}

// getLoadBalancerAttachedInstancesReadiness returns a mapping of instanceId ->
// readiness based on the instance state in any load balancers attached to the
// ASG. The instance must be 'InService' in all load balancers that it is
// attached to, to be considered ready.
func (n *ASGNodePoolsBackend) getLoadBalancerAttachedInstancesReadiness(ctx context.Context, asg autoscalingtypes.AutoScalingGroup) (map[string]bool, error) {
	params := &autoscaling.DescribeLoadBalancersInput{
		AutoScalingGroupName: asg.AutoScalingGroupName,
	}

	resp, err := n.asgClient.DescribeLoadBalancers(ctx, params)
	if err != nil {
		return nil, err
	}

	instanceReadiness := make(map[string]bool)
	for _, lbState := range resp.LoadBalancers {
		// TODO: optimize calls based on state
		params := &elasticloadbalancing.DescribeLoadBalancersInput{
			LoadBalancerNames: []string{aws.ToString(lbState.LoadBalancerName)},
		}

		resp, err := n.elbClient.DescribeLoadBalancers(ctx, params)
		if err != nil {
			return nil, err
		}

		if len(resp.LoadBalancerDescriptions) != 1 {
			return nil, fmt.Errorf("expected 1 load balancer, found %d", len(resp.LoadBalancerDescriptions))
		}

		healthParams := &elasticloadbalancing.DescribeInstanceHealthInput{
			LoadBalancerName: lbState.LoadBalancerName,
			Instances:        resp.LoadBalancerDescriptions[0].Instances,
		}

		healthResp, err := n.elbClient.DescribeInstanceHealth(ctx, healthParams)
		if err != nil {
			return nil, err
		}

		for _, state := range healthResp.InstanceStates {
			inService := autoscalingtypes.LifecycleState(aws.ToString(state.State)) == autoscalingtypes.LifecycleStateInService
			if ready, ok := instanceReadiness[aws.ToString(state.InstanceId)]; ok {
				if ready {
					instanceReadiness[aws.ToString(state.InstanceId)] = inService
				}
			} else {
				instanceReadiness[aws.ToString(state.InstanceId)] = inService
			}
		}
	}

	return instanceReadiness, nil
}

// asgHasAllTags returns true if the asg tags matches the expected tags.
// autoscaling tag keys are unique
func asgHasAllTags(expected, tags []autoscalingtypes.TagDescription) bool {
	if len(expected) > len(tags) {
		return false
	}

	matching := 0

	for _, e := range expected {
		for _, tag := range tags {
			if aws.ToString(e.Key) == aws.ToString(tag.Key) &&
				aws.ToString(e.Value) == aws.ToString(tag.Value) {
				matching++
			}
		}
	}

	return matching == len(expected)
}
