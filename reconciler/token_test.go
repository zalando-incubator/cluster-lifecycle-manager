package reconciler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryTokenSource(t *testing.T) {
	// a static token wins
	ts := (&Config{Registry: "https://registry.example.org", RegistryToken: "static"}).RegistryTokenSource()
	require.NotNil(t, ts)
	tok, err := ts.Token()
	require.NoError(t, err)
	require.Equal(t, "static", tok.AccessToken)

	// an http registry without a static token uses the platform credentials
	require.NotNil(t, (&Config{Registry: "https://registry.example.org", RegistryTokenName: "cluster-registry-rw", CredentialsDir: t.TempDir()}).RegistryTokenSource())

	// a file registry needs no token
	require.Nil(t, (&Config{Registry: "file:///tmp/clusters.yaml"}).RegistryTokenSource())
	require.Nil(t, (&Config{Registry: "clusters.yaml"}).RegistryTokenSource())
}
