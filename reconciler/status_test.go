package reconciler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var statusNow = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

type cond struct {
	Type, Status, Reason, Message string
	Age                           time.Duration // time since the last transition
}

func objWith(kind string, conds ...cond) *unstructured.Unstructured {
	items := []interface{}{}
	for _, c := range conds {
		items = append(items, map[string]interface{}{
			"type": c.Type, "status": c.Status, "reason": c.Reason, "message": c.Message,
			"lastTransitionTime": statusNow.Add(-c.Age).Format(time.RFC3339),
		})
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"kind":   kind,
		"status": map[string]interface{}{"conditions": items},
	}}
}

func tfObj(conds ...cond) *unstructured.Unstructured { return objWith("Terraform", conds...) }
func hrObj(conds ...cond) *unstructured.Unstructured { return objWith("HelmRelease", conds...) }

// The conditions of the live noop-smoke-test object (25 Nov), apply succeeded.
var smokeTest = []cond{
	{"Reconciling", "True", "Progressing", "Fulfilling prerequisites", time.Minute},
	{"Ready", "Unknown", "Progressing", "Initializing", time.Minute},
	{"Output", "True", "TerraformOutputsAvailable", "Outputs available", time.Hour},
	{"Plan", "False", "TerraformPlannedNoChanges", "Plan no changes", time.Hour},
	{"Apply", "True", "TerraformAppliedSucceed", "Applied successfully", time.Hour},
}

func TestDerive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     StatusInput
		phase  Phase
		reason string
		str    string
	}{
		{"no terraform resource", StatusInput{}, PhasePending, "", "Pending"},
		{"no conditions yet", StatusInput{Terraform: tfObj()}, PhasePending, "", "Pending"},
		{"planning", StatusInput{Terraform: tfObj(cond{"Ready", "Unknown", "Progressing", "Reconciliation in progress", time.Minute})},
			PhasePlanning, "Progressing", "Planning"},
		{"planning, stuck", StatusInput{Terraform: tfObj(cond{"Ready", "Unknown", "Progressing", "Initializing", 18 * time.Hour})},
			PhasePlanning, ReasonNoProgress, "Planning"},
		{"plan with changes, auto approval", StatusInput{Approve: "auto", Terraform: tfObj(
			cond{"Ready", "Unknown", "Progressing", "", time.Minute},
			cond{"Plan", "True", "TerraformPlannedWithChanges", "Plan generated", time.Minute})},
			PhaseApplying, "TerraformPlannedWithChanges", "Applying"},
		{"plan with changes, manual approval", StatusInput{Approve: "manual", Terraform: tfObj(
			cond{"Ready", "Unknown", "Progressing", "", time.Minute},
			cond{"Plan", "True", "TerraformPlannedWithChanges", "Plan generated", time.Hour})},
			PhaseAwaitingApproval, "TerraformPlannedWithChanges", "AwaitingApproval"},
		{"apply running", StatusInput{Terraform: tfObj(
			cond{"Ready", "Unknown", "Progressing", "", time.Minute},
			cond{"Apply", "Unknown", "Progressing", "Applying", time.Minute})},
			PhaseApplying, "Progressing", "Applying"},
		{"plan failed", StatusInput{Terraform: tfObj(
			cond{"Ready", "Unknown", "Progressing", "", time.Minute},
			cond{"Plan", "False", "TFExecPlanFailed", "invalid variable", time.Minute})},
			PhaseFailed, "TFExecPlanFailed", "Failed(TFExecPlanFailed)"},
		{"apply failed", StatusInput{Terraform: tfObj(cond{"Apply", "False", "TerraformAppliedFail", "quota exceeded", time.Minute})},
			PhaseFailed, "TerraformAppliedFail", "Failed(TerraformAppliedFail)"},
		{"stalled", StatusInput{Terraform: tfObj(cond{"Stalled", "True", "RetryLimitReached", "Resource reached maximum number of retries.", time.Minute})},
			PhaseFailed, "RetryLimitReached", "Failed(RetryLimitReached)"},
		{"state lock", StatusInput{Terraform: tfObj(cond{"Ready", "False", "LockHeld", "state locked", time.Minute})},
			PhaseFailed, "LockHeld", "Failed(LockHeld)"},

		// the live object: Ready is Unknown although apply and outputs succeeded
		{"infrastructure ready although Ready is Unknown", StatusInput{Terraform: tfObj(smokeTest...), OutputsReady: true},
			PhaseInfraReady, "TerraformAppliedSucceed", "InfraReady"},
		{"apply succeeded but the outputs Secret is not complete", StatusInput{Terraform: tfObj(smokeTest...), OutputsReady: false},
			PhasePlanning, "Progressing", "Planning"},
		{"new plan with changes after a successful apply", StatusInput{Approve: "auto", OutputsReady: true, Terraform: tfObj(
			cond{"Apply", "True", "TerraformAppliedSucceed", "", 2 * time.Hour},
			cond{"Output", "True", "TerraformOutputsAvailable", "", 2 * time.Hour},
			cond{"Plan", "True", "TerraformPlannedWithChanges", "", time.Minute},
			cond{"Ready", "Unknown", "Progressing", "", time.Minute})},
			PhaseApplying, "TerraformPlannedWithChanges", "Applying"},

		{"delivering", StatusInput{Terraform: tfObj(smokeTest...), OutputsReady: true, HelmRelease: hrObj(
			cond{"Ready", "Unknown", "Progressing", "Reconciliation in progress", time.Minute})},
			PhaseDelivering, "Progressing", "Delivering"},
		{"delivering, stuck", StatusInput{Terraform: tfObj(smokeTest...), OutputsReady: true, HelmRelease: hrObj(
			cond{"Ready", "Unknown", "Progressing", "Reconciliation in progress", time.Hour})},
			PhaseDelivering, ReasonNoProgress, "Delivering"},
		{"ready", StatusInput{Terraform: tfObj(smokeTest...), OutputsReady: true, HelmRelease: hrObj(
			cond{"Ready", "True", "InstallSucceeded", "Helm install succeeded", time.Minute},
			cond{"Released", "True", "InstallSucceeded", "Helm install succeeded", time.Minute})},
			PhaseReady, "InstallSucceeded", "Ready"},
		{"install failed", StatusInput{Terraform: tfObj(smokeTest...), OutputsReady: true, HelmRelease: hrObj(
			cond{"Ready", "False", "InstallFailed", "chart not found", time.Minute})},
			PhaseFailed, "InstallFailed", "Failed(InstallFailed)"},
		{"helm release stalled", StatusInput{Terraform: tfObj(smokeTest...), OutputsReady: true, HelmRelease: hrObj(
			cond{"Stalled", "True", "InstallFailed", "retries exhausted", time.Minute})},
			PhaseFailed, "InstallFailed", "Failed(InstallFailed)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Now = statusNow
			got := Derive(tc.in)
			require.Equal(t, tc.phase, got.Phase)
			require.Equal(t, tc.reason, got.Reason)
			require.Equal(t, tc.str, got.String())
		})
	}
}

func TestDeriveInfraReadyWithoutHelmRelease(t *testing.T) {
	got := Derive(StatusInput{Terraform: tfObj(smokeTest...), OutputsReady: true, Now: statusNow})
	require.Equal(t, PhaseInfraReady, got.Phase)
	require.Contains(t, got.Message, "delivery is not created yet")
}

func TestDeriveStuckMessage(t *testing.T) {
	got := Derive(StatusInput{Terraform: tfObj(cond{"Ready", "Unknown", "Progressing", "Initializing", 18 * time.Hour}), Now: statusNow})
	require.Equal(t, "no progress for 18h0m0s: Initializing", got.Message)
}

// reporter returns a reconciler whose clock the test controls.
func reporter(m *memClient, now *time.Time) *Reconciler {
	return &Reconciler{Client: m, Config: testRenderConfig, Now: func() time.Time { return *now }}
}

func (m *memClient) setStatus(gvrName string, kind, name string, conds ...cond) {
	obj := objWith(kind, conds...)
	gvr := gvrTerraform
	if gvrName == "helmreleases" {
		gvr = gvrHelmRelease
	}
	obj.Object["metadata"] = map[string]interface{}{"name": name, "namespace": "flux-system", "uid": "uid-" + name}
	m.objs[key(gvr, "flux-system", name)] = obj
}

func statusData(m *memClient) map[string]interface{} {
	cm := m.objs[key(gvrConfigMap, "flux-system", "status-poc3")]
	if cm == nil {
		return nil
	}
	d, _ := cm.Object["data"].(map[string]interface{})
	return d
}

func events(m *memClient) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for k, o := range m.objs {
		if len(k) > 6 && k[:6] == "events" {
			out = append(out, o)
		}
	}
	return out
}

func TestReportStatusFollowsTheClusterThroughItsPhases(t *testing.T) {
	now := statusNow
	m := newMemClient()
	m.withToken("token-1")
	r := reporter(m, &now)
	spec := testSpec(t, nil)

	// the first loop creates the resources, the Terraform object has no status yet
	res, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, PhasePending, res.Status.Phase)
	require.True(t, res.StatusChanged)
	require.Equal(t, "Pending", statusData(m)[StatusKeyPhase])
	require.Len(t, events(m), 1)

	// nothing changes: nothing is written and no new Event is made
	now = now.Add(15 * time.Second)
	res, err = r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.False(t, res.StatusChanged)
	require.Len(t, events(m), 1)
	require.Equal(t, statusNow.Format(time.RFC3339), statusData(m)[StatusKeySince])

	// tofu-controller plans, then applies, then writes the outputs
	now = now.Add(time.Minute)
	m.setStatus("terraforms", "Terraform", "cluster-poc3", cond{"Ready", "Unknown", "Progressing", "Reconciliation in progress", 0})
	res, err = r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, PhasePlanning, res.Status.Phase)
	require.True(t, res.StatusChanged)

	now = now.Add(time.Minute)
	m.setStatus("terraforms", "Terraform", "cluster-poc3", smokeTest...)
	m.withOutputs()
	res, err = r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, StageDelivering, res.Stage, "the delivery objects are applied in the same loop")
	require.Equal(t, PhaseDelivering, res.Status.Phase, "...and the new HelmRelease has no status yet")

	now = now.Add(time.Minute)
	m.setStatus("helmreleases", "HelmRelease", "example-chart-poc3", cond{"Ready", "True", "InstallSucceeded", "Helm install succeeded", 0})
	res, err = r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, PhaseReady, res.Status.Phase)
	require.Equal(t, "Ready", statusData(m)[StatusKeyPhase])
	require.Equal(t, "InstallSucceeded", statusData(m)[StatusKeyReason])
	require.Equal(t, now.Format(time.RFC3339), statusData(m)[StatusKeySince])

	// one Event per change: Pending, Planning, Delivering, Ready
	require.Len(t, events(m), 4)
	for _, e := range events(m) {
		inv := e.Object["involvedObject"].(map[string]interface{})
		require.Equal(t, "Terraform", inv["kind"])
		require.Equal(t, "cluster-poc3", inv["name"])
		require.Equal(t, "Normal", e.Object["type"])
	}
}

func TestReportStatusFailureAndRecovery(t *testing.T) {
	now := statusNow
	m := newMemClient()
	m.withToken("token-1")
	r := reporter(m, &now)
	spec := testSpec(t, nil)
	_, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)

	m.setStatus("terraforms", "Terraform", "cluster-poc3",
		cond{"Ready", "Unknown", "Progressing", "", 0},
		cond{"Plan", "False", "TFExecPlanFailed", "Error: invalid value for variable", 0})
	res, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, "Failed(TFExecPlanFailed)", res.Status.String())
	require.Equal(t, "TFExecPlanFailed", statusData(m)[StatusKeyReason])
	require.Contains(t, statusData(m)[StatusKeyMessage], "invalid value for variable")

	var warning *unstructured.Unstructured
	for _, e := range events(m) {
		if e.Object["type"] == "Warning" {
			warning = e
		}
	}
	require.NotNil(t, warning)
	require.Equal(t, "TFExecPlanFailed", warning.Object["reason"])

	// the variable is fixed
	now = now.Add(time.Minute)
	m.setStatus("terraforms", "Terraform", "cluster-poc3", cond{"Ready", "Unknown", "Progressing", "", 0})
	res, err = r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, PhasePlanning, res.Status.Phase)
	require.Equal(t, "Planning", statusData(m)[StatusKeyPhase])
}

func TestReportStatusManualApproval(t *testing.T) {
	now := statusNow
	m := newMemClient()
	m.withToken("token-1")
	r := reporter(m, &now)
	spec := testSpec(t, map[string]string{ConfigItemProvisionApprove: "manual"})
	m.setStatus("terraforms", "Terraform", "cluster-poc3",
		cond{"Ready", "Unknown", "Progressing", "Plan generated", 0},
		cond{"Plan", "True", "TerraformPlannedWithChanges", "Plan generated", 0})
	res, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, PhaseAwaitingApproval, res.Status.Phase)
}

func TestReportStatusDryRunWritesNothing(t *testing.T) {
	now := statusNow
	m := newMemClient()
	m.withToken("token-1")
	r := reporter(m, &now)
	r.DryRun = true

	res, err := r.Reconcile(context.Background(), testSpec(t, nil))
	require.NoError(t, err)
	require.True(t, res.StatusChanged)
	require.Empty(t, events(m))
	require.Nil(t, statusData(m))
	require.Len(t, m.objs, 1, "only the seeded token Secret exists")
}

func TestStatusIsWrittenWithItsOwnClient(t *testing.T) {
	now := statusNow
	m := newMemClient()
	status := newMemClient()
	m.withToken("token-1")
	r := reporter(m, &now)
	r.StatusClient = status

	_, err := r.Reconcile(context.Background(), testSpec(t, nil))
	require.NoError(t, err)
	require.True(t, status.has(gvrConfigMap, "flux-system", "status-poc3"))
	require.Len(t, events(status), 1)
	require.Empty(t, events(m))
}

func TestReportStatusWhenTheTokenIsMissing(t *testing.T) {
	now := statusNow
	m := newMemClient()
	r := reporter(m, &now)
	spec := testSpec(t, nil)
	m.setStatus("terraforms", "Terraform", "cluster-poc3", smokeTest...)
	m.withOutputs()

	// the infrastructure exists, but the delivery cannot be prepared
	res, err := r.Reconcile(context.Background(), spec)
	require.ErrorContains(t, err, "token Secret default/cluster-token does not exist")
	require.Equal(t, PhaseInfraReady, res.Status.Phase)
	require.Equal(t, "InfraReady", statusData(m)[StatusKeyPhase])
	require.False(t, m.has(gvrHelmRelease, "flux-system", "example-chart-poc3"))
}

func srcObj(conds ...cond) *unstructured.Unstructured { return objWith("OCIRepository", conds...) }

// What the live noop test showed on 25 Nov: tofu-controller reconciled one
// second after the creation and reported ArtifactFailed, before the source had
// fetched the artifact.
var artifactFailedAtCreation = []cond{
	{"Ready", "False", "ArtifactFailed", "Source is not ready, artifact not found", time.Second},
}

func TestDeriveArtifactFailedWhileTheSourceIsFetching(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source *unstructured.Unstructured
		phase  Phase
		reason string
	}{
		{"source does not exist", nil, PhasePending, "WaitingForSource"},
		{"source has no status yet", srcObj(), PhasePending, "WaitingForSource"},
		{"source is fetching", srcObj(cond{"Ready", "Unknown", "Progressing", "reconciliation in progress", 0}), PhasePending, "WaitingForSource"},
		{"source fails", srcObj(cond{"Ready", "False", "AuthenticationFailed", "denied: not authorized", time.Minute}), PhaseFailed, "AuthenticationFailed"},
		// the source is fine, so the failure is real
		{"source is ready", srcObj(cond{"Ready", "True", "Succeeded", "stored artifact", time.Minute}), PhaseFailed, "ArtifactFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Derive(StatusInput{Terraform: tfObj(artifactFailedAtCreation...), ProvisionSource: tc.source, Now: statusNow})
			require.Equal(t, tc.phase, got.Phase)
			require.Equal(t, tc.reason, got.Reason)
		})
	}

	failing := Derive(StatusInput{Terraform: tfObj(artifactFailedAtCreation...),
		ProvisionSource: srcObj(cond{"Ready", "False", "AuthenticationFailed", "denied: not authorized", time.Minute}), Now: statusNow})
	require.Contains(t, failing.Message, "module source: denied: not authorized")
}

// The noop module has no cluster_id output: the apply succeeded and the
// outputs could not be written (live, 25 Nov).
func TestDeriveOutputsWritingFailed(t *testing.T) {
	got := Derive(StatusInput{
		Approve: "auto",
		Terraform: tfObj(
			cond{"Ready", "False", "OutputsWritingFailed", "output not found: cluster_id", 40 * time.Second},
			cond{"Apply", "True", "TerraformAppliedSucceed", "Applied successfully", time.Minute},
			cond{"Output", "True", "TerraformOutputsAvailable", "Outputs available", time.Minute},
			cond{"Plan", "True", "TerraformPlannedWithChanges", "Plan generated", time.Minute},
		),
		ProvisionSource: srcObj(cond{"Ready", "True", "Succeeded", "stored artifact", time.Hour}),
		Now:             statusNow,
	})
	require.Equal(t, "Failed(OutputsWritingFailed)", got.String())
	require.Equal(t, "output not found: cluster_id", got.Message)
}

func TestReportStatusDoesNotFailWhileTheSourceFetches(t *testing.T) {
	now := statusNow
	m := newMemClient()
	m.withToken("token-1")
	r := reporter(m, &now)
	spec := testSpec(t, nil)

	_, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	// tofu-controller is faster than the source-controller
	m.setStatus("terraforms", "Terraform", "cluster-poc3", artifactFailedAtCreation...)
	m.objs[key(gvrOCIRepository, "flux-system", "provision-poc3")] = &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "source.toolkit.fluxcd.io/v1", "kind": "OCIRepository",
		"metadata": map[string]interface{}{"name": "provision-poc3", "namespace": "flux-system"},
	}}

	res, err := r.Reconcile(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, PhasePending, res.Status.Phase)
	require.Equal(t, "WaitingForSource", res.Status.Reason)
	for _, e := range events(m) {
		require.Equal(t, "Normal", e.Object["type"], "no Warning for a source that is still fetching")
	}
}

// The deployed reconciler had no permission to read Events (seen live on 25 Nov): the failure must
// not fail the run or cost the status ConfigMap, and the Event must not be lost for good.
func TestReportStatusSurvivesAnEventFailure(t *testing.T) {
	now := statusNow
	m := newMemClient()
	m.withToken("token-1")
	m.failOn = "Event"
	r := reporter(m, &now)

	res, err := r.Reconcile(context.Background(), testSpec(t, nil))
	require.NoError(t, err, "a failing Event is not a failure of the run")
	require.True(t, res.StatusChanged)
	require.Equal(t, "Pending", statusData(m)[StatusKeyPhase], "the ConfigMap is written")
	require.Empty(t, events(m))

	// once the permission exists, the next change is reported again
	m.failOn = ""
	now = now.Add(time.Minute)
	m.setStatus("terraforms", "Terraform", "cluster-poc3", cond{"Ready", "Unknown", "Progressing", "", 0})
	_, err = r.Reconcile(context.Background(), testSpec(t, nil))
	require.NoError(t, err)
	require.Len(t, events(m), 1)
}
