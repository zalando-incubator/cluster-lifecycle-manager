package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

var (
	deletionsContent = `
pre_apply:
- name: secretary-pre
  namespace: kube-system
  kind: deployment
- name: mate-pre
  kind: priorityclass
- name: options-pre
  namespace: kube-system
  kind: deployment
  propagation_policy: Orphan
post_apply:
- name: secretary-post
  namespace: kube-system
  kind: deployment
- name: mate-post
  kind: priorityclass
- name: options-post
  namespace: kube-system
  kind: deployment
  grace_period_seconds: 10
`

	deletionsContent2 = `
pre_apply:
- name: {{.Alias}}-pre
  namespace: templated
  kind: deployment
post_apply:
- name: {{.Alias}}-post
  namespace: templated
  kind: deployment
`

	deletionsContent3 = `
pre_apply:
- name: has-no-owner-pre
  namespace: kube-system
  kind: ReplicaSet
  labels:
    foo: bar
    baz: qux
  has_owner: false
- name: require-owner-pre
  namespace: kube-system
  kind: ReplicaSet
  labels:
    foo: bar
    baz: qux
  has_owner: true
`
)

func TestGetInfrastructureID(t *testing.T) {
	expected := "12345678910"
	awsAccountID := getAWSAccountID(fmt.Sprintf("aws:%s", expected))
	if awsAccountID != expected {
		t.Errorf("expected: %s, got: %s", expected, awsAccountID)
	}
}

func TestHasTag(t *testing.T) {
	for _, tc := range []struct {
		msg      string
		tags     []*ec2.Tag
		tag      *ec2.Tag
		expected bool
	}{
		{
			msg: "test finding tag in list successfully",
			tags: []*ec2.Tag{
				{
					Key:   aws.String("key"),
					Value: aws.String("val"),
				},
			},
			tag: &ec2.Tag{
				Key:   aws.String("key"),
				Value: aws.String("val"),
			},
			expected: true,
		},
		{
			msg: "test both key and value must match",
			tags: []*ec2.Tag{
				{
					Key:   aws.String("key"),
					Value: aws.String("val"),
				},
			},
			tag: &ec2.Tag{
				Key:   aws.String("key"),
				Value: aws.String(""),
			},
			expected: false,
		},
		{
			msg:  "test finding no tag in empty list",
			tags: []*ec2.Tag{},
			tag: &ec2.Tag{
				Key:   aws.String("key"),
				Value: aws.String(""),
			},
			expected: false,
		},
	} {
		t.Run(tc.msg, func(t *testing.T) {
			assert.Equal(t, hasTag(tc.tags, tc.tag), tc.expected)
		})
	}
}

func TestFilterSubnets(tt *testing.T) {
	tt.Run("configured IDs", func(tt *testing.T) {
		for _, tc := range []struct {
			msg             string
			subnets         []*ec2.Subnet
			subnetIds       []string
			expectedSubnets []*ec2.Subnet
		}{

			{
				msg: "test filtering out a single subnet",
				subnets: []*ec2.Subnet{
					{
						SubnetId: aws.String("id-1"),
					},
					{
						SubnetId: aws.String("id-2"),
					},
				},
				subnetIds: []string{"id-1"},
				expectedSubnets: []*ec2.Subnet{
					{
						SubnetId: aws.String("id-1"),
					},
				},
			},
			{
				msg: "test filtering invalid subnets",
				subnets: []*ec2.Subnet{
					{
						SubnetId: aws.String("id-1"),
					},
				},
				subnetIds:       []string{"id-2"},
				expectedSubnets: nil,
			},
		} {
			tt.Run(tc.msg, func(t *testing.T) {
				subnets := filterSubnets(tc.subnets, subnetIDIncluded(tc.subnetIds))
				require.EqualValues(t, tc.expectedSubnets, subnets)
			})
		}
	})

	tt.Run("ignore custom", func(tt *testing.T) {
		for _, test := range []struct {
			msg                      string
			subnets, expectedSubnets []*ec2.Subnet
		}{{
			msg: "has no custom",
			subnets: []*ec2.Subnet{{
				SubnetId: aws.String("id-1"),
			}, {
				SubnetId: aws.String("id-2"),
			}, {
				SubnetId: aws.String("id-3"),
			}},
			expectedSubnets: []*ec2.Subnet{{
				SubnetId: aws.String("id-1"),
			}, {
				SubnetId: aws.String("id-2"),
			}, {
				SubnetId: aws.String("id-3"),
			}},
		}, {
			msg: "has custom",
			subnets: []*ec2.Subnet{{
				SubnetId: aws.String("id-1"),
			}, {
				SubnetId: aws.String("id-2"),
				Tags: []*ec2.Tag{{
					Key:   aws.String(customSubnetTag),
					Value: aws.String("foo"),
				}},
			}, {
				SubnetId: aws.String("id-3"),
			}},
			expectedSubnets: []*ec2.Subnet{{
				SubnetId: aws.String("id-1"),
			}, {
				SubnetId: aws.String("id-3"),
			}},
		}} {
			tt.Run(test.msg, func(t *testing.T) {
				subnets := filterSubnets(test.subnets, subnetNot(isCustomSubnet))
				require.EqualValues(t, test.expectedSubnets, subnets)
			})
		}
	})
}

func TestPropagateConfigItemsToNodePool(tt *testing.T) {
	for _, tc := range []struct {
		cluster  map[string]string
		nodePool map[string]string
		expected map[string]string
	}{
		{
			cluster:  nil,
			nodePool: nil,
			expected: map[string]string{},
		},
		{
			cluster:  map[string]string{"foo": "bar"},
			nodePool: nil,
			expected: map[string]string{"foo": "bar"},
		},
		{
			cluster:  nil,
			nodePool: map[string]string{"foo": "wambo"},
			expected: map[string]string{"foo": "wambo"},
		},
		{
			cluster:  map[string]string{"foo": "bar"},
			nodePool: map[string]string{"foo": "wambo"},
			expected: map[string]string{"foo": "wambo"},
		},
	} {
		cluster := &api.Cluster{
			ConfigItems: tc.cluster,
			NodePools:   []*api.NodePool{&api.NodePool{ConfigItems: tc.nodePool}},
		}

		p := clusterpyProvisioner{}
		p.propagateConfigItemsToNodePools(cluster)
		assert.Equal(tt, tc.expected, cluster.NodePools[0].ConfigItems)
	}
}

func TestLabelsString(t *testing.T) {
	labels := labels(map[string]string{"key": "value", "foo": "bar"})
	expected := []string{"key=value,foo=bar", "foo=bar,key=value"}
	labelStr := labels.String()
	if labelStr != expected[0] && labelStr != expected[1] {
		t.Errorf("expected labels format: %+v, got %+v", expected, labels)
	}
}

type mockConfig struct {
	deletions []channel.Manifest
}

func (c *mockConfig) StackManifest(manifestName string) (channel.Manifest, error) {
	return channel.Manifest{}, errors.New("unsupported: StackManifest")
}

func (c *mockConfig) EtcdManifest(manifestName string) (channel.Manifest, error) {
	return channel.Manifest{}, errors.New("unsupported: EtcdManifest")
}

func (c *mockConfig) NodePoolManifest(profileName string, manifestName string) (channel.Manifest, error) {
	return channel.Manifest{}, errors.New("unsupported: NodePoolManifest")
}

func (c *mockConfig) DefaultsManifests() ([]channel.Manifest, error) {
	return nil, errors.New("unsupported: DefaultsManifests")
}

func (c *mockConfig) DeletionsManifests() ([]channel.Manifest, error) {
	return c.deletions, nil
}

func (c *mockConfig) Components() ([]channel.Component, error) {
	return nil, errors.New("unsupported: Components")
}

func (c *mockConfig) Delete() error {
	return nil
}

func TestParseDeletions(t *testing.T) {
	cfg := &mockConfig{
		deletions: []channel.Manifest{
			{Path: "deletions.yaml", Contents: []byte(deletionsContent)},
			{Path: "deletions.yaml", Contents: []byte(deletionsContent2)},
			{Path: "deletions.yaml", Contents: []byte(deletionsContent3)},
		},
	}

	exampleCluster := &api.Cluster{
		Alias: "foobar",
	}
	orphan := metav1.DeletionPropagation("Orphan")
	gps10 := int64(10)
	yes, no := true, false
	expected := &deletions{
		PreApply: []*resource{
			{Name: "secretary-pre", Namespace: "kube-system", Kind: "deployment"},
			{Name: "mate-pre", Namespace: "", Kind: "priorityclass"},
			{Name: "options-pre", Namespace: "kube-system", Kind: "deployment", PropagationPolicy: &orphan},
			{Name: "foobar-pre", Namespace: "templated", Kind: "deployment"},
			{Name: "has-no-owner-pre", HasOwner: &no, Namespace: "kube-system", Kind: "ReplicaSet", Labels: map[string]string{"foo": "bar", "baz": "qux"}},
			{Name: "require-owner-pre", HasOwner: &yes, Namespace: "kube-system", Kind: "ReplicaSet", Labels: map[string]string{"foo": "bar", "baz": "qux"}},
		},
		PostApply: []*resource{
			{Name: "secretary-post", Namespace: "kube-system", Kind: "deployment"},
			{Name: "mate-post", Namespace: "", Kind: "priorityclass"},
			{Name: "options-post", Namespace: "kube-system", Kind: "deployment", GracePeriodSeconds: &gps10},
			{Name: "foobar-post", Namespace: "templated", Kind: "deployment"},
		},
	}

	deletions, err := parseDeletions(cfg, exampleCluster, nil, nil)
	require.NoError(t, err)
	require.EqualValues(t, expected, deletions)
}

func TestRemarshalYAML(t *testing.T) {
	for _, tc := range []struct {
		name     string
		source   string
		expected string
	}{
		{
			name: "basic",
			source: `
foo: bar
int: 1
num: 1.111
map:
  a: b
list:
  - 1
  - 2
nil: ~
`,
			expected: `
foo: bar
int: 1
list:
- 1
- 2
map:
  a: b
nil: null
num: 1.111
`,
		},
		{
			name: "references are inlined",
			source: `
value: &foo 123
ref1: *foo
ref2: *foo
`,
			expected: `
ref1: 123
ref2: 123
value: 123
`,
		},
		{
			name: "manifests with multiple documents are parsed correctly",
			source: `
first: 1
---
second: 2
---
`,
			expected: `
first: 1
---
second: 2
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remarshaled, err := remarshalYAML(tc.source)
			require.NoError(t, err)
			require.Equal(t, strings.TrimPrefix(tc.expected, "\n"), remarshaled)
		})
	}
}

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
		delete, ok := action.(k8stesting.DeleteAction)
		return ok && delete.GetResource().Resource == resource && delete.GetName() == name
	}
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
	i := []struct {
		name           string
		deletion       *resource
		clientErrors   func(state map[string]interface{}) []clientError
		clientReactors func(state map[string]interface{}) []k8stesting.SimpleReactor
		expectError    string
		expectDeleted  []string
	}{
		//
		// TODO: update client-go version and add metav1.DeleteOptions tests
		//
		// Usecases
		{
			name: "delete by name in namespace",
			deletion: &resource{
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
			deletion: &resource{
				Name: "ns-foo",
				Kind: "Namespace",
			},
			clientReactors: func(state map[string]interface{}) []k8stesting.SimpleReactor {
				return []k8stesting.SimpleReactor{
					{
						Verb:     "patch",
						Resource: "namespaces",
						Reaction: func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
							payload := map[string]interface{}{}
							err = json.Unmarshal(action.(k8stesting.PatchAction).GetPatch(), &payload)
							if err != nil {
								return false, nil, err
							}
							for k, v := range payload {
								state[k] = v
							}
							return true, nil, nil
						},
					},
				}
			},
			clientErrors: func(state map[string]interface{}) []clientError {
				if meta, ok := state["metadata"]; ok {
					if annotations, ok := meta.(map[string]interface{})["annotations"]; ok {
						if _, ok := annotations.(map[string]interface{})["zalando.org/delete-date"]; ok {
							return nil
						}
					}
				}
				return []clientError{
					{
						matches: deleteAction("namespaces", "ns-foo"),
						err:     fmt.Errorf("admission webhook \"namespace-admitter.teapot.zalan.do\" denied the request: annotation zalando.org/delete-date not set in manifest to allow resource deletion. See https://cloud.docs.zalando.net/howtos/delete-protection/"),
					},
				}
			},
			expectDeleted: []string{
				"/namespaces/ns-foo",
			},
		},
		{
			name: "delete by single label",
			deletion: &resource{
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
			deletion: &resource{
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
			deletion: &resource{
				Namespace: "ns-foo",
				Kind:      "Deployment",
				Labels:    map[string]string{"app": "nomatch"},
			},
			expectDeleted: []string{},
		},
		{
			name: "delete replicasets without owner",
			deletion: &resource{
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
			name: "delete replicasets with owner",
			deletion: &resource{
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
			name: "delete replicasets regardless owner",
			deletion: &resource{
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
			name: "both name or labels are specified",
			deletion: &resource{
				Name:      "foo",
				Labels:    map[string]string{"foo": "bar"},
				Namespace: "default",
				Kind:      "Deployment",
			},
			expectError: "only one of 'name' or 'labels' must be specified",
		},
		{
			name: "neither name or labels are specified",
			deletion: &resource{
				Namespace: "default",
				Kind:      "Deployment",
			},
			expectError: "either name or labels must be specified to identify a resource",
		},
		{
			name: "has_owner without labels",
			deletion: &resource{
				Name:      "foo",
				Namespace: "default",
				Kind:      "Deployment",
				HasOwner:  &yes,
			},
			expectError: "'has_owner' requires 'labels' to be specified",
		},
		{
			name: "list error",
			deletion: &resource{
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
			deletion: &resource{
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
			deletion: &resource{
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
	}
	for _, tc := range i {
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
			if tc.clientReactors != nil {
				for _, cr := range tc.clientReactors(state) {
					client.PrependReactor(cr.Verb, cr.Resource, cr.Reaction)
				}
			}

			gvr := schema.GroupVersionResource{Group: fakeAPIGroup, Version: fakeAPIVersion, Resource: kindToResource[tc.deletion.Kind]}

			err := performDeletion(context.TODO(), logger, client, gvr, tc.deletion)
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
