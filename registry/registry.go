package registry

import (
	"log"
	"net/url"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"golang.org/x/oauth2"
)

// Filter defines a filter which can be used when listing clusters.
type Filter struct {
	LifecycleStatus *string
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
