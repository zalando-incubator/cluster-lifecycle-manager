package channel

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

type CombinedSource struct {
	sources []ConfigSource
}

type combinedVersion struct {
	owner    *CombinedSource
	versions []ConfigVersion
}

type combinedConfig struct {
	owner   *CombinedSource
	configs []Config
}

func NewCombinedSource(sources []ConfigSource) (ConfigSource, error) {
	names := make(map[string]struct{})
	for _, src := range sources {
		if _, ok := names[src.Name()]; ok {
			return nil, fmt.Errorf("duplicate config source name: %s", src.Name())
		}
		names[src.Name()] = struct{}{}
	}

	return &CombinedSource{
		sources: sources,
	}, nil
}

func (c *CombinedSource) Name() string {
	return "<combined>"
}

func (c *CombinedSource) Update(ctx context.Context, logger *logrus.Entry) error {
	for _, source := range c.sources {
		err := source.Update(ctx, logger)
		if err != nil {
			return fmt.Errorf("error while updating source %s: %v", source.Name(), err)
		}
	}

	return nil
}

func (c *CombinedSource) Version(channel string, overrides map[string]string) (ConfigVersion, error) {
	versions := make([]ConfigVersion, len(c.sources))
	for i, source := range c.sources {
		sourceChannel := channel
		if overrideChannel, ok := overrides[source.Name()]; ok {
			sourceChannel = overrideChannel
		}

		version, err := source.Version(sourceChannel, nil)
		if err != nil {
			return nil, fmt.Errorf("unable to determine version for source %s: %v", source.Name(), err)
		}
		versions[i] = version
	}

	return &combinedVersion{
		owner:    c,
		versions: versions,
	}, nil
}

func (c *CombinedSource) sourceName(pos int) string {
	return c.sources[pos].Name()
}

func (v *combinedVersion) ID() string {
	ids := make([]string, len(v.versions))
	for i, version := range v.versions {
		ids[i] = fmt.Sprintf("%s=%s", v.owner.sourceName(i), version.ID())
	}
	return strings.Join(ids, ";")
}

func (v *combinedVersion) Get(ctx context.Context, logger *logrus.Entry) (Config, error) {
	configs := make([]Config, len(v.versions))
	for i, version := range v.versions {
		config, err := version.Get(ctx, logger)
		if err != nil {
			return nil, fmt.Errorf("unable to checkout version %s for source %s: %v", version.ID(), v.owner.sourceName(i), err)
		}
		configs[i] = config
	}

	return &combinedConfig{
		owner:   v.owner,
		configs: configs,
	}, nil
}

func (c *combinedConfig) mainConfig() (Config, error) {
	if len(c.configs) == 0 {
		return nil, errors.New("no configs found")
	}
	return c.configs[0], nil
}

func (c *combinedConfig) StackManifest(manifestName string) (Manifest, error) {
	mainConfig, err := c.mainConfig()
	if err != nil {
		return Manifest{}, err
	}
	return mainConfig.StackManifest(manifestName)
}

func (c *combinedConfig) EtcdManifest(manifestName string) (Manifest, error) {
	mainConfig, err := c.mainConfig()
	if err != nil {
		return Manifest{}, err
	}
	return mainConfig.EtcdManifest(manifestName)
}

func (c *combinedConfig) NodePoolManifest(profileName string, manifestName string) (Manifest, error) {
	mainConfig, err := c.mainConfig()
	if err != nil {
		return Manifest{}, err
	}
	return mainConfig.NodePoolManifest(profileName, manifestName)
}

func (c *combinedConfig) DefaultsManifests() ([]Manifest, error) {
	var result []Manifest
	for i, config := range c.configs {
		configs, err := config.DefaultsManifests()
		if err != nil {
			return nil, fmt.Errorf("unable to get defaults for source %s: %v", c.owner.sourceName(i), err)
		}
		result = append(result, configs...)
	}
	return result, nil
}

func (c *combinedConfig) DeletionsManifests() ([]Manifest, error) {
	var result []Manifest
	for i, config := range c.configs {
		configs, err := config.DeletionsManifests()
		if err != nil {
			return nil, fmt.Errorf("unable to get deletions for source %s: %v", c.owner.sourceName(i), err)
		}
		result = append(result, configs...)
	}
	return result, nil
}

func (c *combinedConfig) Components() ([]Component, error) {
	var result []Component

	for i, config := range c.configs {
		components, err := config.Components()
		if err != nil {
			return nil, fmt.Errorf("unable to get components for source %s: %v", c.owner.sourceName(i), err)
		}
		for _, component := range components {
			result = append(result, Component{
				Name:      fmt.Sprintf("%s/%s", c.owner.sourceName(i), component.Name),
				Manifests: component.Manifests,
			})
		}
	}

	return result, nil
}

func (c *combinedConfig) Delete() error {
	var res error

	for i, config := range c.configs {
		err := config.Delete()
		if err != nil {
			logrus.Warnf("Unable to delete config for source %s: %v", c.owner.sourceName(i), err)
			res = err
		}
	}

	return res
}
