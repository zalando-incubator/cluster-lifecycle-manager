package updatestrategy

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

type mockASGAPI struct {
	autoscalingiface.AutoScalingAPI
	err    error
	asgs   []*autoscaling.Group
	descLB *autoscaling.DescribeLoadBalancersOutput
}

func (a *mockASGAPI) DescribeAutoScalingGroupsPages(input *autoscaling.DescribeAutoScalingGroupsInput, fn func(*autoscaling.DescribeAutoScalingGroupsOutput, bool) bool) error {
	fn(&autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: a.asgs}, true)
	return a.err
}

func (a *mockASGAPI) DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: a.asgs}, a.err
}

func (a *mockASGAPI) UpdateAutoScalingGroup(input *autoscaling.UpdateAutoScalingGroupInput) (*autoscaling.UpdateAutoScalingGroupOutput, error) {
	return nil, a.err
}

func (a *mockASGAPI) TerminateInstanceInAutoScalingGroup(*autoscaling.TerminateInstanceInAutoScalingGroupInput) (*autoscaling.TerminateInstanceInAutoScalingGroupOutput, error) {
	return nil, a.err
}

func (a *mockASGAPI) DescribeLoadBalancers(input *autoscaling.DescribeLoadBalancersInput) (*autoscaling.DescribeLoadBalancersOutput, error) {
	return a.descLB, a.err
}

func (a *mockASGAPI) DeleteTags(input *autoscaling.DeleteTagsInput) (*autoscaling.DeleteTagsOutput, error) {
	return nil, a.err
}

type mockEC2API struct {
	ec2iface.EC2API
	err           error
	descStatus    *ec2.DescribeInstanceStatusOutput
	descLTVs      *ec2.DescribeLaunchTemplateVersionsOutput
	descTags      *ec2.DescribeTagsOutput
	descInstances *ec2.DescribeInstancesOutput
}

func (e *mockEC2API) DescribeLaunchTemplateVersions(input *ec2.DescribeLaunchTemplateVersionsInput) (*ec2.DescribeLaunchTemplateVersionsOutput, error) {
	return e.descLTVs, e.err
}

func (e *mockEC2API) DescribeInstanceStatus(input *ec2.DescribeInstanceStatusInput) (*ec2.DescribeInstanceStatusOutput, error) {
	return e.descStatus, e.err
}

func (e *mockEC2API) DescribeInstancesPages(input *ec2.DescribeInstancesInput, fn func(*ec2.DescribeInstancesOutput, bool) bool) error {
	if e.err != nil {
		return e.err
	}
	if e.descInstances != nil {
		fn(e.descInstances, true)
	}
	return nil
}

func (e *mockEC2API) DescribeTagsPages(input *ec2.DescribeTagsInput, fn func(*ec2.DescribeTagsOutput, bool) bool) error {
	if e.err != nil {
		return e.err
	}
	fn(e.descTags, true)
	return nil
}

type mockELBAPI struct {
	elbiface.ELBAPI
	err                error
	descLBs            *elb.DescribeLoadBalancersOutput
	descInstanceHealth *elb.DescribeInstanceHealthOutput
}

func (e *mockELBAPI) DescribeLoadBalancers(input *elb.DescribeLoadBalancersInput) (*elb.DescribeLoadBalancersOutput, error) {
	return e.descLBs, e.err
}

func (e *mockELBAPI) DescribeInstanceHealth(input *elb.DescribeInstanceHealthInput) (*elb.DescribeInstanceHealthOutput, error) {
	return e.descInstanceHealth, e.err
}

func TestGet(tt *testing.T) {
	for _, tc := range []struct {
		msg       string
		asgClient autoscalingiface.AutoScalingAPI
		ec2Client ec2iface.EC2API
		elbClient elbiface.ELBAPI
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
				asgs: []*autoscaling.Group{
					{
						Tags: []*autoscaling.TagDescription{
							{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
							{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
						},
						Instances: []*autoscaling.Instance{
							{
								InstanceId: aws.String("instance_id"),
								LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
									LaunchTemplateName: aws.String("launch_template"),
									Version:            aws.String("1"),
								},
							},
						},
						LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
							LaunchTemplateName: aws.String("launch_template"),
							Version:            aws.String("2"),
						},
					},
				},
				descLB: &autoscaling.DescribeLoadBalancersOutput{
					LoadBalancers: []*autoscaling.LoadBalancerState{
						{
							LoadBalancerName: aws.String("foo"),
						},
					},
				},
			},
			ec2Client: &mockEC2API{
				descLTVs: &ec2.DescribeLaunchTemplateVersionsOutput{
					LaunchTemplateVersions: []*ec2.LaunchTemplateVersion{{
						LaunchTemplateData: &ec2.ResponseLaunchTemplateData{
							InstanceType: aws.String("m4.large"),
						},
					}},
				},
			},
			elbClient: &mockELBAPI{
				descLBs: &elb.DescribeLoadBalancersOutput{
					LoadBalancerDescriptions: []*elb.LoadBalancerDescription{
						{
							Instances: []*elb.Instance{
								{
									InstanceId: aws.String("foo"),
								},
							},
						},
					},
				},
				descInstanceHealth: &elb.DescribeInstanceHealthOutput{
					InstanceStates: []*elb.InstanceState{
						{
							InstanceId: aws.String("foo"),
							State:      aws.String(autoscaling.LifecycleStateInService),
						},
					},
				},
			},
			success: true,
		},
		{
			msg: "test getting an old instance",
			asgClient: &mockASGAPI{
				asgs: []*autoscaling.Group{
					{
						Tags: []*autoscaling.TagDescription{
							{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
							{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
						},
						Instances: []*autoscaling.Instance{
							{
								InstanceId: aws.String("instance_id"),
								LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
									LaunchTemplateName: aws.String("launch_template"),
									Version:            aws.String("1"),
								},
							},
						},
						LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
							LaunchTemplateName: aws.String("launch_template"),
							Version:            aws.String("1"),
						},
					},
				},
				descLB: &autoscaling.DescribeLoadBalancersOutput{
					LoadBalancers: []*autoscaling.LoadBalancerState{
						{
							LoadBalancerName: aws.String("foo"),
						},
					},
				},
			},
			ec2Client: &mockEC2API{
				descLTVs: &ec2.DescribeLaunchTemplateVersionsOutput{
					LaunchTemplateVersions: []*ec2.LaunchTemplateVersion{{
						LaunchTemplateData: &ec2.ResponseLaunchTemplateData{
							InstanceType: aws.String("m4.large"),
						},
					}},
				},
			},
			elbClient: &mockELBAPI{
				descLBs: &elb.DescribeLoadBalancersOutput{
					LoadBalancerDescriptions: []*elb.LoadBalancerDescription{
						{
							Instances: []*elb.Instance{
								{
									InstanceId: aws.String("foo"),
								},
							},
						},
					},
				},
				descInstanceHealth: &elb.DescribeInstanceHealthOutput{
					InstanceStates: []*elb.InstanceState{
						{
							InstanceId: aws.String("foo"),
							State:      aws.String(autoscaling.LifecycleStateInService),
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
				clusterID: "",
			}

			_, err := backend.Get(&api.NodePool{Name: "test"})
			if tc.success {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestGetInstanceUpdateInfo(t *testing.T) {
	autoscalingInstance := func(id string, launchTemplateName string, launchTemplateVersion string) *autoscaling.Instance {
		return &autoscaling.Instance{
			InstanceId:       aws.String(id),
			AvailabilityZone: aws.String("eu-central-1"),
			LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String(launchTemplateName),
				Version:            aws.String(launchTemplateVersion),
			},
		}
	}
	ec2Instance := func(id string, instanceType string, spot bool) *ec2.Instance {
		var spotRequestId *string
		if spot {
			spotRequestId = aws.String(fmt.Sprintf("spot-%s", id))
		}
		return &ec2.Instance{
			InstanceId:            aws.String(id),
			InstanceType:          aws.String(instanceType),
			SpotInstanceRequestId: spotRequestId,
		}
	}

	for _, tc := range []struct {
		name                       string
		autoscalingInstances       []*autoscaling.Instance
		autoscalingLaunchTemplate  *autoscaling.LaunchTemplateSpecification
		autoscalingMixedInstPolicy *autoscaling.MixedInstancesPolicy
		ec2LaunchTemplate          *ec2.ResponseLaunchTemplateData
		ec2Instances               []*ec2.Instance
		expectedGenerations        map[string]int
	}{
		{
			name: "launch template, on demand, no change",
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{
				InstanceType: aws.String("m4.large"),
			},
			ec2Instances: []*ec2.Instance{
				ec2Instance("i-1", "m4.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
			},
		},
		{
			name: "launch template, spot, no change",
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{
				InstanceType: aws.String("m4.large"),
				InstanceMarketOptions: &ec2.LaunchTemplateInstanceMarketOptions{
					MarketType: aws.String(ec2.MarketTypeSpot),
				},
			},
			ec2Instances: []*ec2.Instance{
				ec2Instance("i-1", "m4.large", true),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": currentNodeGeneration,
			},
		},
		{
			name: "launch template, template changed",
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "newlt", "2"),
			},
			autoscalingLaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("2"),
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{
				InstanceType: aws.String("m4.large"),
			},
			ec2Instances: []*ec2.Instance{
				ec2Instance("i-1", "m4.large", false),
			},
			expectedGenerations: map[string]int{
				"aws:///eu-central-1/i-1": outdatedNodeGeneration,
				"aws:///eu-central-1/i-2": outdatedNodeGeneration,
			},
		},
		{
			name: "launch template, instance type changed",
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{
				InstanceType: aws.String("m4.large"),
			},
			ec2Instances: []*ec2.Instance{
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
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{
				InstanceType: aws.String("m4.large"),
				InstanceMarketOptions: &ec2.LaunchTemplateInstanceMarketOptions{
					MarketType: aws.String(ec2.MarketTypeSpot),
				},
			},
			ec2Instances: []*ec2.Instance{
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
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingLaunchTemplate: &autoscaling.LaunchTemplateSpecification{
				LaunchTemplateName: aws.String("lt"),
				Version:            aws.String("1"),
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{
				InstanceType: aws.String("m4.large"),
			},
			ec2Instances: []*ec2.Instance{
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
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscaling.MixedInstancesPolicy{
				InstancesDistribution: &autoscaling.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int64(100),
				},
				LaunchTemplate: &autoscaling.LaunchTemplate{
					LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []*autoscaling.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{
				InstanceType: aws.String("m4.large"),
			},
			ec2Instances: []*ec2.Instance{
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
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscaling.MixedInstancesPolicy{
				InstancesDistribution: &autoscaling.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int64(0),
				},
				LaunchTemplate: &autoscaling.LaunchTemplate{
					LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []*autoscaling.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{},
			ec2Instances: []*ec2.Instance{
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
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "newlt", "2"),
			},
			autoscalingMixedInstPolicy: &autoscaling.MixedInstancesPolicy{
				InstancesDistribution: &autoscaling.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int64(100),
				},
				LaunchTemplate: &autoscaling.LaunchTemplate{
					LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("2"),
					},
					Overrides: []*autoscaling.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{},
			ec2Instances: []*ec2.Instance{
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
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscaling.MixedInstancesPolicy{
				InstancesDistribution: &autoscaling.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int64(100),
				},
				LaunchTemplate: &autoscaling.LaunchTemplate{
					LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []*autoscaling.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.xlarge")},
					},
				},
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{},
			ec2Instances: []*ec2.Instance{
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
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscaling.MixedInstancesPolicy{
				InstancesDistribution: &autoscaling.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int64(0),
				},
				LaunchTemplate: &autoscaling.LaunchTemplate{
					LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []*autoscaling.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{},
			ec2Instances: []*ec2.Instance{
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
			autoscalingInstances: []*autoscaling.Instance{
				autoscalingInstance("i-1", "lt", "1"),
				autoscalingInstance("i-2", "lt", "1"),
			},
			autoscalingMixedInstPolicy: &autoscaling.MixedInstancesPolicy{
				InstancesDistribution: &autoscaling.InstancesDistribution{
					OnDemandPercentageAboveBaseCapacity: aws.Int64(100),
				},
				LaunchTemplate: &autoscaling.LaunchTemplate{
					LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
						LaunchTemplateName: aws.String("lt"),
						Version:            aws.String("1"),
					},
					Overrides: []*autoscaling.LaunchTemplateOverrides{
						{InstanceType: aws.String("m4.large")},
						{InstanceType: aws.String("m5.large")},
					},
				},
			},
			ec2LaunchTemplate: &ec2.ResponseLaunchTemplateData{},
			ec2Instances: []*ec2.Instance{
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
					asgs: []*autoscaling.Group{
						{
							Tags: []*autoscaling.TagDescription{
								{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
								{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
							},
							Instances:            tc.autoscalingInstances,
							LaunchTemplate:       tc.autoscalingLaunchTemplate,
							MixedInstancesPolicy: tc.autoscalingMixedInstPolicy,
						},
					},
					descLB: &autoscaling.DescribeLoadBalancersOutput{
						LoadBalancers: []*autoscaling.LoadBalancerState{
							{
								LoadBalancerName: aws.String("foo"),
							},
						},
					},
				},
				ec2Client: &mockEC2API{
					descLTVs: &ec2.DescribeLaunchTemplateVersionsOutput{
						LaunchTemplateVersions: []*ec2.LaunchTemplateVersion{{
							LaunchTemplateData: tc.ec2LaunchTemplate,
						}},
					},
					descInstances: &ec2.DescribeInstancesOutput{
						Reservations: []*ec2.Reservation{
							{
								Instances: tc.ec2Instances,
							},
						},
					},
				},
				elbClient: &mockELBAPI{
					descLBs: &elb.DescribeLoadBalancersOutput{
						LoadBalancerDescriptions: []*elb.LoadBalancerDescription{
							{
								Instances: []*elb.Instance{
									{
										InstanceId: aws.String("foo"),
									},
								},
							},
						},
					},
					descInstanceHealth: &elb.DescribeInstanceHealthOutput{
						InstanceStates: []*elb.InstanceState{
							{
								InstanceId: aws.String("foo"),
								State:      aws.String(autoscaling.LifecycleStateInService),
							},
						},
					},
				},
				clusterID: "",
			}

			res, err := backend.Get(&api.NodePool{Name: "test"})
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
	}
	err := backend.Scale(&api.NodePool{Name: "test"}, 10)
	assert.Error(t, err)

	// test scaling up
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{
			asgs: []*autoscaling.Group{
				{
					Tags: []*autoscaling.TagDescription{
						{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
						{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
					},
					Instances: []*autoscaling.Instance{
						{},
					},
					DesiredCapacity: aws.Int64(1),
				},
				{
					Tags: []*autoscaling.TagDescription{
						{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
						{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
					},
					Instances: []*autoscaling.Instance{
						{},
					},
					DesiredCapacity: aws.Int64(1),
				},
			},
		},
	}
	err = backend.Scale(&api.NodePool{Name: "test"}, 10)
	assert.NoError(t, err)

	// test scaling down
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{
			asgs: []*autoscaling.Group{
				{
					Tags: []*autoscaling.TagDescription{
						{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
						{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
					},
					Instances: []*autoscaling.Instance{
						{},
					},
					DesiredCapacity: aws.Int64(1),
				},
				{
					Tags: []*autoscaling.TagDescription{
						{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
						{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
					},
					Instances: []*autoscaling.Instance{
						{},
					},
					DesiredCapacity: aws.Int64(1),
				},
			},
		},
	}
	err = backend.Scale(&api.NodePool{Name: "test"}, 1)
	assert.NoError(t, err)

	// test getting error
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{err: errors.New("failed")},
		ec2Client: &mockEC2API{err: errors.New("failed")},
	}
	err = backend.Terminate(&Node{}, true)
	assert.Error(t, err)
}

func TestDeleteTags(tt *testing.T) {
	for _, tc := range []struct {
		msg       string
		asgClient autoscalingiface.AutoScalingAPI
		nodePool  *api.NodePool
		tags      map[string]string
		success   bool
	}{
		{
			msg: "test removing tags",
			asgClient: &mockASGAPI{
				asgs: []*autoscaling.Group{
					{
						Tags: []*autoscaling.TagDescription{
							{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
							{Key: aws.String(nodePoolTagLegacy), Value: aws.String("test")},
							{Key: aws.String("tag-to-remove"), Value: aws.String("test")},
						},
						Instances: []*autoscaling.Instance{
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
				clusterID: "",
			}

			err := backend.deleteTags(tc.nodePool, tc.tags)
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
			asgs: []*autoscaling.Group{
				{
					AutoScalingGroupName: aws.String("asg-name"),
					DesiredCapacity:      aws.Int64(3),
					MinSize:              aws.Int64(3),
				},
			},
		},
		ec2Client: &mockEC2API{
			descTags: &ec2.DescribeTagsOutput{
				Tags: []*ec2.TagDescription{
					{
						Key:   aws.String(ec2AutoscalingGroupTagKey),
						Value: aws.String("asg-name"),
					},
				},
			},
			descStatus: &ec2.DescribeInstanceStatusOutput{
				InstanceStatuses: []*ec2.InstanceStatus{
					{
						InstanceState: &ec2.InstanceState{
							Code: aws.Int64(48), // terminated
							Name: aws.String("terminated"),
						},
					},
				},
			},
		},
	}
	err := backend.Terminate(&Node{}, true)
	assert.NoError(t, err)

	// test getting error
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{err: errors.New("failed")},
		ec2Client: &mockEC2API{err: errors.New("failed")},
	}
	err = backend.Terminate(&Node{}, true)
	assert.Error(t, err)

	// test already terminated
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{err: errors.New("already terminated")},
		ec2Client: &mockEC2API{descStatus: &ec2.DescribeInstanceStatusOutput{
			InstanceStatuses: []*ec2.InstanceStatus{
				{
					InstanceState: &ec2.InstanceState{
						Code: aws.Int64(48), // terminated
						Name: aws.String("terminated"),
					},
				},
			},
		}},
	}
	err = backend.Terminate(&Node{}, false)
	assert.NoError(t, err)
}

func TestAsgHasAllTags(t *testing.T) {
	expected := []*autoscaling.TagDescription{
		{Key: aws.String("key-1"), Value: aws.String("value-1")},
		{Key: aws.String("key-2"), Value: aws.String("value-2")},
	}
	tags := []*autoscaling.TagDescription{
		{Key: aws.String("key-2"), Value: aws.String("value-2")},
		{Key: aws.String("key-1"), Value: aws.String("value-1")},
	}
	assert.True(t, asgHasAllTags(expected, tags))
}

func TestNotMatchingAsgHasAllTags(t *testing.T) {
	expected := []*autoscaling.TagDescription{
		{Key: aws.String("key"), Value: aws.String("value")},
	}
	tags := []*autoscaling.TagDescription{
		{Key: aws.String("key"), Value: aws.String("bar")},
	}
	assert.False(t, asgHasAllTags(expected, tags))

	expected = []*autoscaling.TagDescription{
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
