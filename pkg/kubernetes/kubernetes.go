package kubernetes

import (
	"net/http"

	"golang.org/x/oauth2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func newConfig(host string, tokenSrc oauth2.TokenSource) *rest.Config {
	return &rest.Config{
		Host: host,
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &oauth2.Transport{
				Source: tokenSrc,
				Base:   rt,
			}
		},
		Burst: 100,
	}
}

// NewClient initializes a Kubernetes client with the
// specified token source.
func NewClient(host string, tokenSrc oauth2.TokenSource) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(newConfig(host, tokenSrc))
}

// NewDynamicClient initializes a dynamic Kubernetes client with the
// specified token source.
func NewDynamicClient(host string, tokenSrc oauth2.TokenSource) (dynamic.Interface, error) {
	return dynamic.NewForConfig(newConfig(host, tokenSrc))
}
