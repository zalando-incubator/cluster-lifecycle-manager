package channel

import (
	"context"

	log "github.com/sirupsen/logrus"
)

// Directory defines a channel source where everything is stored in a directory.
type Directory struct {
	name     string
	location string
}

// NewDirectory initializes a new directory-based ChannelSource.
func NewDirectory(name string, location string) (ConfigSource, error) {
	return &Directory{
		name:     name,
		location: location,
	}, nil
}

func (d *Directory) Name() string {
	return d.name
}

func (d *Directory) Update(ctx context.Context, logger *log.Entry) error {
	return nil
}

func (d *Directory) Version(channel string) (ConfigVersion, error) {
	return d, nil
}

func (d *Directory) ID() string {
	return "<dir>"
}

func (d *Directory) Get(ctx context.Context, logger *log.Entry) (Config, error) {
	return NewSimpleConfig(d.name, d.location, false)
}
