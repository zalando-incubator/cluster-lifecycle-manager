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
	Get(ctx context.Context, logger *log.Entry, version ConfigVersion) (Config, error)
}

// ConfigVersions is a snapshot of the versions at the time of an update
type ConfigVersions interface {
	Version(channel string) (ConfigVersion, error)
}

type Manifest struct {
	Path     string
	Contents []byte
}

type Component struct {
	Name      string
	Manifests []Manifest
}

type Config interface {
	StackManifest(manifestName string) (Manifest, error)
	NodePoolManifest(profileName string, manifestName string) (Manifest, error)
	DefaultsManifests() ([]Manifest, error)
	DeletionsManifests() ([]Manifest, error)
	Components() ([]Component, error)

	Delete() error
}
