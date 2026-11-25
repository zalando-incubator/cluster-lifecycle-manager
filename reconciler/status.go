package reconciler

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Phase is how far a cluster has come.
type Phase string

const (
	PhasePending          Phase = "Pending"
	PhasePlanning         Phase = "Planning"
	PhaseAwaitingApproval Phase = "AwaitingApproval"
	PhaseApplying         Phase = "Applying"
	PhaseInfraReady       Phase = "InfraReady"
	PhaseDelivering       Phase = "Delivering"
	PhaseReady            Phase = "Ready"
	PhaseFailed           Phase = "Failed"
)

// ReasonNoProgress is the reason of a phase that has not changed for a long
// time while the underlying object reports no failure.
const ReasonNoProgress = "NoProgress"

// DefaultStuckAfter is how long an object may stay in a not yet finished state
// before it is reported as making no progress.
const DefaultStuckAfter = 15 * time.Minute

// Status is the derived state of a cluster, with the reason and message of the
// underlying object.
type Status struct {
	Phase   Phase
	Reason  string
	Message string
}

// String renders the phase as in the contract: Failed(<reason>) or the phase.
func (s Status) String() string {
	if s.Phase == PhaseFailed && s.Reason != "" {
		return fmt.Sprintf("Failed(%s)", s.Reason)
	}
	return string(s.Phase)
}

// StatusInput is what the phase is derived from.
type StatusInput struct {
	// Terraform and HelmRelease are nil if the object does not exist.
	Terraform   *unstructured.Unstructured
	HelmRelease *unstructured.Unstructured
	// OutputsReady is true if the outputs Secret has the endpoint and CA.
	OutputsReady bool
	// Approve is the config item provision_approve.
	Approve    string
	Now        time.Time
	StuckAfter time.Duration
}

type condition struct {
	Status         string
	Reason         string
	Message        string
	LastTransition time.Time
}

func conditionsOf(obj *unstructured.Unstructured) map[string]condition {
	out := map[string]condition{}
	if obj == nil {
		return out
	}
	items, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		c := condition{}
		c.Status, _ = m["status"].(string)
		c.Reason, _ = m["reason"].(string)
		c.Message, _ = m["message"].(string)
		if ts, _ := m["lastTransitionTime"].(string); ts != "" {
			c.LastTransition, _ = time.Parse(time.RFC3339, ts)
		}
		if t, _ := m["type"].(string); t != "" {
			out[t] = c
		}
	}
	return out
}

func failed(c condition) Status {
	reason := c.Reason
	if reason == "" {
		reason = "Failed"
	}
	return Status{Phase: PhaseFailed, Reason: reason, Message: c.Message}
}

// Derive derives the phase of a cluster from its Terraform resource, the
// outputs Secret and its HelmRelease. See terraform-status.md and
// helmrelease-status.md for the conditions it relies on.
//
// The Ready condition of the Terraform resource is not enough: it stays
// Unknown while the runner pod cannot start, and it can be Unknown while apply
// and outputs have already succeeded. Apply and Output decide whether the
// infrastructure exists.
func Derive(in StatusInput) Status {
	if in.StuckAfter <= 0 {
		in.StuckAfter = DefaultStuckAfter
	}

	if in.Terraform == nil {
		return Status{Phase: PhasePending, Message: "Terraform resource does not exist yet"}
	}
	tf := conditionsOf(in.Terraform)

	if c, ok := tf["Stalled"]; ok && c.Status == "True" {
		return failed(c)
	}
	// Plan=False with "no changes" is the success case.
	if c, ok := tf["Plan"]; ok && c.Status == "False" && c.Reason != "TerraformPlannedNoChanges" {
		return failed(c)
	}
	if c, ok := tf["Apply"]; ok && c.Status == "False" {
		return failed(c)
	}
	if c, ok := tf["Ready"]; ok && c.Status == "False" {
		return failed(c)
	}

	if c, ok := tf["Plan"]; ok && c.Status == "True" && c.Reason == "TerraformPlannedWithChanges" {
		if in.Approve == ApproveManual {
			return Status{Phase: PhaseAwaitingApproval, Reason: c.Reason, Message: c.Message}
		}
		return in.noProgress(Status{Phase: PhaseApplying, Reason: c.Reason, Message: c.Message}, tf["Ready"])
	}
	if c, ok := tf["Apply"]; ok && c.Status == "Unknown" {
		return in.noProgress(Status{Phase: PhaseApplying, Reason: c.Reason, Message: c.Message}, tf["Ready"])
	}

	apply, output := tf["Apply"], tf["Output"]
	if !(apply.Status == "True" && output.Status == "True" && in.OutputsReady) {
		ready, hasReady := tf["Ready"]
		st := Status{Phase: PhasePlanning, Reason: ready.Reason, Message: ready.Message}
		if !hasReady {
			st.Phase = PhasePending
		}
		return in.noProgress(st, ready)
	}

	// The infrastructure exists. What is left is the delivery.
	if in.HelmRelease == nil {
		return Status{Phase: PhaseInfraReady, Reason: apply.Reason, Message: "infrastructure is ready, the delivery is not created yet"}
	}
	hr := conditionsOf(in.HelmRelease)
	if c, ok := hr["Stalled"]; ok && c.Status == "True" {
		return failed(c)
	}
	ready := hr["Ready"]
	switch ready.Status {
	case "True":
		return Status{Phase: PhaseReady, Reason: ready.Reason, Message: ready.Message}
	case "False":
		return failed(ready)
	}
	return in.noProgress(Status{Phase: PhaseDelivering, Reason: ready.Reason, Message: ready.Message}, ready)
}

// noProgress marks a status that is not final and has not changed for too long.
func (in StatusInput) noProgress(st Status, ready condition) Status {
	if ready.LastTransition.IsZero() || in.Now.IsZero() {
		return st
	}
	if age := in.Now.Sub(ready.LastTransition); age > in.StuckAfter {
		st.Message = fmt.Sprintf("no progress for %s: %s", age.Truncate(time.Minute), st.Message)
		st.Reason = ReasonNoProgress
	}
	return st
}
