package config

import (
	"os"
	"path"
	"time"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/updatestrategy"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const (
	defaultInterval                         = "10m"
	defaultListener                         = ":9090"
	defaultCredentialsDir                   = "/meta/credentials"
	defaultRegistryTokenName                = "cluster-registry-rw"
	defaultClusterTokenName                 = "cluster-rw"
	defaultRegistry                         = "file://clusters.yaml"
	defaultConcurrentUpdates                = "1"
	defaultConcurrentExternalProcesses      = "10"
	defaultAwsMaxRetries                    = "50"
	defaultAwsMaxRetryInterval              = "10s"
	defaultDrainGracePeriod                 = "6h"
	defaultDrainMinPodLifetime              = "72h"
	defaultDrainMinHealthySiblingLifetime   = "1h"
	defaultDrainMinUnhealthySiblingLifetime = "6h"
	defaultDrainForceEvictInterval          = "5m"
	defaultDrainPollInterval                = "30s"
	defaultUpdateStrategy                   = "clc"
)

var defaultWorkdir = path.Join(os.TempDir(), "clm-workdir")

// LifecycleManagerConfig stores the configuration for app
type LifecycleManagerConfig struct {
	Registry                    string
	AccountFilter               IncludeExcludeFilter
	Token                       string
	RegistryTokenName           string
	ClusterTokenName            string
	AssumedRole                 string
	Interval                    time.Duration
	Debug                       bool
	DumpRequest                 bool
	DryRun                      bool
	ConcurrentUpdates           uint
	Listen                      string
	Workdir                     string
	Directory                   string
	ConfigSources               []string
	SSHPrivateKeyFile           string
	CredentialsDir              string
	ConcurrentExternalProcesses uint
	ApplyOnly                   bool
	AwsMaxRetries               int
	AwsMaxRetryInterval         time.Duration
	UpdateStrategy              UpdateStrategy
	RemoveVolumes               bool
	ManageEtcdStack             bool
}

// UpdateStrategy defines the default update strategy configured for the
// Cluster Lifecycle Manager. It includes a named strategy and a max evict
// timeout. The default strategy can be overwritten with a config item per
// cluster.
type UpdateStrategy struct {
	updatestrategy.DrainConfig
	Strategy string
}

// New returns the app wide configuration file
func New(version string) *LifecycleManagerConfig {
	kingpin.Version(version)
	return &LifecycleManagerConfig{} // populate the values not passed through the flags
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
	kingpin.Flag("config-source", "Config source specification (NAME:dir:PATH or NAME:git:URL). At least one is required.").StringsVar(&cfg.ConfigSources)
	kingpin.Flag("directory", "Use a single directory as a config source (for local/development use)").StringVar(&cfg.Directory)
	kingpin.Flag("concurrent-updates", "Number of updates allowed to run in parallel.").Default(defaultConcurrentUpdates).UintVar(&cfg.ConcurrentUpdates)
	kingpin.Flag("ssh-private-key-path", "Path to SSH private key used when pulling from a private git repository.").Envar("SSH_PRIVATE_KEY_PATH").StringVar(&cfg.SSHPrivateKeyFile)
	kingpin.Flag("credentials-dir", "Path to OAuth credentials").Envar("CREDENTIALS_DIR").Default(defaultCredentialsDir).StringVar(&cfg.CredentialsDir)
	kingpin.Flag("apply-only", "Enable apply only mode which will only apply CloudFormation stacks and manifests, but not do any rolling of nodes.").BoolVar(&cfg.ApplyOnly)
	kingpin.Flag("aws-max-retries", "Maximum number of retries for AWS SDK requests.").Default(defaultAwsMaxRetries).IntVar(&cfg.AwsMaxRetries)
	kingpin.Flag("aws-max-retry-interval", "Maximum interval between retries for AWS SDK requests.").Default(defaultAwsMaxRetryInterval).DurationVar(&cfg.AwsMaxRetryInterval)
	kingpin.Flag("drain-grace-period", "Interval between drain start and the first forced termination.").Default(defaultDrainGracePeriod).DurationVar(&cfg.UpdateStrategy.ForceEvictionGracePeriod)
	kingpin.Flag("drain-min-pod-lifetime", " Minimum lifetime of a pod that allows forced termination when draining.").Default(defaultDrainMinPodLifetime).DurationVar(&cfg.UpdateStrategy.MinPodLifetime)
	kingpin.Flag("drain-min-healthy-sibling-lifetime", "Minimum lifetime of healthy pods in the same PDB as the one considered for forced termination.").Default(defaultDrainMinHealthySiblingLifetime).DurationVar(&cfg.UpdateStrategy.MinHealthyPDBSiblingLifetime)
	kingpin.Flag("drain-min-unhealthy-sibling-lifetime", "Minimum lifetime of unhealthy pods in the same PDB as the one considered for forced termination.").Default(defaultDrainMinUnhealthySiblingLifetime).DurationVar(&cfg.UpdateStrategy.MinUnhealthyPDBSiblingLifetime)
	kingpin.Flag("drain-force-evict-interval", "Interval between forced terminations of pods on the same node.").Default(defaultDrainForceEvictInterval).DurationVar(&cfg.UpdateStrategy.ForceEvictionInterval)
	kingpin.Flag("drain-poll-interval", "Interval between drain attempts.").Default(defaultDrainPollInterval).DurationVar(&cfg.UpdateStrategy.PollInterval)
	kingpin.Flag("update-strategy", "Update strategy to use when updating node pools.").Default(defaultUpdateStrategy).EnumVar(&cfg.UpdateStrategy.Strategy, "rolling", "clc")
	kingpin.Flag("remove-volumes", "Remove EBS volumes when decommissioning.").BoolVar(&cfg.RemoveVolumes)
	kingpin.Flag("concurrent-external-processes", "Number of external processes allowed to run in parallel").Default(defaultConcurrentExternalProcesses).UintVar(&cfg.ConcurrentExternalProcesses)
	kingpin.Flag("manage-etcd-stack", "Enable creation/updates of the etcd stack (should be disabled for pet clusters)").BoolVar(&cfg.ManageEtcdStack)
	return kingpin.Parse()
}
