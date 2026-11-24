package reconciler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

// The dynamic fake client cannot do server-side apply, so a reactor emulates
// it: create if missing, else replace. This checks how the real client uses
// the API: kinds, names, namespaces and what counts as a change.
func TestDynamicClientApply(t *testing.T) {
	scheme := runtime.NewScheme()
	fc := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{gvrTerraform: "TerraformList"})
	fc.PrependReactor("patch", "terraforms", func(action clienttesting.Action) (bool, runtime.Object, error) {
		pa := action.(clienttesting.PatchAction)
		require.Equal(t, "application/apply-patch+yaml", string(pa.GetPatchType()))
		obj := &unstructured.Unstructured{}
		require.NoError(t, json.Unmarshal(pa.GetPatch(), &obj.Object))
		existing, err := fc.Tracker().Get(gvrTerraform, pa.GetNamespace(), pa.GetName())
		if apierrors.IsNotFound(err) {
			return true, obj, fc.Tracker().Create(gvrTerraform, obj, pa.GetNamespace())
		}
		require.NoError(t, err)
		_ = existing
		return true, obj, fc.Tracker().Update(gvrTerraform, obj, pa.GetNamespace())
	})

	c := NewClient(fc, "reconciler")
	spec := testSpec(t, nil)
	tf := testRenderConfig.Terraform(spec)

	got, err := c.Get(context.Background(), gvrTerraform, "flux-system", "cluster-poc3")
	require.NoError(t, err)
	require.Nil(t, got)

	res, err := c.Apply(context.Background(), tf, false)
	require.NoError(t, err)
	require.Equal(t, ActionCreated, res.Action)

	res, err = c.Apply(context.Background(), tf, false)
	require.NoError(t, err)
	require.Equal(t, ActionUnchanged, res.Action)

	changed := testRenderConfig.Terraform(testSpec(t, map[string]string{ConfigItemProvisionVars: "node_count: 5"}))
	res, err = c.Apply(context.Background(), changed, false)
	require.NoError(t, err)
	require.Equal(t, ActionUpdated, res.Action)

	_, err = c.Apply(context.Background(), &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "v1", "kind": "Pod"}}, false)
	require.ErrorContains(t, err, "unsupported kind")
}
