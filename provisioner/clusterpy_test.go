package provisioner

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/stretchr/testify/assert"
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
