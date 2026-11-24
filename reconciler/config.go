package reconciler

import (
	"time"

	kingpin "github.com/alecthomas/kingpin/v2"
)

const (
	defaultRegistry  = "file://clusters.yaml"
	defaultName      = "reconciler"
	defaultProvider  = "zalando-nebius"
	defaultNamespace = "flux-system"
	defaultInterval  = "1m"
)

// Config is the configuration of the reconciler.
type Config struct {
	// Registry is a registry URI: file://<path> for a local file, or an
	// http(s) URL for a cluster registry.
	Registry string
	// RegistryToken is the bearer token used for an http(s) registry.
	RegistryToken string
	// Name is the component name. The reconciler handles registry entries
	// whose config item "managed_by" equals it.
	Name string
	// Providers are the registry providers the reconciler is responsible for.
	Providers []string
	// Namespace is the host cluster namespace all resources are created in.
	Namespace string
	// Kubeconfig is the path to the kubeconfig of the host cluster. Empty
	// means in-cluster configuration.
	Kubeconfig string
	Interval   time.Duration
	Once       bool
	Debug      bool
}

// ParseFlags parses flags and environment variables into the config.
func (cfg *Config) ParseFlags() {
	kingpin.Flag("registry", "Cluster registry: file://<path> or the URL of a cluster registry.").Envar("REGISTRY_URL").Default(defaultRegistry).StringVar(&cfg.Registry)
	kingpin.Flag("registry-token", "Bearer token for an http(s) registry.").Envar("REGISTRY_TOKEN").StringVar(&cfg.RegistryToken)
	kingpin.Flag("name", "Component name. Handles registry entries with the config item managed_by set to this name.").Envar("COMPONENT_NAME").Default(defaultName).StringVar(&cfg.Name)
	kingpin.Flag("provider", "Provider of the registry entries to handle. Can be repeated.").Envar("PROVIDER").Default(defaultProvider).StringsVar(&cfg.Providers)
	kingpin.Flag("namespace", "Host cluster namespace for all created resources.").Envar("NAMESPACE").Default(defaultNamespace).StringVar(&cfg.Namespace)
	kingpin.Flag("kubeconfig", "Path to the kubeconfig of the host cluster. Defaults to in-cluster configuration.").Envar("KUBECONFIG").StringVar(&cfg.Kubeconfig)
	kingpin.Flag("interval", "Interval between loop iterations.").Default(defaultInterval).DurationVar(&cfg.Interval)
	kingpin.Flag("once", "Run a single iteration and exit.").BoolVar(&cfg.Once)
	kingpin.Flag("debug", "Enable debug logging.").BoolVar(&cfg.Debug)
	kingpin.Parse()
}
