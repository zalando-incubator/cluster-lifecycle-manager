package config

import (
	"fmt"
	"os"
	"path"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
)

const (
	defaultInterval              = "10m"
	defaultListener              = ":9090"
	defaultCredentialsDir        = "/meta/credentials"
	defaultRegistryTokenName     = "cluster-registry-rw"
	defaultClusterTokenName      = "cluster-rw"
	defaultRegistry              = "file://clusters.yaml"
	defaultConcurrentUpdates     = "1"
	defaultAwsMaxRetries         = "50"
	defaultAwsMaxRetryInterval   = "10s"
	defaultUpdateMaxEvictTimeout = "10m"
	defaultUpdateStrategy        = "rolling"
)

var defaultWorkdir = path.Join(os.TempDir(), "clm-workdir")

// LifecycleManagerConfig stores the configuration for app
type LifecycleManagerConfig struct {
	Registry            string
	AccountFilter       IncludeExcludeFilter
	Token               string
	RegistryTokenName   string
	ClusterTokenName    string
	AssumedRole         string
	Interval            time.Duration
	Debug               bool
	DumpRequest         bool
	DryRun              bool
	ConcurrentUpdates   uint
	Listen              string
	Workdir             string
	Directory           string
	GitRepositoryURL    string
	SSHPrivateKeyFile   string
	CredentialsDir      string
	EnvironmentOrder    []string
	ApplyOnly           bool
	AwsMaxRetries       int
	AwsMaxRetryInterval time.Duration
	UpdateStrategy      UpdateStrategy
	RemoveVolumes       bool
}

// UpdateStrategy defines the default update strategy configured for the
// Cluster Lifecycle Manager. It includes a named strategy and a max evict
// timeout. The default strategy can be overwritten with a config item per
// cluster.
type UpdateStrategy struct {
	Strategy        string
	MaxEvictTimeout time.Duration
}

// New returns the app wide configuration file
func New(version string) *LifecycleManagerConfig {
	kingpin.Version(version)
	return &LifecycleManagerConfig{} // populate the values not passed through the flags
}

// ValidateFlags for custom flag validation, e.g. check for the interval being not too short
func (cfg *LifecycleManagerConfig) ValidateFlags() error {
	if cfg.GitRepositoryURL == "" && cfg.Directory == "" {
		return fmt.Errorf("Either --git-repository-url or --directory must be specified")
	}
	return nil
}

// ParseFlags calls flag parsing. Might call termination handler in case if the kingpin internal validations are enabled.
func (cfg *LifecycleManagerConfig) ParseFlags() string {
	kingpin.Flag("registry", "The location of a cluster registry. This can either be a filepath to a clusters.yaml or an URL for a cluster registry.").Default(defaultRegistry).Short('f').StringVar(&cfg.Registry)
	kingpin.Flag("include", "Specify a regular expression to include accounts for provisioning.").Default(DefaultInclude).RegexpVar(&cfg.AccountFilter.Include)
	kingpin.Flag("exclude", "Specify a regular expression to exclude accounts for provisioning.").Default(DefaultExclude).RegexpVar(&cfg.AccountFilter.Exclude)
	kingpin.Flag("token", "The token to authenticate with.").StringVar(&cfg.Token)
	kingpin.Flag("registry-token-name", "Name of the token used when authenticating with a cluster registry.").Default(defaultRegistryTokenName).StringVar(&cfg.RegistryTokenName)
	kingpin.Flag("cluster-token-name", "Name of the token used when authenticating with a cluster.").Default(defaultClusterTokenName).StringVar(&cfg.ClusterTokenName)
	kingpin.Flag("assumed-role", "The role ARN to assume in target accounts.").StringVar(&cfg.AssumedRole)
	kingpin.Flag("interval", "The interval between iterations in Duration format, e.g. 60s.").Default(defaultInterval).DurationVar(&cfg.Interval)
	kingpin.Flag("debug", "Enable debug logging.").BoolVar(&cfg.Debug)
	kingpin.Flag("dump-request", "Enable logging http requests.").BoolVar(&cfg.DumpRequest)
	kingpin.Flag("dry-run", "Don't make any changes, just print.").BoolVar(&cfg.DryRun)
	kingpin.Flag("listen", "Address to listen at, e.g. :9090 or 0.0.0.0:9090").Default(defaultListener).StringVar(&cfg.Listen)
	kingpin.Flag("workdir", "Path to working directory used for storing channel configurations.").Default(defaultWorkdir).StringVar(&cfg.Workdir)
	kingpin.Flag("directory", "Path of a directory to use as channel config source.").StringVar(&cfg.Directory)
	kingpin.Flag("git-repository-url", "URL of the git repository to use as channel config source.").StringVar(&cfg.GitRepositoryURL)
	kingpin.Flag("concurrent-updates", "Number of updates allowed to run in parallel.").Default(defaultConcurrentUpdates).UintVar(&cfg.ConcurrentUpdates)
	kingpin.Flag("ssh-private-key-path", "Path to SSH private key used when pulling from a private git repository.").Envar("SSH_PRIVATE_KEY_PATH").StringVar(&cfg.SSHPrivateKeyFile)
	kingpin.Flag("credentials-dir", "Path to OAuth credentials").Envar("CREDENTIALS_DIR").Default(defaultCredentialsDir).StringVar(&cfg.CredentialsDir)
	kingpin.Flag("apply-only", "Enable apply only mode which will only apply CloudFormation stacks and manifests, but not do any rolling of nodes.").BoolVar(&cfg.ApplyOnly)
	kingpin.Flag("aws-max-retries", "Maximum number of retries for AWS SDK requests.").Default(defaultAwsMaxRetries).IntVar(&cfg.AwsMaxRetries)
	kingpin.Flag("aws-max-retry-interval", "Maximum interval between retries for AWS SDK requests.").Default(defaultAwsMaxRetryInterval).DurationVar(&cfg.AwsMaxRetryInterval)
	kingpin.Flag("update-max-evict-timeout", "Maximum timeout for evicting pods during update.").Default(defaultUpdateMaxEvictTimeout).DurationVar(&cfg.UpdateStrategy.MaxEvictTimeout)
	kingpin.Flag("update-strategy", "Update strategy to use when updating node pools.").Default(defaultUpdateStrategy).EnumVar(&cfg.UpdateStrategy.Strategy, "rolling")
	kingpin.Flag("remove-volumes", "Remove EBS volumes when decommissioning").BoolVar(&cfg.RemoveVolumes)
	kingpin.Flag("environment-order", "Roll out channel updates to the environments in a specific order").StringsVar(&cfg.EnvironmentOrder)
	return kingpin.Parse()
}
