package channel

// ConfigSource is an interface for getting the cluster configuration for a
// certain channel.
type ConfigSource interface {
	// Update synchronizes the local copy of the configuration with the remote one.
	Update() error

	// Get returns a Config related to the specified channel from the local copy.
	Get(channel string) (*Config, error)

	// Delete deletes the config.
	Delete(config *Config) error
}

// Config defines the current version of the channel and the path to the
// directory of the channel configuration files.
type Config struct {
	Version string
	Path    string
}
