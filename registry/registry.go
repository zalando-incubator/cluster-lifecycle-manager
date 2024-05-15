package registry

import (
	"log"
	"net/url"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
	"golang.org/x/oauth2"
)

// Filter defines a filter which can be used when listing clusters.
type Filter struct {
	LifecycleStatus *string
	Providers       []string
}

// Registry defines an interface for listing and updating clusters from a
// cluster registry.
type Registry interface {
	ListClusters(filter Filter) ([]*api.Cluster, error)
	UpdateCluster(cluster *api.Cluster) error
}

// NewRegistry initializes a new registry source based on the uri.
func NewRegistry(uri string, tokenSource oauth2.TokenSource, options *Options) Registry {
	url, err := url.Parse(uri)
	if err != nil {
		return nil
	}

	switch url.Scheme {
	case "http", "https":
		return NewHTTPRegistry(url, tokenSource, options)
	case "file", "":
		return NewFileRegistry(url.Host + url.Path)
	default:
		log.Fatalf("unknown registry type: %v", url.Scheme)
	}

	return nil
}

// Includes returns true if the cluster should be included based on the filter.
// Returns false if the given cluster is nil.
func (f *Filter) Includes(cluster *models.Cluster) bool {
	if cluster == nil {
		return false
	}

	if f.LifecycleStatus != nil &&
		*cluster.LifecycleStatus != *f.LifecycleStatus {

		return false
	}

	if len(f.Providers) == 0 {
		return true
	}

	for _, p := range f.Providers {
		if *cluster.Provider == string(p) {
			return true
		}
	}

	return false
}
