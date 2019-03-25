package provisioner

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetInfrastructureID(t *testing.T) {
	expected := "12345678910"
	awsAccountID := getAWSAccountID(fmt.Sprintf("aws:%s", expected))
	if awsAccountID != expected {
		t.Errorf("expected: %s, got: %s", expected, awsAccountID)
	}
}

func TestHasTag(t *testing.T) {
	for _, tc := range []struct {
		msg      string
		tags     []*ec2.Tag
		tag      *ec2.Tag
		expected bool
	}{
		{
			msg: "test finding tag in list successfully",
			tags: []*ec2.Tag{
				{
					Key:   aws.String("key"),
					Value: aws.String("val"),
				},
			},
			tag: &ec2.Tag{
				Key:   aws.String("key"),
				Value: aws.String("val"),
			},
			expected: true,
		},
		{
			msg: "test both key and value must match",
			tags: []*ec2.Tag{
				{
					Key:   aws.String("key"),
					Value: aws.String("val"),
				},
			},
			tag: &ec2.Tag{
				Key:   aws.String("key"),
				Value: aws.String(""),
			},
			expected: false,
		},
		{
			msg:  "test finding no tag in empty list",
			tags: []*ec2.Tag{},
			tag: &ec2.Tag{
				Key:   aws.String("key"),
				Value: aws.String(""),
			},
			expected: false,
		},
	} {
		t.Run(tc.msg, func(t *testing.T) {
			assert.Equal(t, hasTag(tc.tags, tc.tag), tc.expected)
		})
	}
}

func TestFilterSubnets(tt *testing.T) {
	for _, tc := range []struct {
		msg             string
		subnets         []*ec2.Subnet
		subnetIds       []string
		expectedSubnets []*ec2.Subnet
		err             bool
	}{

		{
			msg: "test filtering out a single subnet",
			subnets: []*ec2.Subnet{
				{
					SubnetId: aws.String("id-1"),
				},
				{
					SubnetId: aws.String("id-2"),
				},
			},
			subnetIds: []string{"id-1"},
			expectedSubnets: []*ec2.Subnet{
				{
					SubnetId: aws.String("id-1"),
				},
			},
		},
		{
			msg: "test filtering invalid subnets",
			subnets: []*ec2.Subnet{
				{
					SubnetId: aws.String("id-1"),
				},
			},
			subnetIds:       []string{"id-2"},
			expectedSubnets: nil,
		},
	} {
		tt.Run(tc.msg, func(t *testing.T) {
			subnets, err := filterSubnets(tc.subnets, tc.subnetIds)
			if tc.err {
				require.Error(t, err)
			} else {
				require.EqualValues(t, tc.expectedSubnets, subnets)
			}
		})
	}
}
