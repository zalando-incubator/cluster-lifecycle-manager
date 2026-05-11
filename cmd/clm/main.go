package main

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"gopkg.in/yaml.v2"

	kingpin "github.com/alecthomas/kingpin/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/zalando-incubator/cluster-lifecycle-manager/controller"
	zaws "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/credentials-loader/platformiam"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"github.com/zalando-incubator/cluster-lifecycle-manager/provisioner"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
	"golang.org/x/oauth2"
)

var (
	provisionCmd       = kingpin.Command("provision", "Provision a cluster.")
	decommissionCmd    = kingpin.Command("decommission", "Decommission a cluster.")
	controllerCmd      = kingpin.Command("controller", "Run controller loop.")
	renderCmd          = kingpin.Command("render-with-aws-stup", "Render templates from a config directory with stups for AWS values and print to stdout; To be used to add local development.")
	renderComponents   = renderCmd.Flag("component", "Only render the named component folder(s). Can be specified multiple times.").Strings()
	renderClusterAlias = renderCmd.Flag("cluster-alias", "Only render specified clusters").Required().String()
	renderConfigsFile  = renderCmd.Flag("configs-file", "YAML file with config-items overrides; only manifests whose output changes are printed.").String()
	version            = "unknown"
)

func setupConfigSource(exec *command.ExecManager, cfg *config.LifecycleManagerConfig) (channel.ConfigSource, error) {
	if cfg.Directory != "" {
		if len(cfg.ConfigSources) > 0 {
			log.Fatalf("--directory can't be used with --config-source")
		}
		return channel.NewDirectory("main", cfg.Directory)
	}

	var sources []channel.ConfigSource

	for _, source := range cfg.ConfigSources {
		parts := strings.SplitN(source, ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid config source definition: %s", source)
		}

		name := parts[0]

		switch parts[1] {
		case "dir", "directory":
			dir, err := channel.NewDirectory(name, parts[2])
			if err != nil {
				return nil, fmt.Errorf("unable to setup directory config source %s: %v", name, err)
			}
			sources = append(sources, dir)
		case "git":
			git, err := channel.NewGit(exec, name, cfg.Workdir, parts[2], cfg.SSHPrivateKeyFile)
			if err != nil {
				return nil, fmt.Errorf("unable to setup git config source %s: %v", name, err)
			}
			sources = append(sources, git)
		default:
			return nil, fmt.Errorf("unknown config source type %s", parts[1])
		}
	}

	switch len(sources) {
	case 0:
		return nil, fmt.Errorf("at least one --config-source is required")
	case 1:
		return sources[0], nil
	default:
		return channel.NewCombinedSource(sources)
	}
}

type configsFile struct {
	ConfigItems map[string]any `json:"config-items" yaml:"config-items"`
}

func filterStringConfigs(configs map[string]any) map[string]string {
	filteredConfigs := make(map[string]string)
	for k, v := range configs {
		switch castedV := v.(type) {
		case string:
			filteredConfigs[k] = castedV
		default:
			continue
		}
	}
	return filteredConfigs
}

func loadConfigsFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", path, err)
	}
	var vf configsFile
	if err := yaml.Unmarshal(data, &vf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal file %q: %w", path, err)
	}

	return filterStringConfigs(vf.ConfigItems), nil
}

func stubAWSRenderValues() map[string]any {
	vpcIPv6CIDRs := []string{}
	subnetIPv6CIDRs := ""
	vpcIPv6CIDRs = []string{"2001:db8::/56"}
	subnetIPv6CIDRs = "2001:db8:0:1::/64,2001:db8:0:2::/64"
	return map[string]any{
		"subnets":                   map[string]string{"eu-central-1a": "subnet-00000001", "eu-central-1b": "subnet-00000002"},
		"availability_zones":        []string{"eu-central-1a", "eu-central-1b"},
		"subnet_ipv6_cidrs":         subnetIPv6CIDRs,
		"lb_subnets":                map[string]string{"eu-central-1a": "subnet-00000003"},
		"internal_node_subnets":     map[string]string{"eu-central-1a": "subnet-00000004"},
		"pod_subnets":               map[string]string{"eu-central-1a": "subnet-00000005"},
		"hosted_zone":               "example.com",
		"load_balancer_certificate": "arn:aws:acm:eu-central-1:000000000000:certificate/stub",
		"vpc_ipv4_cidr":             "172.31.0.0/16",
		"vpc_ipv6_cidrs":            vpcIPv6CIDRs,
		"etcd_kms_key_arn":          "arn:aws:kms:eu-central-1:000000000000:key/stub",
		"ClusterStackOutputs":       map[string]string{},
		"S3GeneratedFilesPath":      "s3://stub-bucket/stub-path",
	}
}

func main() {
	cfg := config.New(version)

	cmd := cfg.ParseFlags()

	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	}

	var registryTokenSource, clusterTokenSource oauth2.TokenSource

	if cfg.Token != "" {
		registryTokenSource = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.Token})
		clusterTokenSource = registryTokenSource
	} else {
		// tokenSource used when connecting to a cluster registry.
		registryTokenSource = platformiam.NewTokenSource(cfg.RegistryTokenName, cfg.CredentialsDir)
		// tokenSource used when connecting to a cluster API Server.
		clusterTokenSource = platformiam.NewTokenSource(cfg.ClusterTokenName, cfg.CredentialsDir)
	}

	clusterRegistry := registry.NewRegistry(cfg.Registry, registryTokenSource, &registry.Options{Debug: cfg.DumpRequest})

	ctx := context.Background()

	awsConfigOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.AddWithMaxBackoffDelay(
				retry.AddWithMaxAttempts(
					retry.NewStandard(),
					cfg.AwsMaxRetries,
				),
				cfg.AwsMaxRetryInterval,
			)
		}),
	}

	// setup aws config
	awsConfig, err := zaws.Config(ctx, "", awsConfigOptions...)
	if err != nil {
		log.Fatalf("Failed to setup AWS session: %v", err)
	}
	secretDecrypter := decrypter.SecretDecrypter(map[string]decrypter.Decrypter{
		decrypter.AWSKMSSecretPrefix: decrypter.NewAWSKMSDescrypter(awsConfig),
	})

	rootLogger := log.StandardLogger().WithFields(map[string]any{})

	execManager := command.NewExecManager(cfg.ConcurrentExternalProcesses)

	provisioners := map[api.ProviderID]provisioner.Provisioner{
		api.ZalandoAWSProvider: provisioner.NewZalandoAWSProvisioner(
			execManager,
			clusterTokenSource,
			secretDecrypter,
			cfg.AssumedRole,
			awsConfigOptions,
			&provisioner.Options{
				DryRun:          cfg.DryRun,
				ApplyOnly:       cfg.ApplyOnly,
				UpdateStrategy:  cfg.UpdateStrategy,
				RemoveVolumes:   cfg.RemoveVolumes,
				ManageEtcdStack: cfg.ManageEtcdStack,
			},
		),
		api.ZalandoEKSProvider: provisioner.NewZalandoEKSProvisioner(
			execManager,
			secretDecrypter,
			cfg.AssumedRole,
			awsConfigOptions,
			&provisioner.Options{
				DryRun:         cfg.DryRun,
				ApplyOnly:      cfg.ApplyOnly,
				UpdateStrategy: cfg.UpdateStrategy,
				RemoveVolumes:  cfg.RemoveVolumes,
				Hook:           provisioner.NewZalandoEKSCreationHook(clusterRegistry),
			},
		),
	}

	configSource, err := setupConfigSource(execManager, cfg)
	if err != nil {
		log.Fatalf("Failed to setup channel config source: %v", err)
	}

	if cmd == renderCmd.FullCommand() {
		if err := runRenderCmd(ctx, clusterRegistry, configSource, rootLogger); err != nil {
			log.Fatalf("Error running render command %v", err)
		}
		os.Exit(0)
	}

	if cmd == controllerCmd.FullCommand() {
		log.Info("Running control loop")

		go startHTTPServer(cfg.Listen)

		opts := &controller.Options{
			AccountFilter:     cfg.AccountFilter,
			Interval:          cfg.Interval,
			DryRun:            cfg.DryRun,
			Providers:         cfg.Providers,
			ConcurrentUpdates: cfg.ConcurrentUpdates,
		}

		ctrl := controller.New(
			rootLogger,
			execManager,
			clusterRegistry,
			provisioners,
			configSource,
			opts,
		)

		ctx, cancel := context.WithCancel(context.Background())
		go handleSigterm(cancel)
		ctrl.Run(ctx)

		os.Exit(0)
	}

	clusters, err := clusterRegistry.ListClusters(registry.Filter{})
	if err != nil {
		log.Fatalf("%+v", err)
	}
	for _, cluster := range clusters {
		if !cfg.AccountFilter.Allowed(cluster.InfrastructureAccount) {
			log.Debugf("Skipping %s cluster, infrastructure account does not match provided filter.", cluster.ID)
			continue
		}

		err := configSource.Update(context.Background(), rootLogger)
		if err != nil {
			log.Fatalf("%+v", err)
		}

		channelOverrides, err := cluster.ChannelOverrides()
		if err != nil {
			log.Fatalf("%+v", err)
		}

		version, err := configSource.Version(cluster.Channel, channelOverrides)
		if err != nil {
			log.Fatalf("%+v", err)
		}

		config, err := version.Get(context.Background(), rootLogger)
		if err != nil {
			log.Fatalf("%+v", err)
		}

		for key, value := range cluster.ConfigItems {
			decryptedValue, err := secretDecrypter.Decrypt(ctx, value)
			if err != nil {
				log.Fatalf("%+v", err)
			}

			cluster.ConfigItems[key] = decryptedValue
		}

		p, ok := provisioners[cluster.Provider]
		if !ok {
			log.Fatalf(
				"Cluster %s: unknown provider %q",
				cluster.ID,
				cluster.Provider,
			)
		}

		switch cmd {
		case provisionCmd.FullCommand():
			log.Infof("Provisioning cluster %s", cluster.ID)
			err = p.Provision(
				context.Background(),
				rootLogger,
				cluster,
				config,
			)
			if err != nil {
				log.Fatalf("Fail to provision: %v", err)
			}
			log.Infof("Provisioning done for cluster %s", cluster.ID)
		case decommissionCmd.FullCommand():
			log.Infof("Decommissioning cluster %s", cluster.ID)
			err = p.Decommission(context.Background(), rootLogger, cluster)
			if err != nil {
				log.Fatalf("Fail to decommission: %v", err)
			}
			log.Infof("Decommissioning done for cluster %s", cluster.ID)
		default:
			log.Fatalf("unknown cmd: %s", cmd)
		}
	}
}

func runRenderCmd(
	ctx context.Context,
	clusterRegistry registry.Registry,
	configSource channel.ConfigSource,
	rootLogger *log.Entry,
) error {
	if *renderClusterAlias == "" {
		return fmt.Errorf("Render command require non empty cluster alias")
	}

	clusters, err := clusterRegistry.ListClusters(registry.Filter{})
	if err != nil {
		return fmt.Errorf("%+v", err)
	}

	var cluster *api.Cluster
	for _, c := range clusters {
		if c.Alias == *renderClusterAlias {
			cluster = c
			break
		}
	}

	if err := configSource.Update(ctx, rootLogger); err != nil {
		return fmt.Errorf("%+v", err)
	}

	if cluster == nil {
		return fmt.Errorf("Cluster with alias %q not found in registry", *renderClusterAlias)
	}

	awsStubValues := stubAWSRenderValues()

	channelOverrides, err := cluster.ChannelOverrides()
	if err != nil {
		return fmt.Errorf("%+v", err)
	}
	version, err := configSource.Version(cluster.Channel, channelOverrides)
	if err != nil {
		return fmt.Errorf("%+v", err)
	}
	channelConfig, err := version.Get(ctx, rootLogger)
	if err != nil {
		return fmt.Errorf("%+v", err)
	}

	defer func() error {
		err := channelConfig.Delete()
		if err != nil {
			return fmt.Errorf("%+v", err)
		}
		return nil
	}()

	log.Debugf("=== Cluster %s ===\n", cluster.Alias)
	if err := provisioner.ApplyDefaults(channelConfig, cluster); err != nil {
		log.Warnf("Error applying defaults for %s: %v", cluster.ID, err)
	}

	filterComponents := make(map[string]bool, len(*renderComponents))
	for _, c := range *renderComponents {
		filterComponents[c] = true
	}

	printComponents := func(components []provisioner.RenderedComponent, baselineIdx map[string][]*kubernetes.ResourceManifest) {
		for _, comp := range components {
			if len(filterComponents) > 0 && !filterComponents[comp.Name] {
				continue
			}
			for i, m := range comp.Manifests {
				if baselineIdx != nil {
					if base := baselineIdx[comp.Name]; i < len(base) && m == base[i] {
						continue
					}
				}
				contents, err := m.ToYaml()
				if err != nil {
					log.Fatalf("failed to marshal file contents %v", err)
				}
				fmt.Printf("---\n%s\n", contents)
			}
		}
	}

	if *renderConfigsFile != "" {
		overrides, err := loadConfigsFile(*renderConfigsFile)
		if err != nil {
			return fmt.Errorf("failed to load values file: %v", err)
		}
		overrideCluster := *cluster
		overrideCluster.ConfigItems = make(map[string]string, len(cluster.ConfigItems))
		maps.Copy(overrideCluster.ConfigItems, cluster.ConfigItems)
		baseline, err := provisioner.RenderManifests(channelConfig, &overrideCluster, awsStubValues)
		if err != nil {
			return fmt.Errorf("error rendering baseline for %s: %v", cluster.ID, err)
		}
		maps.Copy(overrideCluster.ConfigItems, overrides)
		changed, err := provisioner.RenderManifests(channelConfig, &overrideCluster, awsStubValues)
		if err != nil {
			return fmt.Errorf("Error rendering overrides for %s: %v", cluster.ID, err)
		}

		// Only show changed files
		baselineIdx := make(map[string][]*kubernetes.ResourceManifest, len(baseline))
		for _, comp := range baseline {
			baselineIdx[comp.Name] = comp.Manifests
		}
		printComponents(changed, baselineIdx)
	} else {
		components, err := provisioner.RenderManifests(channelConfig, cluster, awsStubValues)
		if err != nil {
			return fmt.Errorf("Error rendering manifests for %s: %v", cluster.ID, err)
		}
		printComponents(components, nil)
	}
	return nil
}

func startHTTPServer(listen string) {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	log.Fatal(http.ListenAndServe(listen, nil))
}

func handleSigterm(cancelFunc func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-signals
	log.Info("Received Term signal. Terminating...")
	cancelFunc()
}
