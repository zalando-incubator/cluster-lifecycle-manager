package provisioner

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

type (
	mockAWSAdapter struct{}
	mockRegistry   struct{}
)

func (m *mockAWSAdapter) GetEKSClusterCA(_ *api.Cluster) (
	*EKSClusterInfo,
	error,
) {
	return &EKSClusterInfo{
		Endpoint:             "https://api.cluster.local",
		CertificateAuthority: "YmxhaA==",
	}, nil
}

func (r *mockRegistry) ListClusters(_ registry.Filter) (
	[]*api.Cluster,
	error,
) {
	return []*api.Cluster{}, nil
}

func (r *mockRegistry) UpdateLifecycleStatus(_ *api.Cluster) error {
	return nil
}

func (r *mockRegistry) UpdateConfigItems(_ *api.Cluster) error {
	return nil
}

func TestGetPostOptions(t *testing.T) {
	for _, tc := range []struct {
		cfOutput map[string]string
		expected *HookResponse
	}{
		{
			cfOutput: map[string]string{
				"EKSSubneta": "subnet-123",
				"EKSSubnetb": "subnet-456",
				"EKSSubnetc": "subnet-789",
			},
			expected: &HookResponse{
				APIServerURL: "https://api.cluster.local",
				CAData:       []byte("blah"),
				AZInfo: &AZInfo{
					subnets: map[string]string{
						"eu-central-1a": "subnet-123",
						"eu-central-1b": "subnet-456",
						"eu-central-1c": "subnet-789",
					},
				},
				TemplateValues: map[string]interface{}{
					subnetsValueKey: map[string]string{
						"eu-central-1a": "subnet-123",
						"eu-central-1b": "subnet-456",
						"eu-central-1c": "subnet-789",
					},
				},
			},
		},
		{
			cfOutput: map[string]string{},
			expected: &HookResponse{
				APIServerURL: "https://api.cluster.local",
				CAData:       []byte("blah"),
			},
		},
	} {
		z := NewZalandoEKSCreationHook(&mockRegistry{})
		res, err := z.Execute(
			&mockAWSAdapter{},
			&api.Cluster{},
			tc.cfOutput,
		)

		require.NoError(t, err)
		require.Equal(t, tc.expected, res)
	}
}
