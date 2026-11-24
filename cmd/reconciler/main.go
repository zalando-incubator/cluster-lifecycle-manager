package main

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"

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

	for {
		runOnce(cfg, clusterRegistry)
		if cfg.Once {
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

func runOnce(cfg *reconciler.Config, clusterRegistry registry.Registry) {
	clusters, err := clusterRegistry.ListClusters(registry.Filter{})
	if err != nil {
		log.Errorf("Failed to list clusters: %v", err)
		return
	}

	selected, skipped := reconciler.Select(clusters, cfg.Providers, cfg.Name)
	for _, s := range skipped {
		if s.OtherProvider {
			log.Debugf("Skipping cluster %s: %s", s.Alias, s.Reason)
		} else {
			log.Infof("Skipping cluster %s: %s", s.Alias, s.Reason)
		}
	}

	for _, cluster := range selected {
		spec, err := reconciler.ParseSpec(cluster)
		if err != nil {
			log.Errorf("%v", err)
			continue
		}
		log.Infof("Cluster %s selected (module %s@%s, chart %s %s)", spec.Alias,
			spec.ProvisionModuleRepo, spec.ProvisionModuleRef, spec.DeliveryChart, spec.DeliveryChartVersion)
	}
}
