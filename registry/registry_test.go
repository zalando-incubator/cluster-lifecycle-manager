package registry

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/zalando-build/cluster-registry/models"
)

func TestFilter(t *testing.T) {
	for _, tc := range []struct {
		testCase        string
		lifecycleStatus *string
		providers       []string
		cluster         *models.Cluster
		expected        bool
	}{
		{
			testCase:        "nil cluster",
			lifecycleStatus: nil,
			providers:       nil,
			cluster:         nil,
			expected:        false,
		},
		{
			testCase:        "nil filters",
			lifecycleStatus: nil,
			providers:       nil,
			cluster: &models.Cluster{
				LifecycleStatus: aws.String("ready"),
				Provider:        aws.String("zalando-aws"),
			},
			expected: true,
		},
		{
			testCase:        "valid lifecycle status",
			lifecycleStatus: aws.String("ready"),
			providers:       nil,
			cluster: &models.Cluster{
				LifecycleStatus: aws.String("ready"),
			},
			expected: true,
		},
		{
			testCase:        "not valid lifecycle status",
			lifecycleStatus: aws.String("ready"),
			providers:       nil,
			cluster: &models.Cluster{
				LifecycleStatus: aws.String("decommissioned"),
			},
			expected: false,
		},
		{
			testCase:        "valid provider",
			lifecycleStatus: aws.String("ready"),
			providers:       []string{"zalando-aws"},
			cluster: &models.Cluster{
				LifecycleStatus: aws.String("ready"),
				Provider:        aws.String("zalando-aws"),
			},
			expected: true,
		},
	} {
		t.Run(tc.testCase, func(t *testing.T) {
			f := &Filter{
				LifecycleStatus: tc.lifecycleStatus,
				Providers:       tc.providers,
			}
			if f.Includes(tc.cluster) != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, !tc.expected)
			}
		})
	}
}
