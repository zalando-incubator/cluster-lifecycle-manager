package reconciler

import (
	"net/url"

	"golang.org/x/oauth2"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/credentials-loader/platformiam"
)

// RegistryTokenSource returns the token source for the registry: a static
// token if one is configured, the platform credentials for an http(s)
// registry, and none for a file registry.
func (cfg *Config) RegistryTokenSource() oauth2.TokenSource {
	if cfg.RegistryToken != "" {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.RegistryToken})
	}
	if u, err := url.Parse(cfg.Registry); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return platformiam.NewTokenSource(cfg.RegistryTokenName, cfg.CredentialsDir)
	}
	return nil
}
