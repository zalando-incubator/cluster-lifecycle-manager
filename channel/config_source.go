package channel

import (
	"context"

	log "github.com/sirupsen/logrus"
)

type ConfigVersion string

// ConfigSource is an interface for getting the cluster configuration for a
// certain channel.
type ConfigSource interface {
	// Update synchronizes the local copy of the configuration with the remote one
	// and returns the available channel versions.
	Update(ctx context.Context, logger *log.Entry) (ConfigVersions, error)

	// Get returns a Config related to the specified version from the local copy.
	Get(ctx context.Context, logger *log.Entry, version ConfigVersion) (*Config, error)

	// Delete deletes the config.
	Delete(logger *log.Entry, config *Config) error
}

// ConfigVersions is a snapshot of the versions at the time of an update
type ConfigVersions interface {
	Version(channel string) (ConfigVersion, error)
}

// Config defines the path to the directory of the channel configuration files.
type Config struct {
	Path string
}
