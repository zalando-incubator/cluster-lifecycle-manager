package aws

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws/client/metadata"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/stretchr/testify/require"
)

func TestShouldRetry(t *testing.T) {
	for _, tc := range []struct {
		caseName         string
		maxRetries       int
		maxRetryInterval time.Duration
		request          *request.Request
		expected         bool
	}{
		{
			caseName:   "should not retry",
			maxRetries: 1,
			request:    &request.Request{},
			expected:   false,
		},
		{
			caseName:   "should retry with metadata service",
			maxRetries: 1,
			request: &request.Request{
				ClientInfo: metadata.ClientInfo{
					ServiceName: "ec2metadata",
				},
			},
			expected: true,
		},
		{
			caseName:   "should not retry with metadata service",
			maxRetries: 1,
			request: &request.Request{
				ClientInfo: metadata.ClientInfo{
					ServiceName: "ec2metadata",
				},
				RetryCount: 8,
			},
			expected: false,
		},
	} {
		t.Run(tc.caseName, func(t *testing.T) {
			retryer := NewClampedRetryer(tc.maxRetries, time.Second)

			res := retryer.ShouldRetry(tc.request)
			require.Equal(t, tc.expected, res)
		})
	}
}

func TestRetryRules(t *testing.T) {
	for _, tc := range []struct {
		caseName            string
		maxRetryInterval    time.Duration
		request             *request.Request
		expectedLessOrEqual time.Duration
	}{
		{
			caseName:            "should return max retry interval",
			maxRetryInterval:    time.Millisecond,
			request:             &request.Request{},
			expectedLessOrEqual: time.Millisecond,
		},
		{
			caseName:         "should not return max retry interval",
			maxRetryInterval: time.Second,
			request: &request.Request{
				ClientInfo: metadata.ClientInfo{
					ServiceName: "ec2metadata",
				},
			},
			expectedLessOrEqual: time.Second / 2,
		},
	} {
		t.Run(tc.caseName, func(t *testing.T) {
			retryer := NewClampedRetryer(1, tc.maxRetryInterval)

			res := retryer.RetryRules(tc.request)
			require.LessOrEqual(t, res, tc.expectedLessOrEqual)
		})
	}
}
