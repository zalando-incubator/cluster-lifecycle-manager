package reconciler

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// Action is what applying an object did, or would do in a dry run.
type Action string

const (
	ActionCreated   Action = "created"
	ActionUpdated   Action = "updated"
	ActionUnchanged Action = "unchanged"
)

// ApplyResult is the result of applying one object.
type ApplyResult struct {
	Action Action
	// Diff is only set for dry runs of an update or create: a unified diff
	// between the live object and the object after applying. Secret values
	// are redacted.
	Diff string
}

// Client is the part of the Kubernetes API the reconciler needs. It exists so
// tests can replace the API server.
type Client interface {
	// Get returns an object or nil if it does not exist.
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)
	// Apply applies the object with server-side apply, forcing conflicts with
	// other field managers on the fields it sets. In a dry run nothing is
	// changed.
	Apply(ctx context.Context, obj *unstructured.Unstructured, dryRun bool) (ApplyResult, error)
}

var (
	gvrSecret         = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	gvrConfigMap      = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	gvrEvent          = schema.GroupVersionResource{Version: "v1", Resource: "events"}
	gvrOCIRepository  = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "ocirepositories"}
	gvrHelmRepository = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "helmrepositories"}
	gvrHelmRelease    = schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
	gvrTerraform      = schema.GroupVersionResource{Group: "infra.contrib.fluxcd.io", Version: "v1alpha2", Resource: "terraforms"}
)

// gvrFor maps the kinds the reconciler creates to their resources.
func gvrFor(obj *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	switch obj.GetAPIVersion() + "/" + obj.GetKind() {
	case "v1/Secret":
		return gvrSecret, nil
	case "v1/ConfigMap":
		return gvrConfigMap, nil
	case "v1/Event":
		return gvrEvent, nil
	case "source.toolkit.fluxcd.io/v1/OCIRepository":
		return gvrOCIRepository, nil
	case "source.toolkit.fluxcd.io/v1/HelmRepository":
		return gvrHelmRepository, nil
	case "helm.toolkit.fluxcd.io/v2/HelmRelease":
		return gvrHelmRelease, nil
	case "infra.contrib.fluxcd.io/v1alpha2/Terraform":
		return gvrTerraform, nil
	}
	return schema.GroupVersionResource{}, fmt.Errorf("unsupported kind %s %s", obj.GetAPIVersion(), obj.GetKind())
}

type dynamicClient struct {
	client       dynamic.Interface
	fieldManager string
}

// NewClient returns a Client on top of a dynamic Kubernetes client.
func NewClient(client dynamic.Interface, fieldManager string) Client {
	return &dynamicClient{client: client, fieldManager: fieldManager}
}

func (c *dynamicClient) Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	obj, err := c.client.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	return obj, err
}

func (c *dynamicClient) Apply(ctx context.Context, obj *unstructured.Unstructured, dryRun bool) (ApplyResult, error) {
	gvr, err := gvrFor(obj)
	if err != nil {
		return ApplyResult{}, err
	}
	live, err := c.Get(ctx, gvr, obj.GetNamespace(), obj.GetName())
	if err != nil {
		return ApplyResult{}, fmt.Errorf("get %s %s: %w", obj.GetKind(), obj.GetName(), err)
	}

	opts := metav1.ApplyOptions{FieldManager: c.fieldManager, Force: true}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	}
	applied, err := c.client.Resource(gvr).Namespace(obj.GetNamespace()).Apply(ctx, obj.GetName(), obj, opts)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply %s %s: %w", obj.GetKind(), obj.GetName(), err)
	}
	return compare(live, applied, dryRun), nil
}

// compare decides what happened by comparing the object before and after
// applying, ignoring everything the server maintains.
func compare(live, applied *unstructured.Unstructured, withDiff bool) ApplyResult {
	if live == nil {
		res := ApplyResult{Action: ActionCreated}
		if withDiff {
			res.Diff = diff(nil, applied)
		}
		return res
	}
	if reflect.DeepEqual(normalize(live), normalize(applied)) {
		return ApplyResult{Action: ActionUnchanged}
	}
	res := ApplyResult{Action: ActionUpdated}
	if withDiff {
		res.Diff = diff(live, applied)
	}
	return res
}

// normalize returns a copy without the fields maintained by the server.
func normalize(obj *unstructured.Unstructured) map[string]interface{} {
	out := obj.DeepCopy().Object
	delete(out, "status")
	if md, ok := out["metadata"].(map[string]interface{}); ok {
		for _, key := range []string{"managedFields", "resourceVersion", "generation", "uid", "creationTimestamp", "selfLink"} {
			delete(md, key)
		}
	}
	return out
}

// diff renders a unified diff of two objects. Either may be nil. Secret
// values are never shown.
func diff(before, after *unstructured.Unstructured) string {
	render := func(obj, other *unstructured.Unstructured, differs string) []string {
		if obj == nil {
			return nil
		}
		m := normalize(obj)
		if obj.GetKind() == "Secret" {
			redact(m, other, differs)
		}
		b, err := yaml.Marshal(m)
		if err != nil {
			return []string{fmt.Sprintf("<cannot render: %v>", err)}
		}
		return strings.SplitAfter(string(b), "\n")
	}
	d, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        render(before, after, "<redacted: old value>"),
		B:        render(after, before, "<redacted: new value>"),
		FromFile: "live",
		ToFile:   "applied",
		Context:  3,
	})
	return d
}

// redact replaces the values of a Secret. A value that differs from the one in
// the other object gets the marker differs, which is different for the two
// sides, so a rotation is still visible in the diff.
func redact(m map[string]interface{}, other *unstructured.Unstructured, differs string) {
	for _, field := range []string{"data", "stringData"} {
		values, ok := m[field].(map[string]interface{})
		if !ok {
			continue
		}
		var otherValues map[string]interface{}
		if other != nil {
			otherValues, _ = other.Object[field].(map[string]interface{})
		}
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch ov, present := otherValues[k]; {
			case !present && other != nil:
				values[k] = "<redacted: only here>"
			case present && !reflect.DeepEqual(ov, values[k]):
				values[k] = differs
			default:
				values[k] = "<redacted>"
			}
		}
	}
}
