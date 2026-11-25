package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const maxMessageLength = 1000

// Keys of the status ConfigMap.
const (
	StatusKeyPhase   = "phase"
	StatusKeyReason  = "reason"
	StatusKeyMessage = "message"
	StatusKeySince   = "since"
)

func truncate(s string) string {
	if len(s) > maxMessageLength {
		return s[:maxMessageLength] + "..."
	}
	return s
}

// reportStatus derives the phase of the cluster and reports it: to the status
// ConfigMap, to a Kubernetes Event on the Terraform resource and to the log.
// ConfigMap and Event are only written when the phase or reason changed, so a
// quiet cluster causes no writes. A dry run writes nothing.
func (r *Reconciler) reportStatus(ctx context.Context, spec *ClusterSpec, res *Result) error {
	n := resourceNames(spec)
	ns := r.Config.Namespace

	tf, err := r.Client.Get(ctx, gvrTerraform, ns, n.terraform)
	if err != nil {
		return fmt.Errorf("get Terraform %s: %w", n.terraform, err)
	}
	hr, err := r.Client.Get(ctx, gvrHelmRelease, ns, n.release)
	if err != nil {
		return fmt.Errorf("get HelmRelease %s: %w", n.release, err)
	}
	outputsReady, err := r.outputsReady(ctx, n.outputs)
	if err != nil {
		return err
	}

	st := Derive(StatusInput{
		Terraform: tf, HelmRelease: hr, OutputsReady: outputsReady,
		Approve: spec.ProvisionApprove, Now: r.now(), StuckAfter: r.StuckAfter,
	})
	res.Status = st

	prev, err := r.Client.Get(ctx, gvrConfigMap, ns, n.status)
	if err != nil {
		return fmt.Errorf("get ConfigMap %s: %w", n.status, err)
	}
	prevPhase, prevReason, _ := previousStatus(prev)
	res.StatusChanged = prevPhase != string(st.Phase) || prevReason != st.Reason
	if !res.StatusChanged {
		return nil
	}

	since := r.now().UTC().Format(time.RFC3339)
	log.Infof("Cluster %s: %s -> %s (%s)", spec.Alias, orNone(prevPhase), st, truncate(st.Message))
	if r.DryRun {
		return nil
	}

	cm := r.Config.Status(spec)
	cm.Object["data"] = map[string]interface{}{
		StatusKeyPhase:   string(st.Phase),
		StatusKeyReason:  st.Reason,
		StatusKeyMessage: truncate(st.Message),
		StatusKeySince:   since,
	}
	statusClient := r.StatusClient
	if statusClient == nil {
		statusClient = r.Client
	}
	if _, err := statusClient.Apply(ctx, cm, false); err != nil {
		return err
	}
	_, err = statusClient.Apply(ctx, r.event(spec, tf, st, since), false)
	return err
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// outputsReady tells if the outputs Secret has the endpoint and CA.
func (r *Reconciler) outputsReady(ctx context.Context, name string) (bool, error) {
	outputs, err := r.Client.Get(ctx, gvrSecret, r.Config.Namespace, name)
	if err != nil {
		return false, fmt.Errorf("get outputs Secret %s: %w", name, err)
	}
	if outputs == nil {
		return false, nil
	}
	endpoint, _ := secretValue(outputs, OutputEndpoint)
	ca, _ := secretValue(outputs, OutputCACertificate)
	return endpoint != "" && ca != "", nil
}

func previousStatus(cm *unstructured.Unstructured) (phase, reason, since string) {
	if cm == nil {
		return "", "", ""
	}
	data, _, _ := unstructured.NestedStringMap(cm.Object, "data")
	return data[StatusKeyPhase], data[StatusKeyReason], data[StatusKeySince]
}

// event renders a Kubernetes Event about the Terraform resource. Its name
// contains the phase and the time, so every change makes a new Event.
func (r *Reconciler) event(spec *ClusterSpec, tf *unstructured.Unstructured, st Status, since string) *unstructured.Unstructured {
	n := resourceNames(spec)
	typ := "Normal"
	if st.Phase == PhaseFailed || st.Reason == ReasonNoProgress {
		typ = "Warning"
	}
	involved := map[string]interface{}{
		"apiVersion": "infra.contrib.fluxcd.io/v1alpha2",
		"kind":       "Terraform",
		"name":       n.terraform,
		"namespace":  r.Config.Namespace,
	}
	if tf != nil && tf.GetUID() != "" {
		involved["uid"] = string(tf.GetUID())
	}
	reason := string(st.Phase)
	if st.Reason != "" && st.Phase == PhaseFailed {
		reason = st.Reason
	}
	ts, _ := time.Parse(time.RFC3339, since)
	e := r.Config.object("v1", "Event", fmt.Sprintf("%s.%s.%x", n.terraform, strings.ToLower(string(st.Phase)), ts.Unix()), spec)
	e.Object["type"] = typ
	e.Object["reason"] = reason
	e.Object["message"] = fmt.Sprintf("%s: %s", st, truncate(st.Message))
	e.Object["involvedObject"] = involved
	e.Object["source"] = map[string]interface{}{"component": r.Config.Name}
	e.Object["reportingComponent"] = r.Config.Name
	e.Object["firstTimestamp"] = since
	e.Object["lastTimestamp"] = since
	e.Object["count"] = int64(1)
	return e
}
