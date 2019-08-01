package aws

import (
	"time"

	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
)

const (
	metadataService           = "ec2metadata"
	maxMetadataServiceRetries = 8
)

type clampedRetryer struct {
	client.DefaultRetryer
	maxRetryInterval time.Duration
}

func NewClampedRetryer(maxRetries int, maxRetryInterval time.Duration) request.Retryer {
	return clampedRetryer{
		DefaultRetryer:   client.DefaultRetryer{NumMaxRetries: maxRetries},
		maxRetryInterval: maxRetryInterval,
	}
}

func (retryer clampedRetryer) ShouldRetry(r *request.Request) bool {
	if r.ClientInfo.ServiceName == metadataService {
		return r.RetryCount < maxMetadataServiceRetries
	}
	return retryer.DefaultRetryer.ShouldRetry(r)
}

func (retryer clampedRetryer) RetryRules(r *request.Request) time.Duration {
	baseInterval := retryer.DefaultRetryer.RetryRules(r)
	if baseInterval < retryer.maxRetryInterval {
		return baseInterval
	}
	return retryer.maxRetryInterval
}
