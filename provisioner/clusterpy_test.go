package provisioner

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/stretchr/testify/assert"

	"golang.org/x/oauth2"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

func TestVersion(t *testing.T) {
	cluster := &api.Cluster{
		ID: "aws:123456789012:eu-central-1:kube-1",
		InfrastructureAccount: "aws:123456789012",
		LocalID:               "kube-1",
		APIServerURL:          "https://kube-1.foo.example.org/",
		Channel:               "alpha",
		Environment:           "production",
		CriticalityLevel:      1,
		LifecycleStatus:       "ready",
		Provider:              "zalando-aws",
		Region:                "eu-central-1",
		ConfigItems: map[string]string{
			"product_x_key": "abcde",
			"product_y_key": "12345",
		},
		NodePools: []*api.NodePool{
			{
				Name:             "master-default",
				Profile:          "master/default",
				InstanceType:     "m3.medium",
				DiscountStrategy: "none",
				MinSize:          2,
				MaxSize:          2,
			},
			{
				Name:             "worker-default",
				Profile:          "worker/default",
				InstanceType:     "r4.large",
				DiscountStrategy: "none",
				MinSize:          3,
				MaxSize:          20,
			},
		},
	}

	token := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "foo.bar.token"})
	provisioner := NewClusterpyProvisioner(token, "", aws.NewConfig(), nil)
	version, err := provisioner.Version(cluster, "git-commit-hash")
	if err != nil {
		t.Errorf("should not fail: %v", err)
	}

	version2, err := provisioner.Version(cluster, "git-commit-hash")
	if err != nil {
		t.Errorf("should not fail: %v", err)
	}

	if version != version2 {
		t.Errorf("expected version %s, got %s", version, version2)
	}
}

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
