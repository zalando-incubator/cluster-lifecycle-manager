package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"golang.org/x/oauth2"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/config"
	"github.com/zalando-incubator/cluster-lifecycle-manager/controller"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/credentials-loader/platformiam"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
	"github.com/zalando-incubator/cluster-lifecycle-manager/provisioner"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

var (
	provisionCmd    = kingpin.Command("provision", "Provision a cluster.")
	decommissionCmd = kingpin.Command("decommission", "Decommission a cluster.")
	controllerCmd   = kingpin.Command("controller", "Run controller loop.")
	version         = "unknown"
)

func main() {
	cfg := config.New(version)

	command := cfg.ParseFlags()

	if err := cfg.ValidateFlags(); err != nil {
		log.Fatalf("Incorrectly configured flag: %v", err)
	}

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

	p := provisioner.NewClusterpyProvisioner(clusterTokenSource, cfg.AssumedRole, awsConfig, &provisioner.Options{
		DryRun:         cfg.DryRun,
		ApplyOnly:      cfg.ApplyOnly,
		UpdateStrategy: cfg.UpdateStrategy,
		RemoveVolumes:  cfg.RemoveVolumes,
	})

	var configSource channel.ConfigSource

	if cfg.Directory != "" {
		configSource = channel.NewDirectory(cfg.Directory)
	} else {
		var err error
		configSource, err = channel.NewGit(cfg.Workdir, cfg.GitRepositoryURL, cfg.SSHPrivateKeyFile)
		if err != nil {
			log.Fatalf("Failed to setup git channel config source: %v", err)
		}
	}

	if command == controllerCmd.FullCommand() {
		log.Info("Running control loop")

		go serveHealthCheck(cfg.Listen)

		opts := &controller.Options{
			AccountFilter:     cfg.AccountFilter,
			Interval:          cfg.Interval,
			DryRun:            cfg.DryRun,
			SecretDecrypter:   secretDecrypter,
			ConcurrentUpdates: cfg.ConcurrentUpdates,
		}

		ctrl := controller.New(clusterRegistry, p, configSource, opts)

		ctx, cancel := context.WithCancel(context.Background())
		go handleSigterm(cancel)
		ctrl.Run(ctx)

		os.Exit(0)
	}

	clusters, err := clusterRegistry.ListClusters(registry.Filter{})
	if err != nil {
		log.Fatalf("%+v", err)
	}
	sortByEnvironmentPriority(clusters, cfg.EnvironmentOrder)

	for _, cluster := range clusters {
		if !cfg.AccountFilter.Allowed(cluster.InfrastructureAccount) {
			log.Debugf("Skipping %s cluster, infrastructure account does not match provided filter.", cluster.ID)
			continue
		}

		channels, err := configSource.Update()
		if err != nil {
			log.Fatalf("%+v", err)
		}

		version, err := channels.Version(cluster.Channel)
		if err != nil {
			log.Fatalf("%+v", err)
		}

		config, err := configSource.Get(version)
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

		switch command {
		case provisionCmd.FullCommand():
			log.Infof("Provisioning cluster %s", cluster.ID)
			err = p.Provision(cluster, config)
			if err != nil {
				log.Fatalf("Fail to provision: %v", err)
			}
			log.Infof("Provisioning done for cluster %s", cluster.ID)
		case decommissionCmd.FullCommand():
			log.Infof("Decommissioning cluster %s", cluster.ID)
			err = p.Decommission(cluster, config)
			if err != nil {
				log.Fatalf("Fail to decommission: %v", err)
			}
			log.Infof("Decommissioning done for cluster %s", cluster.ID)
		default:
			log.Fatalf("unknown command: %s", command)
		}
	}
}

func sortByEnvironmentPriority(clusters []*api.Cluster, environmentPriority []string) {
	computedPriorities := make(map[string]int)
	for i, env := range environmentPriority {
		computedPriorities[env] = i + 1
	}

	sort.SliceStable(clusters, func(i, j int) bool {
		iPriority := computedPriorities[clusters[i].Environment]
		jPriority := computedPriorities[clusters[j].Environment]
		return iPriority < jPriority
	})
}

func serveHealthCheck(listen string) {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.ListenAndServe(listen, nil)
}

func handleSigterm(cancelFunc func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-signals
	log.Info("Received Term signal. Terminating...")
	cancelFunc()
}
