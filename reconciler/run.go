package reconciler

import (
	"context"
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/zalando-incubator/cluster-lifecycle-manager/registry"
)

// RunOnce lists the registry, selects the clusters the reconciler is
// responsible for and reconciles each of them. It returns the number of
// failures: a failed registry listing counts as one, and so does every
// selected cluster that could not be parsed or reconciled. A cluster that is
// in the phase Failed is a state to report, not a failure of the reconciler.
func (r *Reconciler) RunOnce(ctx context.Context, clusterRegistry registry.Registry, cfg *Config) int {
	clusters, err := clusterRegistry.ListClusters(registry.Filter{})
	if err != nil {
		log.Errorf("Failed to list clusters: %v", err)
		return 1
	}

	selected, skipped := Select(clusters, cfg.Providers, cfg.Name)
	for _, s := range skipped {
		if s.OtherProvider {
			log.Debugf("Skipping cluster %s: %s", s.Alias, s.Reason)
		} else {
			log.Infof("Skipping cluster %s: %s", s.Alias, s.Reason)
		}
	}

	failures := 0
	for _, cluster := range selected {
		spec, err := ParseSpec(cluster)
		if err != nil {
			log.Errorf("%v", err)
			failures++
			continue
		}

		res, err := r.Reconcile(ctx, spec)
		if res != nil {
			r.logResult(res)
		}
		if err != nil {
			log.Errorf("Cluster %s: %v", spec.Alias, err)
			failures++
		}
	}
	return failures
}

func (r *Reconciler) logResult(res *Result) {
	out := r.Diffs
	if out == nil {
		out = os.Stdout
	}

	changed := 0
	for _, o := range res.Objects {
		if o.Action == ActionUnchanged {
			continue
		}
		changed++
		suffix := ""
		if r.DryRun {
			suffix = " (dry run)"
		}
		log.Infof("Cluster %s: %s %s %s%s", res.Cluster, o.Kind, o.Name, o.Action, suffix)
		if o.Diff != "" {
			fmt.Fprint(out, o.Diff)
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
