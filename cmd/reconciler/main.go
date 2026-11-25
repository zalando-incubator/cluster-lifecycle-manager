package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/zalando-incubator/cluster-lifecycle-manager/reconciler"
	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

func main() {
	cfg := &reconciler.Config{}
	cfg.ParseFlags()

	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var tokenSource oauth2.TokenSource
	if cfg.RegistryToken != "" {
		tokenSource = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.RegistryToken})
	}
	clusterRegistry := registry.NewRegistry(cfg.Registry, tokenSource, &registry.Options{})
	if clusterRegistry == nil {
		log.Fatalf("Invalid registry %q", cfg.Registry)
	}

	hostConfig, err := hostClusterConfig(cfg.Kubeconfig)
	if err != nil {
		log.Fatalf("Failed to configure the host cluster client: %v", err)
	}
	dyn, err := dynamic.NewForConfig(hostConfig)
	if err != nil {
		log.Fatalf("Failed to create the host cluster client: %v", err)
	}
	r := &reconciler.Reconciler{
		Client: reconciler.NewClient(dyn, cfg.Name),
		// The status has its own field manager, so applying the other
		// resources never touches the data of the status ConfigMap.
		StatusClient: reconciler.NewClient(dyn, cfg.Name+"-status"),
		Config:       cfg.Render,
		DryRun:       cfg.DryRun,
		StuckAfter:   cfg.StuckAfter,
	}

	for {
		failures := r.RunOnce(ctx, clusterRegistry, cfg)
		if cfg.Once {
			// A single run is used by hand and in checks: tell the caller
			// if anything failed.
			stop()
			if failures > 0 {
				log.Errorf("%d failure(s)", failures)
				os.Exit(1)
			}
			return
		}
		select {
		case <-ctx.Done():
			log.Info("Shutting down")
			return
		case <-time.After(cfg.Interval):
		}
	}
}

// hostClusterConfig returns the client configuration of the host cluster, from
// a kubeconfig file or in-cluster.
func hostClusterConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		return rest.InClusterConfig()
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
