package reconciler

import (
	"encoding/base64"
	"fmt"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func base64Encode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// BuildKubeconfig builds a kubeconfig for a cluster from the outputs of the
// infrastructure module and a token. The endpoint may come with or without a
// scheme; the CA certificate may be PEM or base64 encoded PEM.
func BuildKubeconfig(alias, endpoint, caCertificate, token string) ([]byte, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("empty endpoint")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}

	ca, err := normalizeCA(caCertificate)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("empty token")
	}

	cfg := clientcmdapi.NewConfig()
	cfg.Clusters[alias] = &clientcmdapi.Cluster{Server: endpoint, CertificateAuthorityData: ca}
	cfg.AuthInfos[alias] = &clientcmdapi.AuthInfo{Token: strings.TrimSpace(token)}
	cfg.Contexts[alias] = &clientcmdapi.Context{Cluster: alias, AuthInfo: alias}
	cfg.CurrentContext = alias
	return clientcmd.Write(*cfg)
}

func normalizeCA(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("empty CA certificate")
	}
	if strings.HasPrefix(value, "-----BEGIN") {
		return []byte(value), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(decoded)), "-----BEGIN") {
		return nil, fmt.Errorf("CA certificate is neither PEM nor base64 encoded PEM")
	}
	return decoded, nil
}
