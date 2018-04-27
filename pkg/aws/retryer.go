package aws

import (
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"time"
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

func (retryer clampedRetryer) RetryRules(r *request.Request) time.Duration {
	baseInterval := retryer.DefaultRetryer.RetryRules(r)
	if baseInterval < retryer.maxRetryInterval {
		return baseInterval
	} else {
		return retryer.maxRetryInterval
	}
}
