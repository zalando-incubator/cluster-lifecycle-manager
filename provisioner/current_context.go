package provisioner

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"

	// Register kubeconfig authentication providers, including OIDC.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type kubectlClient struct {
	*kubernetes.Applier
	*kubernetes.ClientsCollection
}

// newCurrentContextKubernetesClient uses the current kubeconfig context for testing.
// It ignores the cluster's API server URL.
func newCurrentContextKubernetesClient(cluster *api.Cluster) (kubernetesApplyClient, error) {
	cfg, contextName, err := currentKubeConfig()
	if err != nil {
		return nil, err
	}
	log.WithField("cluster", cluster.ID).Warnf("Using current kubeconfig context %q (%s); cluster API server URL is ignored", contextName, cfg.Host)
	applier, err := kubernetes.NewApplier(cfg, maxApplyRetries)
	if err != nil {
		return nil, err
	}
	clients, err := kubernetes.NewClientsCollectionFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &kubectlClient{Applier: applier, ClientsCollection: clients}, nil
}

func currentKubeConfig() (*rest.Config, string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	raw, err := rules.Load()
	if err != nil {
		return nil, "", fmt.Errorf("load kubeconfig: %w", err)
	}
	name := raw.CurrentContext
	if name == "" {
		return nil, "", fmt.Errorf("kubeconfig has no current context")
	}
	context, ok := raw.Contexts[name]
	if !ok || context == nil {
		return nil, "", fmt.Errorf("kubeconfig current context %q not found", name)
	}
	cluster, ok := raw.Clusters[context.Cluster]
	if !ok || cluster == nil || cluster.Server == "" {
		return nil, "", fmt.Errorf("kubeconfig current context %q has no cluster server", name)
	}
	cfg, err := clientcmd.NewNonInteractiveClientConfig(*raw, name, &clientcmd.ConfigOverrides{}, rules).ClientConfig()
	if err != nil {
		return nil, "", fmt.Errorf("configure kubeconfig context %q: %w", name, err)
	}
	return cfg, name, nil
}
