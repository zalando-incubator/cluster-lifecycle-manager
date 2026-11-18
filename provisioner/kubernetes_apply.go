package provisioner

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	"gopkg.in/yaml.v2"
)

// kubernetesApplyClient applies manifests and deletes resources on a cluster.
type kubernetesApplyClient interface {
	Apply(context.Context, *kubernetes.ResourceManifest) error
	DeleteResource(context.Context, *log.Entry, *kubernetes.Resource) error
}

// kubernetesApplyClientFactory creates a client for the cluster being provisioned.
type kubernetesApplyClientFactory func(*api.Cluster) (kubernetesApplyClient, error)

type kubernetesApplyProvisioner struct {
	provider        api.ProviderID
	secretDecrypter decrypter.Decrypter
	dryRun          bool
	newClient       kubernetesApplyClientFactory
}

// newKubernetesApplyProvisioner applies channel manifests using a provider-specific client.
func newKubernetesApplyProvisioner(provider api.ProviderID, newClient kubernetesApplyClientFactory, secretDecrypter decrypter.Decrypter, dryRun bool) *kubernetesApplyProvisioner {
	if secretDecrypter == nil {
		secretDecrypter = decrypter.SecretDecrypter{}
	}
	return &kubernetesApplyProvisioner{provider: provider, secretDecrypter: secretDecrypter, dryRun: dryRun, newClient: newClient}
}

func (p *kubernetesApplyProvisioner) Supports(cluster *api.Cluster) bool {
	return cluster.Provider == p.provider
}

func (p *kubernetesApplyProvisioner) Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, channelConfig channel.Config) error {
	if !p.Supports(cluster) {
		return ErrProviderNotSupported
	}

	defaultsFiles, err := channelConfig.DefaultsManifests()
	if err != nil {
		return fmt.Errorf("unable to read configuration defaults: %w", err)
	}
	withoutConfigItems := *cluster
	withoutConfigItems.ConfigItems = make(map[string]string)
	defaults := make(map[string]string)
	for _, file := range defaultsFiles {
		rendered, err := renderSingleTemplate(file, &withoutConfigItems, nil, nil, nil, nil)
		if err != nil {
			return fmt.Errorf("unable to render defaults file %s: %w", file.Path, err)
		}
		var items map[string]string
		if err := yaml.Unmarshal([]byte(rendered), &items); err != nil {
			return fmt.Errorf("failed to unmarshal defaults file %s: %w", file.Path, err)
		}
		for key, value := range items {
			defaults[key] = value
		}
	}
	if cluster.ConfigItems == nil {
		cluster.ConfigItems = make(map[string]string)
	}
	for key, value := range defaults {
		if _, ok := cluster.ConfigItems[key]; !ok {
			cluster.ConfigItems[key] = value
		}
	}
	for key, value := range cluster.ConfigItems {
		plaintext, err := p.secretDecrypter.Decrypt(ctx, value)
		if err != nil {
			return fmt.Errorf("unable to decrypt config item %s: %w", key, err)
		}
		cluster.ConfigItems[key] = plaintext
	}
	for _, nodePool := range cluster.NodePools {
		if nodePool.ConfigItems == nil {
			nodePool.ConfigItems = make(map[string]string)
		}
		for key, value := range cluster.ConfigItems {
			if _, ok := nodePool.ConfigItems[key]; !ok {
				nodePool.ConfigItems[key] = value
			}
		}
	}

	values := map[string]interface{}{}
	deletions, err := parseDeletions(channelConfig, cluster, values, nil, nil)
	if err != nil {
		return err
	}
	manifests, err := renderManifests(channelConfig, cluster, values, nil, nil)
	if err != nil {
		return err
	}
	if p.dryRun {
		logger.Infof("Dry run: skipping Kubernetes deletions and application of manifests")
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.newClient == nil {
		return fmt.Errorf("no Kubernetes client factory configured for provider %s", p.provider)
	}
	client, err := p.newClient(cluster)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("Kubernetes client factory returned nil for provider %s", p.provider)
	}
	deleteResources := func(resources []*kubernetes.Resource) error {
		for _, resource := range resources {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := client.DeleteResource(ctx, logger, resource); err != nil {
				return err
			}
		}
		return nil
	}
	logger.Debugf("Running PreApply deletions (%d)", len(deletions.PreApply))
	if err := deleteResources(deletions.PreApply); err != nil {
		return err
	}
	for _, module := range manifests {
		logger := logger.WithField("module", module.name)
		for _, manifest := range module.manifests {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := client.Apply(ctx, manifest); err != nil {
				return fmt.Errorf("kubectl apply failed for %s: %w", manifest.SourceFile, err)
			}
			logger.Infof("%s/%s %s/%s applied", manifest.APIVersion, manifest.Kind, manifest.Namespace, manifest.Name)
		}
	}
	logger.Debugf("Running PostApply deletions (%d)", len(deletions.PostApply))
	return deleteResources(deletions.PostApply)
}

func (p *kubernetesApplyProvisioner) Decommission(_ context.Context, _ *log.Entry, cluster *api.Cluster) error {
	if !p.Supports(cluster) {
		return ErrProviderNotSupported
	}
	return fmt.Errorf("%s does not support decommissioning clusters", p.provider)
}
