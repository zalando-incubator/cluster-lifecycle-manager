package provisioner

import (
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
