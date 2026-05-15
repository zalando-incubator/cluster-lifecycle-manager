package provisioner

import (
	"fmt"
	"maps"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	"gopkg.in/yaml.v2"
)

// RenderedComponent holds the rendered output for a single component.
type RenderedComponent struct {
	Name      string
	Manifests []*kubernetes.ResourceManifest
}

// RenderManifests renders all component manifests from the given config against
// the cluster. values is passed directly to the template engine.
func RenderManifests(cfg channel.Config, cluster *api.Cluster, values map[string]any) ([]RenderedComponent, error) {
	packages, err := renderManifestsInRenderMode(cfg, cluster, values, nil, nil, true)
	if err != nil {
		return nil, err
	}
	result := make([]RenderedComponent, 0, len(packages))
	for _, pkg := range packages {
		result = append(result, RenderedComponent{Name: pkg.name, Manifests: pkg.manifests})
	}
	return result, nil
}

// ApplyDefaults renders config-defaults.yaml and merges any missing keys into
// cluster.ConfigItems, mirroring the production updateDefaults logic. Call this
// before RenderManifests so templates can rely on default config item values.
// basically a copy from clusterpyProvisioner.updateDefaults
func ApplyDefaults(cfg channel.Config, cluster *api.Cluster) error {
	files, err := cfg.DefaultsManifests()
	if err != nil {
		return err
	}

	withoutConfigItems := *cluster
	withoutConfigItems.ConfigItems = make(map[string]string)

	allDefaults := make(map[string]string)
	for _, f := range files {
		rendered, err := renderSingleTemplateWithRenderMode(f, &withoutConfigItems, nil, nil, nil, nil, true)
		if err != nil {
			return err
		}
		var defaults map[string]string
		if err := yaml.Unmarshal([]byte(rendered), &defaults); err != nil {
			return fmt.Errorf("failed to unmarshal defaults file %s: %w", f.Path, err)
		}
		maps.Copy(allDefaults, defaults)
	}

	for k, v := range allDefaults {
		if _, ok := cluster.ConfigItems[k]; !ok {
			cluster.ConfigItems[k] = v
		}
	}
	return nil
}
