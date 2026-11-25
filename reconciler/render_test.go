package reconciler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestOptionalRenderSettings(t *testing.T) {
	spec := testSpec(t, nil)

	// without the settings nothing is rendered for them
	c := testRenderConfig
	c.RunnerPodApplication, c.RunnerPodComponent, c.OCIProvider = "", "", ""
	src, _, _ := unstructured.NestedMap(c.ProvisionSource(spec).Object, "spec")
	require.NotContains(t, src, "provider")
	labels, _, _ := unstructured.NestedMap(c.Terraform(spec).Object, "spec", "runnerPodTemplate", "metadata", "labels")
	require.Empty(t, labels)

	// with them, the module source authenticates with the provider and the
	// runner pods carry the labels kube-janitor needs
	c.OCIProvider, c.RunnerPodApplication, c.RunnerPodComponent = "aws", "kubernetes", "reconciler"
	src, _, _ = unstructured.NestedMap(c.ProvisionSource(spec).Object, "spec")
	require.Equal(t, "aws", src["provider"])
	labels, _, _ = unstructured.NestedMap(c.Terraform(spec).Object, "spec", "runnerPodTemplate", "metadata", "labels")
	require.Equal(t, map[string]interface{}{"application": "kubernetes", "component": "reconciler"}, labels)
}
