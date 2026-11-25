package reconciler

import (
	"fmt"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

// Skipped describes a registry entry the reconciler does not handle. Only
// identifying fields are kept, never config items.
type Skipped struct {
	Alias  string
	Reason string
	// OtherProvider is true if the cluster is skipped only because its
	// provider is not handled, which is expected for most registry entries.
	OtherProvider bool
}

// Select returns the clusters the component called name is responsible for:
// the provider must be one of providers and the config item "managed_by" must
// be set to name. Unlike the Cluster Lifecycle Manager, a missing "managed_by"
// does not select a cluster. Every other cluster is returned as skipped with
// the reason. The registry filter is not relied on, since not all registries
// apply it.
func Select(clusters []*api.Cluster, providers []string, name string) ([]*api.Cluster, []Skipped) {
	allowed := make(map[api.ProviderID]bool, len(providers))
	for _, p := range providers {
		allowed[api.ProviderID(p)] = true
	}

	var selected []*api.Cluster
	var skipped []Skipped
	for _, cluster := range clusters {
		if cluster == nil {
			continue
		}
		switch {
		case !allowed[cluster.Provider]:
			skipped = append(skipped, Skipped{cluster.Alias, fmt.Sprintf("provider %q is not handled", cluster.Provider), true})
		case cluster.ConfigItems[api.ManagedByConfigItem] != name:
			skipped = append(skipped, Skipped{cluster.Alias, fmt.Sprintf("config item %q is not %q", api.ManagedByConfigItem, name), false})
		default:
			selected = append(selected, cluster)
		}
	}
	return selected, skipped
}
