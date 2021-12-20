package provisioner

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		},
	}

	exampleCluster := &api.Cluster{
		Alias: "foobar",
	}
	orphan := metav1.DeletionPropagation("Orphan")
	gps10 := int64(10)
	expected := &deletions{
		PreApply: []*resource{
			{Name: "secretary-pre", Namespace: "kube-system", Kind: "deployment"},
			{Name: "mate-pre", Namespace: "", Kind: "priorityclass"},
			{Name: "options-pre", Namespace: "kube-system", Kind: "deployment", PropagationPolicy: &orphan},
			{Name: "foobar-pre", Namespace: "templated", Kind: "deployment"},
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
