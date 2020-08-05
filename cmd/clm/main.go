package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/zalando-incubator/cluster-lifecycle-manager/controller"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/credentials-loader/platformiam"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"github.com/zalando-incubator/cluster-lifecycle-manager/provisioner"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
	"golang.org/x/oauth2"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	provisionCmd    = kingpin.Command("provision", "Provision a cluster.")
	decommissionCmd = kingpin.Command("decommission", "Decommission a cluster.")
	controllerCmd   = kingpin.Command("controller", "Run controller loop.")
	version         = "unknown"
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

	awsConfig := aws.Config(cfg.AwsMaxRetries, cfg.AwsMaxRetryInterval)

	// setup aws session
	sess, err := aws.Session(awsConfig, "")
	if err != nil {
		log.Fatalf("Failed to setup AWS session: %v", err)
	}
	secretDecrypter := decrypter.SecretDecrypter(map[string]decrypter.Decrypter{
		decrypter.AWSKMSSecretPrefix: decrypter.NewAWSKMSDescrypter(sess),
	})

	rootLogger := log.StandardLogger().WithFields(map[string]interface{}{})

	execManager := command.NewExecManager(cfg.ConcurrentExternalProcesses)

	p := provisioner.NewClusterpyProvisioner(execManager, clusterTokenSource, secretDecrypter, cfg.AssumedRole, awsConfig, &provisioner.Options{
		DryRun:         cfg.DryRun,
		ApplyOnly:      cfg.ApplyOnly,
		UpdateStrategy: cfg.UpdateStrategy,
		RemoveVolumes:  cfg.RemoveVolumes,
	})

	configSource, err := setupConfigSource(execManager, cfg)
	if err != nil {
		log.Fatalf("Failed to setup channel config source: %v", err)
	}

	if cmd == controllerCmd.FullCommand() {
		log.Info("Running control loop")

		go startHTTPServer(cfg.Listen)

		opts := &controller.Options{
			AccountFilter:     cfg.AccountFilter,
			Interval:          cfg.Interval,
			DryRun:            cfg.DryRun,
			ConcurrentUpdates: cfg.ConcurrentUpdates,
		}

		ctrl := controller.New(rootLogger, execManager, clusterRegistry, p, configSource, opts)

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
			decryptedValue, err := secretDecrypter.Decrypt(value)
			if err != nil {
				log.Fatalf("%+v", err)
			}

			cluster.ConfigItems[key] = decryptedValue
		}

		switch cmd {
		case provisionCmd.FullCommand():
			log.Infof("Provisioning cluster %s", cluster.ID)
			err = p.Provision(context.Background(), rootLogger, cluster, config)
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
