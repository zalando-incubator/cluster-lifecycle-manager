package kubernetes

import (
	"net/http"

	"golang.org/x/oauth2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NewKubeClientWithTokenSource initializes a Kubernetes client with the
// specified token source.
func NewKubeClientWithTokenSource(host string, tokenSrc oauth2.TokenSource) (kubernetes.Interface, error) {
	cfg := &rest.Config{
		Host: host,
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &oauth2.Transport{
				Source: tokenSrc,
				Base:   rt,
			}
		},
	}

	return kubernetes.NewForConfig(cfg)
}
