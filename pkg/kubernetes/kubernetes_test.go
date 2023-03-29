package kubernetes

import (
	"context"
	"fmt"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	fakeAPIGroup   = "group"
	fakeAPIVersion = "version"
)

func newObject(kind, namespace, name string, labels map[string]string, references []metav1.OwnerReference) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(fakeAPIGroup + "/" + fakeAPIVersion)
	u.SetKind(kind)
	u.SetNamespace(namespace)
	u.SetName(name)
	u.SetLabels(labels)
	u.SetOwnerReferences(references)
	return u
}

type objectList []runtime.Object

func (ol *objectList) add(kind, namespace, name string, labels map[string]string, controller *unstructured.Unstructured) *unstructured.Unstructured {
	var refs []metav1.OwnerReference
	if controller != nil {
		refs = []metav1.OwnerReference{*metav1.NewControllerRef(controller, controller.GroupVersionKind())}
	}
	o := newObject(kind, namespace, name, labels, refs)
	*ol = append(*ol, o)
	return o
}

type clientError struct {
	matches func(k8stesting.Action) bool
	err     error
}

func listAction(resource string) func(k8stesting.Action) bool {
	return func(action k8stesting.Action) bool {
		list, ok := action.(k8stesting.ListAction)
		return ok && list.GetResource().Resource == resource
	}
}

func deleteAction(resource, name string) func(k8stesting.Action) bool {
	return func(action k8stesting.Action) bool {
		deleteAction, ok := action.(k8stesting.DeleteAction)
		return ok && deleteAction.GetResource().Resource == resource && deleteAction.GetName() == name
	}
}

type fakeRESTMapper struct {
	kindToResource map[string]string
}

func (f *fakeRESTMapper) KindFor(_ schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, nil
}

func (f *fakeRESTMapper) KindsFor(_ schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, nil
}

func (f *fakeRESTMapper) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{
		Group:    fakeAPIGroup,
		Version:  fakeAPIVersion,
		Resource: f.kindToResource[input.Resource],
	}, nil
}

func (f *fakeRESTMapper) ResourcesFor(_ schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, nil
}

func (f *fakeRESTMapper) RESTMapping(_ schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	return nil, nil
}

func (f *fakeRESTMapper) RESTMappings(_ schema.GroupKind, _ ...string) ([]*meta.RESTMapping, error) {
	return nil, nil
}

func (f *fakeRESTMapper) ResourceSingularizer(_ string) (singular string, err error) {
	return "", nil
}

func TestPerformDeletion(t *testing.T) {
	kindToResource := map[string]string{
		"Namespace":  "namespaces",
		"Deployment": "deployments",
		"ReplicaSet": "replicasets",
	}
	gvrToListKind := map[schema.GroupVersionResource]string{}
	for kind, resource := range kindToResource {
		gvrToListKind[schema.GroupVersionResource{Group: fakeAPIGroup, Version: fakeAPIVersion, Resource: resource}] = kind + "List"
	}

	var objects objectList

	objects.add("Namespace", "", "ns-foo", nil, nil)
	objects.add("Deployment", "default", "foo", nil, nil)
	objects.add("Deployment", "ns-foo", "foo", nil, nil)

	fooApp := objects.add("Deployment", "ns-foo", "foo-app", map[string]string{"app": "foo"}, nil)
	fooAppCom := objects.add("Deployment", "ns-foo", "foo-app-com", map[string]string{"app": "foo", "com": "foo"}, nil)
	fooAppComEnv := objects.add("Deployment", "ns-foo", "foo-app-com-env", map[string]string{"app": "foo", "com": "foo", "env": "foo"}, nil)

	objects.add("ReplicaSet", "ns-foo", "foo-app-0001", map[string]string{"app": "foo"}, nil)
	objects.add("ReplicaSet", "ns-foo", "foo-app-0002", map[string]string{"app": "foo"}, fooApp)
	objects.add("ReplicaSet", "ns-foo", "foo-app-com-0001", map[string]string{"app": "foo", "com": "foo"}, nil)
	objects.add("ReplicaSet", "ns-foo", "foo-app-com-0002", map[string]string{"app": "foo", "com": "foo"}, fooAppCom)
	objects.add("ReplicaSet", "ns-foo", "foo-app-com-env-0001", map[string]string{"app": "foo", "com": "foo", "env": "foo"}, nil)
	objects.add("ReplicaSet", "ns-foo", "foo-app-com-env-0002", map[string]string{"app": "foo", "com": "foo", "env": "foo"}, fooAppComEnv)

	yes, no := true, false
	for _, tc := range []struct {
		name          string
		deletion      *Resource
		clientErrors  func(state map[string]interface{}) []clientError
		expectError   string
		expectDeleted []string
	}{
		//
		// TODO: update client-go version and add metav1.DeleteOptions tests
		//
		// Usecases
		{
			name: "delete by name in namespace",
			deletion: &Resource{
				Name:      "foo",
				Namespace: "default",
				Kind:      "Deployment",
			},
			expectDeleted: []string{
				"/namespaces/default/deployments/foo",
			},
		},
		{
			name: "delete by name non-namespaced",
			deletion: &Resource{
				Name: "ns-foo",
				Kind: "Namespace",
			},
			expectDeleted: []string{
				"/namespaces/ns-foo",
			},
		},
		{
			name: "delete by single label",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Labels:    map[string]string{"app": "foo"},
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/deployments/foo-app",
				"/namespaces/ns-foo/deployments/foo-app-com",
				"/namespaces/ns-foo/deployments/foo-app-com-env",
			},
		},
		{
			name: "delete by multiple labels",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Labels:    map[string]string{"app": "foo", "com": "foo"},
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/deployments/foo-app-com",
				"/namespaces/ns-foo/deployments/foo-app-com-env",
			},
		},
		{
			name: "no match by label",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Labels:    map[string]string{"app": "nomatch"},
			},
			expectDeleted: []string{},
		},
		{
			name: "delete by selector (single label)",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Selector:  "app=foo",
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/deployments/foo-app",
				"/namespaces/ns-foo/deployments/foo-app-com",
				"/namespaces/ns-foo/deployments/foo-app-com-env",
			},
		},
		{
			name: "delete by selector (multiple labels)",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Selector:  "app=foo,com=foo",
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/deployments/foo-app-com",
				"/namespaces/ns-foo/deployments/foo-app-com-env",
			},
		},
		{
			name: "delete by selector (label not equal)",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Selector:  "app=foo, env != foo",
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/deployments/foo-app",
				"/namespaces/ns-foo/deployments/foo-app-com",
			},
		},
		{
			name: "delete by selector (label not in)",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Selector:  "app=foo, env notin (foo, bar)",
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/deployments/foo-app",
				"/namespaces/ns-foo/deployments/foo-app-com",
			},
		},
		{
			name: "delete by selector (no match)",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Selector:  "app=nomatch",
			},
			expectDeleted: []string{},
		},
		{
			name: "delete replicasets without owner",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "ReplicaSet",
				Labels:    map[string]string{"app": "foo", "com": "foo"},
				HasOwner:  &no,
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/replicasets/foo-app-com-0001",
				"/namespaces/ns-foo/replicasets/foo-app-com-env-0001",
			},
		},
		{
			name: "delete replicasets with owner and labels",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "ReplicaSet",
				Labels:    map[string]string{"app": "foo", "com": "foo"},
				HasOwner:  &yes,
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/replicasets/foo-app-com-0002",
				"/namespaces/ns-foo/replicasets/foo-app-com-env-0002",
			},
		},
		{
			name: "delete replicasets with owner and selector",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "ReplicaSet",
				Selector:  "app=foo,com=foo",
				HasOwner:  &yes,
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/replicasets/foo-app-com-0002",
				"/namespaces/ns-foo/replicasets/foo-app-com-env-0002",
			},
		},
		{
			name: "delete replicasets regardless owner",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "ReplicaSet",
				Labels:    map[string]string{"app": "foo", "com": "foo"},
				HasOwner:  nil,
			},
			expectDeleted: []string{
				"/namespaces/ns-foo/replicasets/foo-app-com-0001",
				"/namespaces/ns-foo/replicasets/foo-app-com-env-0001",
				"/namespaces/ns-foo/replicasets/foo-app-com-0002",
				"/namespaces/ns-foo/replicasets/foo-app-com-env-0002",
			},
		},
		// Errors
		{
			name: "all of name, selector and labels are specified",
			deletion: &Resource{
				Name:      "foo",
				Labels:    map[string]string{"foo": "bar"},
				Selector:  "foo=bar",
				Namespace: "default",
				Kind:      "Deployment",
			},
			expectError: "only one of 'name', 'selector' or 'labels' must be specified to identify a resource",
		},
		{
			name: "both name and labels are specified",
			deletion: &Resource{
				Name:      "foo",
				Labels:    map[string]string{"foo": "bar"},
				Namespace: "default",
				Kind:      "Deployment",
			},
			expectError: "only one of 'name', 'selector' or 'labels' must be specified to identify a resource",
		},
		{
			name: "both name and selector are specified",
			deletion: &Resource{
				Name:      "foo",
				Selector:  "foo=bar",
				Namespace: "default",
				Kind:      "Deployment",
			},
			expectError: "only one of 'name', 'selector' or 'labels' must be specified to identify a resource",
		},
		{
			name: "both selector and labels are specified",
			deletion: &Resource{
				Labels:    map[string]string{"foo": "bar"},
				Selector:  "foo=bar",
				Namespace: "default",
				Kind:      "Deployment",
			},
			expectError: "only one of 'name', 'selector' or 'labels' must be specified to identify a resource",
		},
		{
			name: "none of name, selector or labels are specified",
			deletion: &Resource{
				Namespace: "default",
				Kind:      "Deployment",
			},
			expectError: "either 'name', 'selector' or 'labels' must be specified to identify a resource",
		},
		{
			name: "has_owner without selector or labels",
			deletion: &Resource{
				Name:      "foo",
				Namespace: "default",
				Kind:      "Deployment",
				HasOwner:  &yes,
			},
			expectError: "'has_owner' requires 'selector' or 'labels' to be specified",
		},
		{
			name: "list error",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Labels:    map[string]string{"app": "foo"},
			},
			clientErrors: func(map[string]interface{}) []clientError {
				return []clientError{
					{
						matches: listAction("deployments"),
						err:     fmt.Errorf("listing deployments failed"),
					},
				}
			},
			expectError: "listing deployments failed",
		},
		{
			name: "delete error",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Labels:    map[string]string{"app": "foo"},
			},
			clientErrors: func(map[string]interface{}) []clientError {
				return []clientError{
					{
						matches: deleteAction("deployments", "foo-app"),
						err:     fmt.Errorf("foo-app delete failed"),
					},
				}
			},
			expectError: "unable to delete: foo-app delete failed",
		},
		{
			name: "skips not found error",
			deletion: &Resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Labels:    map[string]string{"app": "foo"},
			},
			clientErrors: func(map[string]interface{}) []clientError {
				return []clientError{
					{
						matches: deleteAction("deployments", "foo-app"),
						err:     apierrors.NewNotFound(schema.GroupResource{}, "foo-app"),
					},
				}
			},
			expectDeleted: []string{
				// skips "/namespaces/ns-foo/deployments/foo-app" due to not found error
				"/namespaces/ns-foo/deployments/foo-app-com",
				"/namespaces/ns-foo/deployments/foo-app-com-env",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.StandardLogger().WithFields(map[string]interface{}{"testcase": tc.name})

			state := map[string]interface{}{}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), gvrToListKind, objects...)
			if tc.clientErrors != nil {
				client.PrependReactor("*", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
					for _, ce := range tc.clientErrors(state) {
						if ce.matches(action) {
							return true, nil, ce.err
						}
					}
					return false, nil, nil
				})
			}

			c := ClientsCollection{
				TypedClient:   nil,
				DynamicClient: client,
				Mapper:        &fakeRESTMapper{kindToResource},
			}
			err := c.DeleteResource(context.TODO(), logger, tc.deletion)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}

			var deleted []string
			for _, action := range client.Actions() {
				ignore := false
				if tc.clientErrors != nil {
					for _, ce := range tc.clientErrors(state) {
						if ce.matches(action) {
							ignore = true
							break
						}
					}
				}

				if action, ok := action.(k8stesting.DeleteAction); ok && !ignore && action.GetVerb() == "delete" {
					fqn := "/"
					if action.GetNamespace() != "" {
						fqn += "namespaces/" + action.GetNamespace() + "/"
					}
					fqn += action.GetResource().Resource + "/" + action.GetName()

					deleted = append(deleted, fqn)
				}
			}
			assert.ElementsMatch(t, tc.expectDeleted, deleted)
		})
	}
}

func TestParseTaint(t *testing.T) {
	testCases := []struct {
		title          string
		input          string
		expectedOutput *corev1.Taint
		expectError    bool
	}{
		{title: "taint with key, value, and effect", input: "dedicated=test:NoSchedule", expectedOutput: &corev1.Taint{Key: "dedicated", Value: "test", Effect: "NoSchedule"}, expectError: false},
		{title: "taint with key and effect", input: "example:NoSchedule", expectedOutput: &corev1.Taint{Key: "example", Value: "", Effect: "NoSchedule"}, expectError: false},
		{title: "taint with only key", input: "dedicated", expectedOutput: &corev1.Taint{Key: "dedicated", Value: "", Effect: ""}, expectError: false},
		{title: "taint with key and value", input: "dedicated=test", expectedOutput: nil, expectError: true},
		{title: "taint with invalid effect", input: "dedicated:foo=NoSchedule", expectedOutput: nil, expectError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.title, func(t *testing.T) {
			taint, err := ParseTaint(tc.input)
			if tc.expectError != (err != nil) {
				assert.Fail(t, "testcase error state does not match expectations")
			}
			assert.Equal(t, tc.expectedOutput, taint)
		})
	}
}
