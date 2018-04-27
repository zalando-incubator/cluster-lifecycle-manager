package updatestrategy

import (
	"errors"
	"testing"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elb/elbiface"
	"github.com/stretchr/testify/assert"
)

type mockASGAPI struct {
	autoscalingiface.AutoScalingAPI
	err    error
	asgs   []*autoscaling.Group
	descLC *autoscaling.DescribeLaunchConfigurationsOutput
	descLB *autoscaling.DescribeLoadBalancersOutput
}

func (a *mockASGAPI) DescribeAutoScalingGroupsPages(input *autoscaling.DescribeAutoScalingGroupsInput, fn func(*autoscaling.DescribeAutoScalingGroupsOutput, bool) bool) error {
	fn(&autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: a.asgs}, true)
	return a.err

}

func (a *mockASGAPI) DescribeLaunchConfigurations(input *autoscaling.DescribeLaunchConfigurationsInput) (*autoscaling.DescribeLaunchConfigurationsOutput, error) {
	return a.descLC, a.err
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

type mockEC2API struct {
	ec2iface.EC2API
	err        error
	descAttr   *ec2.DescribeInstanceAttributeOutput
	descStatus *ec2.DescribeInstanceStatusOutput
	descSpot   *ec2.DescribeSpotInstanceRequestsOutput
	descInsts  *ec2.DescribeInstancesOutput
}

func (e *mockEC2API) DescribeInstanceAttribute(input *ec2.DescribeInstanceAttributeInput) (*ec2.DescribeInstanceAttributeOutput, error) {
	return e.descAttr, e.err
}

func (e *mockEC2API) DescribeSpotInstanceRequests(input *ec2.DescribeSpotInstanceRequestsInput) (*ec2.DescribeSpotInstanceRequestsOutput, error) {
	return e.descSpot, e.err
}

func (e *mockEC2API) DescribeInstancesPages(input *ec2.DescribeInstancesInput, fn func(*ec2.DescribeInstancesOutput, bool) bool) error {
	fn(e.descInsts, false)
	return e.err
}

func (e *mockEC2API) DescribeInstanceStatus(input *ec2.DescribeInstanceStatusInput) (*ec2.DescribeInstanceStatusOutput, error) {
	return e.descStatus, e.err
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
							{Key: aws.String(nodePoolTag), Value: aws.String("test")},
						},
						Instances: []*autoscaling.Instance{
							{
								InstanceId: aws.String("instance_id"),
							},
						},
					},
				},
				descLC: &autoscaling.DescribeLaunchConfigurationsOutput{
					LaunchConfigurations: []*autoscaling.LaunchConfiguration{
						{
							InstanceType: aws.String("instance_type"),
							UserData:     aws.String("user_data"),
							ImageId:      aws.String("ami"),
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
				descAttr: &ec2.DescribeInstanceAttributeOutput{
					InstanceType: &ec2.AttributeValue{Value: aws.String("instance_type")},
					UserData:     &ec2.AttributeValue{Value: aws.String("user_data")},
				},
				descInsts: &ec2.DescribeInstancesOutput{
					Reservations: []*ec2.Reservation{
						{
							Instances: []*ec2.Instance{
								{
									InstanceId: aws.String("instance_id"),
									ImageId:    aws.String("ami"),
								},
							},
						},
					},
				},
				descSpot: &ec2.DescribeSpotInstanceRequestsOutput{
					SpotInstanceRequests: []*ec2.SpotInstanceRequest{},
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
							{Key: aws.String(nodePoolTag), Value: aws.String("test")},
						},
						Instances: []*autoscaling.Instance{
							{
								InstanceId: aws.String("instance_id"),
							},
						},
					},
				},
				descLC: &autoscaling.DescribeLaunchConfigurationsOutput{
					LaunchConfigurations: []*autoscaling.LaunchConfiguration{
						{
							InstanceType: aws.String("instance_type"),
							UserData:     aws.String("user_data"),
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
				descAttr: &ec2.DescribeInstanceAttributeOutput{
					InstanceType: &ec2.AttributeValue{Value: aws.String("instance_type")},
					UserData:     &ec2.AttributeValue{Value: aws.String("user_data_new")},
				},
				descInsts: &ec2.DescribeInstancesOutput{
					Reservations: []*ec2.Reservation{
						{
							Instances: []*ec2.Instance{
								{
									InstanceId: aws.String("instance_id"),
									ImageId:    aws.String("ami"),
								},
							},
						},
					},
				},
				descSpot: &ec2.DescribeSpotInstanceRequestsOutput{
					SpotInstanceRequests: []*ec2.SpotInstanceRequest{},
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
				assert.NoError(t, err)
			}
		})
	}
}

func TestScale(t *testing.T) {
	// test not getting the ASG
	backend := &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{},
	}
	err := backend.Scale(&api.NodePool{Name: "test"}, 10)
	assert.Error(t, err)

	// test success
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{
			asgs: []*autoscaling.Group{
				{
					Tags: []*autoscaling.TagDescription{
						{Key: aws.String(clusterIDTagPrefix), Value: aws.String(resourceLifecycleOwned)},
						{Key: aws.String(nodePoolTag), Value: aws.String("test")},
					},
					Instances: []*autoscaling.Instance{
						{},
					},
				},
			},
		},
	}
	err = backend.Scale(&api.NodePool{Name: "test"}, 10)
	assert.NoError(t, err)

	// test getting error
	backend = &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{err: errors.New("failed")},
		ec2Client: &mockEC2API{err: errors.New("failed")},
	}
	err = backend.Terminate(&Node{}, true)
	assert.Error(t, err)
}

func TestTerminate(t *testing.T) {
	// test success
	backend := &ASGNodePoolsBackend{
		asgClient: &mockASGAPI{},
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
						Code: aws.Int64(32),
						Name: aws.String("shutting-down"),
					},
				},
			},
		}},
	}
	err = backend.Terminate(&Node{}, true)
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
