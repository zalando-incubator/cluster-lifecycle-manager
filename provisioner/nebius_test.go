package provisioner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
)

func newTestNebiusProvisioner() *NebiusProvisioner {
	p := NewNebiusProvisioner(nil, "", nil, false).(*NebiusProvisioner)
	p.setupAWSAdapter = func(_ context.Context, _ *log.Entry, _ *api.Cluster) (*awsAdapter, error) {
		return newAWSAdapterWithStubs(cftypes.StackStatusCreateComplete, ""), nil
	}
	return p
}

type nebiusCFStub struct {
	cloudformationAPI
	events        *[]string
	created       *cloudformation.CreateStackInput
	updated       *cloudformation.UpdateStackInput
	createErr     error
	updateErr     error
	describeErr   error
	describeCalls int
	describeErrAt int
	stackStatus   cftypes.StackStatus
	stackOutputs  []cftypes.Output
}

func (c *nebiusCFStub) CreateStack(_ context.Context, input *cloudformation.CreateStackInput, _ ...func(*cloudformation.Options)) (*cloudformation.CreateStackOutput, error) {
	*c.events = append(*c.events, "stack")
	c.created = input
	return &cloudformation.CreateStackOutput{}, c.createErr
}

func (c *nebiusCFStub) UpdateTerminationProtection(context.Context, *cloudformation.UpdateTerminationProtectionInput, ...func(*cloudformation.Options)) (*cloudformation.UpdateTerminationProtectionOutput, error) {
	return &cloudformation.UpdateTerminationProtectionOutput{}, nil
}

func (c *nebiusCFStub) UpdateStack(_ context.Context, input *cloudformation.UpdateStackInput, _ ...func(*cloudformation.Options)) (*cloudformation.UpdateStackOutput, error) {
	c.updated = input
	return &cloudformation.UpdateStackOutput{}, c.updateErr
}

func (c *nebiusCFStub) DescribeStacks(_ context.Context, _ *cloudformation.DescribeStacksInput, _ ...func(*cloudformation.Options)) (*cloudformation.DescribeStacksOutput, error) {
	c.describeCalls++
	if c.describeErr != nil && (c.describeErrAt == 0 || c.describeErrAt == c.describeCalls) {
		return nil, c.describeErr
	}
	return &cloudformation.DescribeStacksOutput{Stacks: []cftypes.Stack{{StackStatus: c.stackStatus, Outputs: c.stackOutputs}}}, nil
}

type nebiusTestDecrypter struct{}

func (nebiusTestDecrypter) Decrypt(_ context.Context, value string) (string, error) {
	if value == "from-default" {
		return "decrypted-default", nil
	}
	return value, nil
}

type nebiusS3Stub struct {
	buckets []*s3.CreateBucketInput
	uploads []*transfermanager.UploadObjectInput
}

func (s *nebiusS3Stub) CreateBucket(_ context.Context, input *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	s.buckets = append(s.buckets, input)
	return &s3.CreateBucketOutput{}, nil
}

func (s *nebiusS3Stub) UploadObject(_ context.Context, input *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	s.uploads = append(s.uploads, input)
	return &transfermanager.UploadObjectOutput{Location: aws.String("https://example.com/template")}, nil
}

func nebiusStackConfig(t *testing.T, stack string) *channel.SimpleConfig {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"cluster/config-defaults.yaml":       "stack_value: from-default\n",
		"cluster/cluster.yaml":               stack,
		"cluster/manifests/deletions.yaml":   "pre_apply:\n- name: '{{ .Values.ClusterStackOutputs.Name }}-pre'\n  kind: ConfigMap\npost_apply:\n- name: '{{ .Values.ClusterStackOutputs.Name }}-post'\n  kind: ConfigMap\n",
		"cluster/manifests/test/config.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: '{{ .Values.ClusterStackOutputs.Name }}'\n  namespace: default\n",
	}
	for name, contents := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0644))
	}
	config, err := channel.NewSimpleConfig("test", dir, false)
	require.NoError(t, err)
	return config
}

func TestNebiusClusterStackBeforeKubernetes(t *testing.T) {
	var events []string
	cf := &nebiusCFStub{events: &events, stackStatus: cftypes.StackStatusCreateComplete, stackOutputs: []cftypes.Output{{OutputKey: aws.String("Name"), OutputValue: aws.String("from-stack")}}}
	s3 := &nebiusS3Stub{}
	adapter := &awsAdapter{cloudformationClient: cf, s3Client: s3, s3Uploader: s3, region: "eu-central-1", logger: log.NewEntry(log.New())}
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "nebius-test", LocalID: "my-cluster", InfrastructureAccount: "aws:123456789012", Region: "eu-central-1"}
	p := newTestNebiusProvisioner()
	p.apply.secretDecrypter = nebiusTestDecrypter{}
	p.setupAWSAdapter = func(_ context.Context, _ *log.Entry, got *api.Cluster) (*awsAdapter, error) {
		require.Same(t, cluster, got)
		return adapter, nil
	}
	p.apply.newClient = func(got *api.Cluster) (kubernetesApplyClient, error) {
		require.Same(t, cluster, got)
		return &recordingClient{&recordingApplier{events: &events}, &recordingDeleter{events: &events}}, nil
	}
	config := nebiusStackConfig(t, "AWSTemplateFormatVersion: '2010-09-09'\nDescription: '{{ .Cluster.ConfigItems.stack_value }}'\nResources: {}\n")
	require.NoError(t, p.Provision(context.Background(), log.NewEntry(log.New()), cluster, config))
	require.Equal(t, []string{"stack", "delete:from-stack-pre", "apply:from-stack", "delete:from-stack-post"}, events)
	require.Equal(t, "my-cluster", aws.ToString(cf.created.StackName))
	require.Contains(t, aws.ToString(cf.created.TemplateBody), "decrypted-default")
	require.Contains(t, cf.created.Capabilities, cftypes.CapabilityCapabilityNamedIam)
	require.Empty(t, s3.buckets)
	require.Empty(t, s3.uploads)
}

func TestNebiusClusterStackLargeTemplate(t *testing.T) {
	var events []string
	cf := &nebiusCFStub{events: &events, stackStatus: cftypes.StackStatusCreateComplete}
	s3 := &nebiusS3Stub{}
	adapter := &awsAdapter{cloudformationClient: cf, s3Client: s3, s3Uploader: s3, region: "eu-central-1", logger: log.NewEntry(log.New())}
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "cluster-id", LocalID: "my-cluster", InfrastructureAccount: "aws:123456789012", Region: "eu-central-1"}
	config := nebiusStackConfig(t, "AWSTemplateFormatVersion: '2010-09-09'\nResources: {}\n# "+strings.Repeat("x", stackMaxSize))
	p := newTestNebiusProvisioner()
	_, err := p.provisionClusterStack(context.Background(), log.NewEntry(log.New()), adapter, cluster, config, map[string]interface{}{})
	require.NoError(t, err)
	require.Len(t, s3.buckets, 1)
	require.Equal(t, "cluster-lifecycle-manager-123456789012-eu-central-1", aws.ToString(s3.buckets[0].Bucket))
	require.Len(t, s3.uploads, 1)
	require.Equal(t, "cluster-id.template", aws.ToString(s3.uploads[0].Key))
	require.Equal(t, "https://example.com/template", aws.ToString(cf.created.TemplateURL))

	p.apply.dryRun = true
	s3.buckets, s3.uploads, events = nil, nil, nil
	_, err = p.provisionClusterStack(context.Background(), log.NewEntry(log.New()), adapter, cluster, config, map[string]interface{}{})
	require.NoError(t, err)
	require.Empty(t, s3.buckets)
	require.Empty(t, s3.uploads)
	require.Empty(t, events)
}

func TestNebiusClusterStackFailureStopsKubernetes(t *testing.T) {
	failure := errors.New("cloudformation failed")
	var events []string
	cf := &nebiusCFStub{events: &events, createErr: failure}
	adapter := &awsAdapter{cloudformationClient: cf, logger: log.NewEntry(log.New())}
	p := newTestNebiusProvisioner()
	p.setupAWSAdapter = func(context.Context, *log.Entry, *api.Cluster) (*awsAdapter, error) { return adapter, nil }
	p.apply.newClient = func(*api.Cluster) (kubernetesApplyClient, error) {
		t.Fatal("created Kubernetes client after stack failure")
		return nil, nil
	}
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "cluster-id", LocalID: "my-cluster", InfrastructureAccount: "aws:123456789012", Region: "eu-central-1"}
	err := p.Provision(context.Background(), log.NewEntry(log.New()), cluster, applyTestConfig(t))
	require.ErrorIs(t, err, failure)
	require.Equal(t, []string{"stack"}, events)
}

func TestNebiusClusterStackUpdate(t *testing.T) {
	var events []string
	cf := &nebiusCFStub{events: &events, createErr: &cftypes.AlreadyExistsException{}, stackStatus: cftypes.StackStatusUpdateComplete}
	adapter := &awsAdapter{cloudformationClient: cf, logger: log.NewEntry(log.New())}
	p := newTestNebiusProvisioner()
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, LocalID: "my-cluster"}
	_, err := p.provisionClusterStack(context.Background(), log.NewEntry(log.New()), adapter, cluster, applyTestConfig(t), map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, cf.updated)
	require.Equal(t, "my-cluster", aws.ToString(cf.updated.StackName))
}

func TestNebiusDryRunDoesNotModifyAWSOrKubernetes(t *testing.T) {
	var events []string
	cf := &nebiusCFStub{events: &events, stackStatus: cftypes.StackStatusCreateComplete}
	s3 := &nebiusS3Stub{}
	adapter := &awsAdapter{cloudformationClient: cf, s3Client: s3, s3Uploader: s3, logger: log.NewEntry(log.New())}
	p := newTestNebiusProvisioner()
	p.apply.dryRun = true
	p.setupAWSAdapter = func(context.Context, *log.Entry, *api.Cluster) (*awsAdapter, error) { return adapter, nil }
	p.apply.newClient = func(*api.Cluster) (kubernetesApplyClient, error) {
		t.Fatal("created Kubernetes client in dry run")
		return nil, nil
	}
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "test", LocalID: "test", InfrastructureAccount: "aws:123456789012", Region: "eu-central-1"}
	require.NoError(t, p.Provision(context.Background(), log.NewEntry(log.New()), cluster, applyTestConfig(t)))
	require.Empty(t, events)
	require.Zero(t, cf.describeCalls)
	require.Empty(t, s3.buckets)
	require.Empty(t, s3.uploads)
}

func TestNebiusDryRunCannotRenderUnavailableOutputs(t *testing.T) {
	var events []string
	cf := &nebiusCFStub{events: &events}
	adapter := &awsAdapter{cloudformationClient: cf, logger: log.NewEntry(log.New())}
	p := newTestNebiusProvisioner()
	p.apply.dryRun = true
	p.setupAWSAdapter = func(context.Context, *log.Entry, *api.Cluster) (*awsAdapter, error) { return adapter, nil }
	p.apply.newClient = func(*api.Cluster) (kubernetesApplyClient, error) {
		t.Fatal("created Kubernetes client in dry run")
		return nil, nil
	}
	cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "id", LocalID: "local", InfrastructureAccount: "aws:123456789012", Region: "eu-central-1"}
	config := nebiusStackConfig(t, "AWSTemplateFormatVersion: '2010-09-09'\nResources: {}\n")
	err := p.Provision(context.Background(), log.NewEntry(log.New()), cluster, config)
	require.ErrorContains(t, err, "Name")
	require.Empty(t, events)
}

func TestNebiusUnavailableInstanceTypes(t *testing.T) {
	context := newTemplateContext(nil, nil, nil, nil, nil, nil)
	_, err := instanceTypeCPUQuantity(context, "m5.large")
	require.ErrorContains(t, err, "instance type data is not available")
	_, err = instanceTypeMemoryQuantity(context, "m5.large")
	require.ErrorContains(t, err, "instance type data is not available")
}

func TestNebiusClusterStackReadFailuresStopKubernetes(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("describe-%d", failAt), func(t *testing.T) {
			failure := errors.New("describe failed")
			var events []string
			cf := &nebiusCFStub{events: &events, stackStatus: cftypes.StackStatusCreateComplete, describeErr: failure, describeErrAt: failAt}
			adapter := &awsAdapter{cloudformationClient: cf, logger: log.NewEntry(log.New())}
			p := newTestNebiusProvisioner()
			p.setupAWSAdapter = func(context.Context, *log.Entry, *api.Cluster) (*awsAdapter, error) { return adapter, nil }
			p.apply.newClient = func(*api.Cluster) (kubernetesApplyClient, error) {
				t.Fatal("created Kubernetes client after stack failure")
				return nil, nil
			}
			cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "test", LocalID: "test", InfrastructureAccount: "aws:123456789012", Region: "eu-central-1"}
			err := p.Provision(context.Background(), log.NewEntry(log.New()), cluster, applyTestConfig(t))
			require.ErrorIs(t, err, failure)
			require.Equal(t, []string{"stack"}, events)
		})
	}
}

func TestNebiusClusterStackValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cluster api.Cluster
		message string
	}{
		{"missing id", api.Cluster{Provider: api.ZalandoNebiusProvider, LocalID: "local", Region: "eu-central-1", InfrastructureAccount: "aws:123456789012"}, "id and local_id"},
		{"missing local id", api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "id", Region: "eu-central-1", InfrastructureAccount: "aws:123456789012"}, "id and local_id"},
		{"missing region", api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "id", LocalID: "local", InfrastructureAccount: "aws:123456789012"}, "AWS region"},
		{"invalid account", api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "id", LocalID: "local", Region: "eu-central-1", InfrastructureAccount: "nebius:account"}, "AWS infrastructure account"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestNebiusProvisioner()
			p.setupAWSAdapter = func(context.Context, *log.Entry, *api.Cluster) (*awsAdapter, error) {
				t.Fatal("created AWS adapter for invalid cluster")
				return nil, nil
			}
			err := p.Provision(context.Background(), log.NewEntry(log.New()), &tc.cluster, applyTestConfig(t))
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestNebiusInvalidStackDoesNotModifyAWS(t *testing.T) {
	for _, tc := range []struct {
		name, stack, message string
	}{
		{"missing value", "Description: '{{ .Values.missing }}'\nResources: {}\n", "unable to render cluster stack"},
		{"invalid yaml", "Resources: [invalid", "unable to parse cluster stack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []string
			cf := &nebiusCFStub{events: &events}
			s3 := &nebiusS3Stub{}
			adapter := &awsAdapter{cloudformationClient: cf, s3Client: s3, s3Uploader: s3, logger: log.NewEntry(log.New())}
			p := newTestNebiusProvisioner()
			p.setupAWSAdapter = func(context.Context, *log.Entry, *api.Cluster) (*awsAdapter, error) { return adapter, nil }
			p.apply.newClient = func(*api.Cluster) (kubernetesApplyClient, error) {
				t.Fatal("created Kubernetes client for invalid stack")
				return nil, nil
			}
			cluster := &api.Cluster{Provider: api.ZalandoNebiusProvider, ID: "id", LocalID: "local", InfrastructureAccount: "aws:123456789012", Region: "eu-central-1"}
			err := p.Provision(context.Background(), log.NewEntry(log.New()), cluster, nebiusStackConfig(t, tc.stack))
			require.ErrorContains(t, err, tc.message)
			require.Empty(t, events)
			require.Empty(t, s3.buckets)
			require.Empty(t, s3.uploads)
		})
	}
}
