package channel

import (
	"context"

	log "github.com/sirupsen/logrus"
)

type ConfigVersion interface {
	// ID returns this version's unique ID (e.g. a git commit SHA)
	ID() string

	// Get returns a Config related to the specified version from the local copy.
	Get(ctx context.Context, logger *log.Entry) (Config, error)
}

// ConfigSource is an interface for getting the cluster configuration for a
// certain channel.
type ConfigSource interface {
	// Name of the config source
	Name() string

	// Update synchronizes the local copy of the configuration with the remote one.
	Update(ctx context.Context, logger *log.Entry) error

	// Version returns the ConfigVersion for the corresponding channel (or overrides, if this is a
	// combined source)
	Version(channel string, overrides map[string]string) (ConfigVersion, error)
}

// GarbageCollector is an optional interface for config sources that support
// garbage collection. Sources should implement this to support deferred
// garbage collection after all updates are complete.
type GarbageCollector interface {
	GarbageCollect(ctx context.Context, logger *log.Entry) error
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
	EtcdManifest(manifestName string) (Manifest, error)
	CFManifests() ([]Manifest, error)
	NodePoolManifest(profileName string, manifestName string) (Manifest, error)
	DefaultsManifests() ([]Manifest, error)
	DeletionsManifests() ([]Manifest, error)
	CFDeletionsManifests() ([]Manifest, error)
	Components() ([]Component, error)
	Delete() error
}
