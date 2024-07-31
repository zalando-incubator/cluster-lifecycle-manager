package provisioner

import (
	"testing"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/stretchr/testify/require"
)

type (
	mockAWSAdapter struct {}
)

func (m *mockAWSAdapter) GetEKSClusterCA(_ *api.Cluster) (
	*EKSClusterInfo,
	error,
) {
	return &EKSClusterInfo{
		Endpoint: "https://api.cluster.local",
		CertificateAuthority: "YmxhaA==",
	}, nil
}

func TestGetPostOptions(t *testing.T) {
	for _, tc := range []struct {
		cfOutput map[string]string
		expected *PostOptions
	}{
		{
			cfOutput: map[string]string{
				"EKSSubneta": "subnet-123",
				"EKSSubnetb": "subnet-456",
				"EKSSubnetc": "subnet-789",
			},
			expected: &PostOptions{
				APIServerURL: "https://api.cluster.local",
				CAData: []byte("blah"),
				ConfigItems: map[string]string{
					"eks_endpoint": "https://api.cluster.local",
					"eks_certficate_authority_data": "YmxhaA==",
				},
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
			expected: &PostOptions{
				APIServerURL: "https://api.cluster.local",
				CAData: []byte("blah"),
				ConfigItems: map[string]string{
					"eks_endpoint": "https://api.cluster.local",
					"eks_certficate_authority_data": "YmxhaA==",
				},
			},
		},
	}{
		z := &ZalandoEKSModifier{}
		res, err := z.GetPostOptions(
			&mockAWSAdapter{},
			&api.Cluster{},
			tc.cfOutput,
		)

		require.NoError(t, err)
		require.Equal(t, tc.expected, res)
	}
}
