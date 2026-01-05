package updatestrategy

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws/iface"
)

type mockASGAPI struct {
	iface.AutoScalingAPI
	err    error
	asgs   []autoscalingtypes.AutoScalingGroup
	descLB *autoscaling.DescribeLoadBalancersOutput
}

func (a *mockASGAPI) DescribeAutoScalingGroups(context.Context, *autoscaling.DescribeAutoScalingGroupsInput, ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: a.asgs}, a.err
}

func (a *mockASGAPI) UpdateAutoScalingGroup(context.Context, *autoscaling.UpdateAutoScalingGroupInput, ...func(*autoscaling.Options)) (*autoscaling.UpdateAutoScalingGroupOutput, error) {
	return nil, a.err
}

func (a *mockASGAPI) TerminateInstanceInAutoScalingGroup(context.Context, *autoscaling.TerminateInstanceInAutoScalingGroupInput, ...func(*autoscaling.Options)) (*autoscaling.TerminateInstanceInAutoScalingGroupOutput, error) {
	return nil, a.err
}

func (a *mockASGAPI) DescribeLoadBalancers(context.Context, *autoscaling.DescribeLoadBalancersInput, ...func(*autoscaling.Options)) (*autoscaling.DescribeLoadBalancersOutput, error) {
	return a.descLB, a.err
}

func (a *mockASGAPI) DeleteTags(context.Context, *autoscaling.DeleteTagsInput, ...func(*autoscaling.Options)) (*autoscaling.DeleteTagsOutput, error) {
	return nil, a.err
}

type mockEC2API struct {
	iface.EC2API
	err           error
	descStatus    *ec2.DescribeInstanceStatusOutput
	descLTVs      *ec2.DescribeLaunchTemplateVersionsOutput
	descTags      *ec2.DescribeTagsOutput
	descInstances *ec2.DescribeInstancesOutput
}

func (e *mockEC2API) DescribeLaunchTemplateVersions(context.Context, *ec2.DescribeLaunchTemplateVersionsInput, ...func(*ec2.Options)) (*ec2.DescribeLaunchTemplateVersionsOutput, error) {
	return e.descLTVs, e.err
}

func (e *mockEC2API) DescribeInstanceStatus(context.Context, *ec2.DescribeInstanceStatusInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceStatusOutput, error) {
	return e.descStatus, e.err
}

func (e *mockEC2API) DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if e.descInstances == nil {
		return &ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{},
		}, e.err
	}
	return e.descInstances, e.err
}

func (e *mockEC2API) DescribeTags(context.Context, *ec2.DescribeTagsInput, ...func(*ec2.Options)) (*ec2.DescribeTagsOutput, error) {
	return e.descTags, e.err
}

type mockELBAPI struct {
	iface.ELBAPI
	err                error
	descLBs            *elasticloadbalancing.DescribeLoadBalancersOutput
	descInstanceHealth *elasticloadbalancing.DescribeInstanceHealthOutput
}

func (e *mockELBAPI) DescribeLoadBalancers(context.Context, *elasticloadbalancing.DescribeLoadBalancersInput, ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DescribeLoadBalancersOutput, error) {
	return e.descLBs, e.err
}

func (e *mockELBAPI) DescribeInstanceHealth(context.Context, *elasticloadbalancing.DescribeInstanceHealthInput, ...func(*elasticloadbalancing.Options)) (*elasticloadbalancing.DescribeInstanceHealthOutput, error) {
	return e.descInstanceHealth, e.err
}

func TestGet(tt *testing.T) {
	for _, tc := range []struct {
		msg       string
		asgClient iface.AutoScalingAPI
		ec2Client iface.EC2API
		elbClient iface.ELBAPI
		success   bool
	}{
		{
			msg:       "test not returning any ASG",
			asgClient: &mockASGAPI{},
			ec2Client: &mockEC2API{},
			success:   false,
		},
		{
			msg: "test getting a new instance",
			asgClient: &mockASGAPI{
				asgs: []autoscalingtypes.AutoScalingGroup{
					{
						Tags: []autoscalingtypes.TagDescription{
							{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
							{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
						},
						Instances: []autoscalingtypes.Instance{
							{
								InstanceId: aws.String("instance_id"),
								LaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
									LaunchTemplateName: aws.String("launch_template"),
									Version:            aws.String("1"),
								},
							},
						},
						LaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
							LaunchTemplateName: aws.String("launch_template"),
							Version:            aws.String("2"),
						},
					},
				},
				descLB: &autoscaling.DescribeLoadBalancersOutput{
					LoadBalancers: []autoscalingtypes.LoadBalancerState{
						{
							LoadBalancerName: aws.String("foo"),
						},
					},
				},
			},
			ec2Client: &mockEC2API{
				descLTVs: &ec2.DescribeLaunchTemplateVersionsOutput{
					LaunchTemplateVersions: []ec2types.LaunchTemplateVersion{{
						LaunchTemplateData: &ec2types.ResponseLaunchTemplateData{
							InstanceType: ec2types.InstanceTypeM4Large,
						},
					}},
				},
			},
			elbClient: &mockELBAPI{
				descLBs: &elasticloadbalancing.DescribeLoadBalancersOutput{
					LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{
						{
							Instances: []elbtypes.Instance{
								{
									InstanceId: aws.String("foo"),
								},
							},
						},
					},
				},
				descInstanceHealth: &elasticloadbalancing.DescribeInstanceHealthOutput{
					InstanceStates: []elbtypes.InstanceState{
						{
							InstanceId: aws.String("foo"),
							State:      aws.String(string(autoscalingtypes.LifecycleStateInService)),
						},
					},
				},
			},
			success: true,
		},
		{
			msg: "test getting an old instance",
			asgClient: &mockASGAPI{
				asgs: []autoscalingtypes.AutoScalingGroup{
					{
						Tags: []autoscalingtypes.TagDescription{
							{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
							{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
						},
						Instances: []autoscalingtypes.Instance{
							{
								InstanceId: aws.String("instance_id"),
								LaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
									LaunchTemplateName: aws.String("launch_template"),
									Version:            aws.String("1"),
								},
							},
						},
						LaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
							LaunchTemplateName: aws.String("launch_template"),
							Version:            aws.String("1"),
						},
					},
				},
				descLB: &autoscaling.DescribeLoadBalancersOutput{
					LoadBalancers: []autoscalingtypes.LoadBalancerState{
						{
							LoadBalancerName: aws.String("foo"),
						},
					},
				},
			},
			ec2Client: &mockEC2API{
				descLTVs: &ec2.DescribeLaunchTemplateVersionsOutput{
					LaunchTemplateVersions: []ec2types.LaunchTemplateVersion{{
						LaunchTemplateData: &ec2types.ResponseLaunchTemplateData{
							InstanceType: ec2types.InstanceTypeM4Large,
						},
					}},
				},
			},
			elbClient: &mockELBAPI{
				descLBs: &elasticloadbalancing.DescribeLoadBalancersOutput{
					LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{
						{
							Instances: []elbtypes.Instance{
								{
									InstanceId: aws.String("foo"),
								},
							},
						},
					},
				},
				descInstanceHealth: &elasticloadbalancing.DescribeInstanceHealthOutput{
					InstanceStates: []elbtypes.InstanceState{
						{
							InstanceId: aws.String("foo"),
							State:      aws.String(string(autoscalingtypes.LifecycleStateInService)),
						},
					},
				},
			},
			success: true,
		},
	} {
		tt.Run(tc.msg, func(t *testing.T) {
			backend := &ASGNodePoolsBackend{
				asgClient: tc.asgClient,
				ec2Client: tc.ec2Client,
				elbClient: tc.elbClient,
				cluster:   &api.Cluster{},
			}

			_, err := backend.Get(context.Background(), &api.NodePool{Name: "test"})
			if tc.success {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestGetInstanceUpdateInfo(t *testing.T) {
	autoscalingInstance := func(id string, launchTemplateName string, launchTemplateVersion string) autoscalingtypes.Instance {
		return autoscalingtypes.Instance{
			InstanceId:       aws.String(id),
			AvailabilityZone: aws.String("eu-central-1"),
			LaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String(launchTemplateName),
				Version:            aws.String(launchTemplateVersion),
			},
		}
	}
	ec2Instance := func(id string, instanceType string, spot bool) ec2types.Instance {
		var spotRequestID *string
		if spot {
			spotRequestID = aws.String(fmt.Sprintf("spot-%s", id))
		}
		return ec2types.Instance{
			InstanceId:            aws.String(id),
			InstanceType:          ec2types.InstanceType(instanceType),
			SpotInstanceRequestId: spotRequestID,
		}
	}

	for _, tc := range []struct {
		name                       string
		autoscalingInstances       []autoscalingtypes.Instance
		autoscalingLaunchTemplate  *autoscalingtypes.LaunchTemplateSpecification
		autoscalingMixedInstPolicy *autoscalingtypes.MixedInstancesPolicy
		ec2LaunchTemplate          *ec2types.ResponseLaunchTemplateData
		ec2Instances               []ec2types.Instance
		expectedGenerations        map[string]int
	}{
		{
			name: "launch template, on demand, no change",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{
				InstanceType: ec2types.InstanceTypeM4Large,
			},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
			},
		},
		{
			name: "launch template, spot, no change",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{
				InstanceType: ec2types.InstanceTypeM4Large,
				InstanceMarketOptions: &ec2types.LaunchTemplateInstanceMarketOptions{
					MarketType: ec2types.MarketTypeSpot,
				},
			},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", true),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
			},
		},
		{
			name: "launch template, template changed",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "newlt", "2"),
			},
			autoscalingLaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("2"),
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{
				InstanceType: ec2types.InstanceTypeM4Large,
			},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": outdatedNodeGeneration,
				"aws:///eu-central-1/i-2": outdatedNodeGeneration,
			},
		},
		{
			name: "launch template, instance type changed",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{
				InstanceType: ec2types.InstanceTypeM4Large,
			},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", false),
				ec2Instance("i-2", "m5.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
				"aws:///eu-central-1/i-2": outdatedNodeGeneration,
			},
		},
		{
			name: "launch template, spot enabled",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{
				InstanceType: ec2types.InstanceTypeM4Large,
				InstanceMarketOptions: &ec2types.LaunchTemplateInstanceMarketOptions{
					MarketType: ec2types.MarketTypeSpot,
				},
			},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", true),
				ec2Instance("i-2", "m4.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
				"aws:///eu-central-1/i-2": outdatedNodeGeneration,
			},
		},
		{
			name: "launch template, spot disabled",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscalingtypes.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{
				InstanceType: ec2types.InstanceTypeM4Large,
			},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", false),
				ec2Instance("i-2", "m4.large", true),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
				"aws:///eu-central-1/i-2": outdatedNodeGeneration,
			},
		},
		{
			name: "mixed instances, on demand, no change",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscalingtypes.MixedInstancesPolicy{
				InstancesDistribution: &autoscalingtypes.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int32(100),
				},
				LaunchTemplate: &autoscalingtypes.LaunchTemplate{
					LaunchTemplateSpecification: &autoscalingtypes.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []autoscalingtypes.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{
				InstanceType: ec2types.InstanceTypeM4Large,
			},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", false),
				ec2Instance("i-2", "m5.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
				"aws:///eu-central-1/i-2": currentNodeGeneration,
			},
		},
		{
			name: "mixed instances, spot, no change",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscalingtypes.MixedInstancesPolicy{
				InstancesDistribution: &autoscalingtypes.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int32(0),
				},
				LaunchTemplate: &autoscalingtypes.LaunchTemplate{
					LaunchTemplateSpecification: &autoscalingtypes.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []autoscalingtypes.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", true),
				ec2Instance("i-2", "m5.large", true),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
				"aws:///eu-central-1/i-2": currentNodeGeneration,
			},
		},
		{
			name: "mixed instances, template changed",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "newlt", "2"),
			},
			autoscalingMixedInstPolicy: &autoscalingtypes.MixedInstancesPolicy{
				InstancesDistribution: &autoscalingtypes.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int32(100),
				},
				LaunchTemplate: &autoscalingtypes.LaunchTemplate{
					LaunchTemplateSpecification: &autoscalingtypes.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("2"),
					},
					Overrides: []autoscalingtypes.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", false),
				ec2Instance("i-2", "m5.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": outdatedNodeGeneration,
				"aws:///eu-central-1/i-2": outdatedNodeGeneration,
			},
		},
		{
			name: "mixed instances, instance type changed",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscalingtypes.MixedInstancesPolicy{
				InstancesDistribution: &autoscalingtypes.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int32(100),
				},
				LaunchTemplate: &autoscalingtypes.LaunchTemplate{
					LaunchTemplateSpecification: &autoscalingtypes.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []autoscalingtypes.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.xlarge")},
					},
				},
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", false),
				ec2Instance("i-2", "m5.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
				"aws:///eu-central-1/i-2": outdatedNodeGeneration,
			},
		},
		{
			name: "mixed instances, spot enabled",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscalingtypes.MixedInstancesPolicy{
				InstancesDistribution: &autoscalingtypes.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int32(0),
				},
				LaunchTemplate: &autoscalingtypes.LaunchTemplate{
					LaunchTemplateSpecification: &autoscalingtypes.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []autoscalingtypes.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", true),
				ec2Instance("i-2", "m5.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
				"aws:///eu-central-1/i-2": outdatedNodeGeneration,
			},
		},
		{
			name: "mixed instances, spot disabled",
			autoscalingInstances: []autoscalingtypes.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscalingtypes.MixedInstancesPolicy{
				InstancesDistribution: &autoscalingtypes.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int32(100),
				},
				LaunchTemplate: &autoscalingtypes.LaunchTemplate{
					LaunchTemplateSpecification: &autoscalingtypes.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []autoscalingtypes.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2types.ResponseLaunchTemplateData{},
			ec2Instances: []ec2types.Instance{
				ec2Instance("i-1", "m4.large", false),
				ec2Instance("i-2", "m5.large", true),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
				"aws:///eu-central-1/i-2": outdatedNodeGeneration,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &ASGNodePoolsBackend{
				asgClient: &mockASGAPI{
					asgs: []autoscalingtypes.AutoScalingGroup{
						{
							Tags: []autoscalingtypes.TagDescription{
								{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
								{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
							},
							Instances:            tc.autoscalingInstances,
							LaunchTemplate:       tc.autoscalingLaunchTemplate,
							MixedInstancesPolicy: tc.autoscalingMixedInstPolicy,
						},
					},
					descLB: &autoscaling.DescribeLoadBalancersOutput{
						LoadBalancers: []autoscalingtypes.LoadBalancerState{
							{
								LoadBalancerName: aws.String("foo"),
							},
						},
					},
				},
				ec2Client: &mockEC2API{
					descLTVs: &ec2.DescribeLaunchTemplateVersionsOutput{
						LaunchTemplateVersions: []ec2types.LaunchTemplateVersion{{
							LaunchTemplateData: tc.ec2LaunchTemplate,
						}},
					},
					descInstances: &ec2.DescribeInstancesOutput{
						Reservations: []ec2types.Reservation{
							{
								Instances: tc.ec2Instances,
							},
						},
					},
				},
				elbClient: &mockELBAPI{
					descLBs: &elasticloadbalancing.DescribeLoadBalancersOutput{
						LoadBalancerDescriptions: []elbtypes.LoadBalancerDescription{
							{
								Instances: []elbtypes.Instance{
									{
										InstanceId: aws.String("foo"),
									},
								},
							},
						},
					},
					descInstanceHealth: &elasticloadbalancing.DescribeInstanceHealthOutput{
						InstanceStates: []elbtypes.InstanceState{
							{
								InstanceId: aws.String("foo"),
								State:      aws.String(string(autoscalingtypes.LifecycleStateInService)),
							},
						},
					},
				},
				cluster: &api.Cluster{},
			}

			res, err := backend.Get(context.Background(), &api.NodePool{Name: "test"})
			require.NoError(t, err)

			instanceGenerations := make(map[string]int)
			for _, node := range res.Nodes {
				instanceGenerations[node.ProviderID] = node.Generation
			}
			require.Equal(t, tc.expectedGenerations, instanceGenerations)
		})
	}
}

func TestScale(t *testing.T) {
	// test not getting the ASGs
	backend := &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{},
		cluster:   &api.Cluster{},
	}
	err := backend.Scale(context.Background(), &api.NodePool{Name: "test"}, 10)
	assert.Error(t, err)

	// test scaling up
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{
			asgs: []autoscalingtypes.AutoScalingGroup{
				{
					Tags: []autoscalingtypes.TagDescription{
						{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
						{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
					},
					Instances: []autoscalingtypes.Instance{
						{},
					},
					DesiredCapacity: aws.Int32(1),
				},
				{
					Tags: []autoscalingtypes.TagDescription{
						{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
						{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
					},
					Instances: []autoscalingtypes.Instance{
						{},
					},
					DesiredCapacity: aws.Int32(1),
				},
			},
		},
		cluster: &api.Cluster{},
	}
	err = backend.Scale(context.Background(), &api.NodePool{Name: "test"}, 10)
	assert.NoError(t, err)

	// test scaling down
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{
			asgs: []autoscalingtypes.AutoScalingGroup{
				{
					Tags: []autoscalingtypes.TagDescription{
						{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
						{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
					},
					Instances: []autoscalingtypes.Instance{
						{},
					},
					DesiredCapacity: aws.Int32(1),
				},
				{
					Tags: []autoscalingtypes.TagDescription{
						{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
						{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
					},
					Instances: []autoscalingtypes.Instance{
						{},
					},
					DesiredCapacity: aws.Int32(1),
				},
			},
		},
		cluster: &api.Cluster{},
	}
	err = backend.Scale(context.Background(), &api.NodePool{Name: "test"}, 1)
	assert.NoError(t, err)

	// test getting error
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{err: errors.New("failed")},
		ec2Client: &mockEC2API{err: errors.New("failed")},
	}
	err = backend.Terminate(context.Background(), nil, &Node{}, true)
	assert.Error(t, err)
}

func TestDeleteTags(tt *testing.T) {
	for _, tc := range []struct {
		msg       string
		asgClient iface.AutoScalingAPI
		nodePool  *api.NodePool
		tags      map[string]string
		success   bool
	}{
		{
			msg: "test removing tags",
			asgClient: &mockASGAPI{
				asgs: []autoscalingtypes.AutoScalingGroup{
					{
						Tags: []autoscalingtypes.TagDescription{
							{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
							{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
							{Key: aws.String("tag-to-remove"), Value: aws.String("test")},
						},
						Instances: []autoscalingtypes.Instance{
							{},
						},
					},
				},
			},
			tags: map[string]string{
				"tag-to-remove": "test",
			},
			nodePool: &api.NodePool{Name: "test"},
			success:  true,
		},
		{
			msg:       "test errors when getting asg",
			asgClient: &mockASGAPI{err: errors.New("failed")},
			nodePool: &api.NodePool{
				Name:    "test",
				MinSize: 2,
				MaxSize: 2,
			},
			success: false,
		},
	} {
		tt.Run(tc.msg, func(t *testing.T) {
			backend := &ASGNodePoolsBackend{
				asgClient: tc.asgClient,
				cluster:   &api.Cluster{},
			}

			err := backend.deleteTags(context.Background(), tc.nodePool, tc.tags)
			if tc.success {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTerminate(t *testing.T) {
	// test success
	backend := &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{
			asgs: []autoscalingtypes.AutoScalingGroup{
				{
					AutoScalingGroupName: aws.String("asg-name"),
					DesiredCapacity:      aws.Int32(3),
					MinSize:              aws.Int32(3),
				},
			},
		},
		ec2Client: &mockEC2API{
			descTags: &ec2.DescribeTagsOutput{
				Tags: []ec2types.TagDescription{
					{
						Key:   aws.String(ec2AutoscalingGroupTagKey),
						Value: aws.String("asg-name"),
					},
				},
			},
			descStatus: &ec2.DescribeInstanceStatusOutput{
				InstanceStatuses: []ec2types.InstanceStatus{
					{
						InstanceState: &ec2types.InstanceState{
							Code: aws.Int32(48), // terminated
							Name: ec2types.InstanceStateNameTerminated,
						},
					},
				},
			},
		},
	}
	err := backend.Terminate(context.Background(), nil, &Node{}, true)
	assert.NoError(t, err)

	// test getting error
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{err: errors.New("failed")},
		ec2Client: &mockEC2API{err: errors.New("failed")},
	}
	err = backend.Terminate(context.Background(), nil, &Node{}, true)
	assert.Error(t, err)

	// test already terminated
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{err: errors.New("already terminated")},
		ec2Client: &mockEC2API{descStatus: &ec2.DescribeInstanceStatusOutput{
			InstanceStatuses: []ec2types.InstanceStatus{
				{
					InstanceState: &ec2types.InstanceState{
						Code: aws.Int32(48), // terminated
						Name: ec2types.InstanceStateNameTerminated,
					},
				},
			},
		}},
	}
	err = backend.Terminate(context.Background(), nil, &Node{}, false)
	assert.NoError(t, err)
}

func TestAsgHasAllTags(t *testing.T) {
	expected := []autoscalingtypes.TagDescription{
		{Key: aws.String("key-1"), Value: aws.String("value-1")},
		{Key: aws.String("key-2"), Value: aws.String("value-2")},
	}
	tags := []autoscalingtypes.TagDescription{
		{Key: aws.String("key-2"), Value: aws.String("value-2")},
		{Key: aws.String("key-1"), Value: aws.String("value-1")},
	}
	assert.True(t, asgHasAllTags(expected, tags))
}

func TestNotMatchingAsgHasAllTags(t *testing.T) {
	expected := []autoscalingtypes.TagDescription{
		{Key: aws.String("key"), Value: aws.String("value")},
	}
	tags := []autoscalingtypes.TagDescription{
		{Key: aws.String("key"), Value: aws.String("bar")},
	}
	assert.False(t, asgHasAllTags(expected, tags))

	expected = []autoscalingtypes.TagDescription{
		{Key: aws.String("key"), Value: aws.String("value")},
		{Key: aws.String("foo"), Value: aws.String("bar")},
	}
	assert.False(t, asgHasAllTags(expected, tags))
}

func TestInstanceIDFromProviderID(t *testing.T) {
	providerID := "aws:///eu-central-1a/i-abc"
	instanceID := "i-abc"
	az := "eu-central-1a"
	assert.Equal(t, instanceID, instanceIDFromProviderID(providerID, az))

	invalidFormat := "aws:///i-abc"
	assert.Equal(t, invalidFormat, instanceIDFromProviderID(invalidFormat, az))
}
