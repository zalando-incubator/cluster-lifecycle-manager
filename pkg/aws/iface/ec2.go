package iface

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type EC2API interface {
	ec2.DescribeInstancesAPIClient
	ec2.DescribeInstanceTypesAPIClient
	ec2.DescribeVolumesAPIClient
	DeleteVolume(ctx context.Context, params *ec2.DeleteVolumeInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVolumeOutput, error)
	ec2.DescribeVpcsAPIClient
	ec2.DescribeSubnetsAPIClient
	CreateTags(ctx context.Context, params *ec2.CreateTagsInput, optFns ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error)
	DeleteTags(ctx context.Context, params *ec2.DeleteTagsInput, optFns ...func(*ec2.Options)) (*ec2.DeleteTagsOutput, error)
	ec2.DescribeNetworkInterfacesAPIClient
	DeleteNetworkInterface(ctx context.Context, params *ec2.DeleteNetworkInterfaceInput, optFns ...func(*ec2.Options)) (*ec2.DeleteNetworkInterfaceOutput, error)
	ec2.DescribeImagesAPIClient
	DescribeInstanceAttribute(ctx context.Context, params *ec2.DescribeInstanceAttributeInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstanceAttributeOutput, error)
	TerminateInstances(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
	ec2.DescribeInstanceStatusAPIClient
	ec2.DescribeLaunchTemplateVersionsAPIClient
	ec2.DescribeTagsAPIClient
}
