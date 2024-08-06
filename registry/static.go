package registry

import "github.com/zalando-incubator/cluster-lifecycle-manager/api"

type staticRegistry struct{}

// NewStaticRegistry initializes a new staticRegistry.
func NewStaticRegistry() Registry {
	return &staticRegistry{}
}

func (r *staticRegistry) ListClusters(_ Filter) ([]*api.Cluster, error) {
	clusters := []*api.Cluster{
		{
			APIServerURL:          "http://127.0.0.1:8001",
			Channel:               "alpha",
			ConfigItems:           map[string]string{"foo": "bar"},
			CriticalityLevel:      2,
			Environment:           "dev",
			ID:                    "123",
			InfrastructureAccount: "fake:abc",
			LifecycleStatus:       "ready",
		},
	}

	return clusters, nil
}

func (r *staticRegistry) UpdateLifecycleStatus(_ *api.Cluster) error {
	return nil
}

func (r *staticRegistry) UpdateConfigItems(_ *api.Cluster) error {
	return nil
}
