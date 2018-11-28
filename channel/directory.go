package channel

import (
	"context"

	log "github.com/sirupsen/logrus"
)

// Directory defines a channel source where everything is stored in a directory.
type Directory struct {
	location string
}

type directoryVersions struct{}

// NewDirectory initializes a new directory-based ChannelSource.
func NewDirectory(location string) ConfigSource {
	return &Directory{location: location}
}

func (d *Directory) Update(ctx context.Context, logger *log.Entry) (ConfigVersions, error) {
	result := &directoryVersions{}
	return result, nil
}

// Get returns the contents from the directory.
func (d *Directory) Get(ctx context.Context, logger *log.Entry, version ConfigVersion) (*Config, error) {
	return &Config{
		Path: d.location,
	}, nil
}

// Delete is a no-op for the directory channelSource.
func (d *Directory) Delete(logger *log.Entry, config *Config) error {
	return nil
}

func (d *directoryVersions) Version(channel string) (ConfigVersion, error) {
	return "<dir>", nil
}
