package main

import (
	"context"
	"fmt"
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
		runOnce(ctx, cfg, clusterRegistry, r)
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

func runOnce(ctx context.Context, cfg *reconciler.Config, clusterRegistry registry.Registry, r *reconciler.Reconciler) {
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

		res, err := r.Reconcile(ctx, spec)
		if err != nil {
			log.Errorf("Cluster %s: %v", spec.Alias, err)
			continue
		}
		logResult(res, cfg.DryRun)
	}
}

func logResult(res *reconciler.Result, dryRun bool) {
	changed := 0
	for _, o := range res.Objects {
		if o.Action != reconciler.ActionUnchanged {
			changed++
			log.Infof("Cluster %s: %s %s %s%s", res.Cluster, o.Kind, o.Name, o.Action, map[bool]string{true: " (dry run)", false: ""}[dryRun])
			if o.Diff != "" {
				fmt.Print(o.Diff)
			}
		}
	}
	msg := fmt.Sprintf("Cluster %s: %s, %s, %d of %d objects changed", res.Cluster, res.Stage, res.Status, changed, len(res.Objects))
	if res.Waiting != "" {
		msg += ", waiting: " + res.Waiting
	}
	// Quiet when nothing happens: this runs every few seconds.
	if changed > 0 || res.StatusChanged {
		log.Info(msg)
	} else {
		log.Debug(msg)
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
