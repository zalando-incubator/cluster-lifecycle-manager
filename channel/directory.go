package channel

// Directory defines a channel source where everything is stored in a directory.
type Directory struct {
	location string
}

// NewDirectory initializes a new directory-based ChannelSource.
func NewDirectory(location string) ConfigSource {
	return &Directory{location: location}
}

func (d *Directory) Update() error {
	return nil
}

// Get returns the contents from the directory.
func (d *Directory) Get(channel string) (*Config, error) {
	return &Config{
		Version: channel,
		Path:    d.location,
	}, nil
}

// Delete is a no-op for the directory channelSource.
func (d *Directory) Delete(config *Config) error {
	return nil
}
