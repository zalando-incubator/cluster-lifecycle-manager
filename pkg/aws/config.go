package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	awsSessionName = "cluster-lifecycle-manager"
)

// Config sets up an AWS config with the region automatically detected from
// the environment or the ec2 metadata service if running on ec2.
func Config(ctx context.Context, assumedRole string, optFns ...func(*config.LoadOptions) error) (aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return aws.Config{}, err
	}

	if cfg.Region == "" {
		// try to get region from metadata service
		metadata := imds.NewFromConfig(cfg)
		resp, err := metadata.GetRegion(ctx, &imds.GetRegionInput{})
		if err != nil {
			return aws.Config{}, err
		}
		cfg.Region = resp.Region
	}

	if assumedRole != "" {
		stsClient := sts.NewFromConfig(cfg)
		provider := stscreds.NewAssumeRoleProvider(stsClient, assumedRole, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = awsSessionName
		})
		cfg.Credentials = provider
	}

	return cfg, nil
}
