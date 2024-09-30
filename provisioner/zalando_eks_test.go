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

func (m *mockAWSAdapter) GetEKSClusterDetails(_ *api.Cluster) (
	*EKSClusterDetails,
	error,
) {
	return &EKSClusterDetails{
		Endpoint:             "https://api.cluster.local",
		CertificateAuthority: "YmxhaA==",
		OIDCIssuerURL:        "https://oidc.provider.local/id/foo",
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

func (r *mockRegistry) UpdateConfigItems(_ *api.Cluster, _ map[string]string) error {
	return nil
}

func TestCreationHookExecute(t *testing.T) {
	for _, tc := range []struct {
		expected *HookResponse
	}{
		{
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
		)

		require.NoError(t, err)
		require.Equal(t, tc.expected, res)
	}
}
