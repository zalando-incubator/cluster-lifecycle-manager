package iface

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
)

type AutoScalingAPI interface {
	autoscaling.DescribeAutoScalingGroupsAPIClient
	UpdateAutoScalingGroup(ctx context.Context, params *autoscaling.UpdateAutoScalingGroupInput, optFns ...func(*autoscaling.Options)) (*autoscaling.UpdateAutoScalingGroupOutput, error)
	SuspendProcesses(ctx context.Context, params *autoscaling.SuspendProcessesInput, optFns ...func(*autoscaling.Options)) (*autoscaling.SuspendProcessesOutput, error)
	ResumeProcesses(ctx context.Context, params *autoscaling.ResumeProcessesInput, optFns ...func(*autoscaling.Options)) (*autoscaling.ResumeProcessesOutput, error)
	TerminateInstanceInAutoScalingGroup(ctx context.Context, params *autoscaling.TerminateInstanceInAutoScalingGroupInput, optFns ...func(*autoscaling.Options)) (*autoscaling.TerminateInstanceInAutoScalingGroupOutput, error)
	DeleteTags(ctx context.Context, params *autoscaling.DeleteTagsInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DeleteTagsOutput, error)
	autoscaling.DescribeLoadBalancersAPIClient
}
