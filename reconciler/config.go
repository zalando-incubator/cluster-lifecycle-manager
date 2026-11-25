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
	defaultInterval  = "15s"
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
	// StuckAfter is how long a cluster may stay in an unfinished phase before
	// it is reported as making no progress.
	StuckAfter time.Duration
	// DryRun prints what would change instead of changing it.
	DryRun bool

	Render RenderConfig
}

// ParseFlags parses flags and environment variables into the config.
func (cfg *Config) ParseFlags() {
	kingpin.Flag("registry", "Cluster registry: file://<path> or the URL of a cluster registry.").Envar("REGISTRY_URL").Default(defaultRegistry).StringVar(&cfg.Registry)
	kingpin.Flag("registry-token", "Bearer token for an http(s) registry.").Envar("REGISTRY_TOKEN").StringVar(&cfg.RegistryToken)
	kingpin.Flag("name", "Component name. Handles registry entries with the config item managed_by set to this name.").Envar("COMPONENT_NAME").Default(defaultName).StringVar(&cfg.Name)
	kingpin.Flag("provider", "Provider of the registry entries to handle. Can be repeated.").Envar("PROVIDER").Default(defaultProvider).StringsVar(&cfg.Providers)
	kingpin.Flag("namespace", "Host cluster namespace for all created resources.").Envar("NAMESPACE").Default(defaultNamespace).StringVar(&cfg.Namespace)
	kingpin.Flag("kubeconfig", "Path to the kubeconfig of the host cluster. Defaults to in-cluster configuration.").Envar("HOST_KUBECONFIG").StringVar(&cfg.Kubeconfig)
	kingpin.Flag("interval", "Interval between loop iterations.").Default(defaultInterval).DurationVar(&cfg.Interval)
	kingpin.Flag("once", "Run a single iteration and exit.").BoolVar(&cfg.Once)
	kingpin.Flag("debug", "Enable debug logging.").BoolVar(&cfg.Debug)
	kingpin.Flag("stuck-after", "How long an unfinished phase may last before it is reported as making no progress.").Default(DefaultStuckAfter.String()).DurationVar(&cfg.StuckAfter)
	kingpin.Flag("dry-run", "Do not change anything, print a diff of what would change.").BoolVar(&cfg.DryRun)

	kingpin.Flag("runner-service-account", "Service account of the infrastructure runner pods.").Default("tofu-runner").StringVar(&cfg.Render.RunnerServiceAccount)
	kingpin.Flag("runner-credentials-secret", "Secret with the provider credentials mounted into the runner pods.").Default("nebius-service-account").StringVar(&cfg.Render.RunnerCredentialsSecret)
	kingpin.Flag("runner-credentials-env", "Environment variable that points the runner to the provider credentials file.").Default("NEBIUS_CREDENTIALS_FILE").StringVar(&cfg.Render.RunnerCredentialsEnv)
	kingpin.Flag("runner-credentials-mount-path", "Directory the provider credentials are mounted at in the runner pods.").Default("/var/run/secrets/nebius").StringVar(&cfg.Render.RunnerCredentialsMountPath)
	kingpin.Flag("runner-credentials-file", "File name of the provider credentials in the credentials Secret.").Default("credentials.json").StringVar(&cfg.Render.RunnerCredentialsFile)
	kingpin.Flag("cluster-token-secret", "Secret with the token used in the kubeconfigs of created clusters.").Default("nebius-deployment-credentials").StringVar(&cfg.Render.ClusterToken.Name)
	kingpin.Flag("cluster-token-secret-namespace", "Namespace of the token Secret.").Default("default").StringVar(&cfg.Render.ClusterToken.Namespace)
	kingpin.Flag("cluster-token-secret-key", "Key of the token in the token Secret.").Default("kubernetes-api-token-secret").StringVar(&cfg.Render.ClusterToken.Key)
	kingpin.Flag("oci-pull-secret", "Optional Secret with registry credentials for the infrastructure module repository.").StringVar(&cfg.Render.OCIPullSecret)
	kingpin.Parse()
	cfg.Render.Namespace = cfg.Namespace
	cfg.Render.Name = cfg.Name
}
