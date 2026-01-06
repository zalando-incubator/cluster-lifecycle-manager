package provisioner

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws/iface"

	log "github.com/sirupsen/logrus"
)

type ec2APIStub struct {
	iface.EC2API
}

func (e *ec2APIStub) DescribeVpcs(context.Context, *ec2.DescribeVpcsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	return &ec2.DescribeVpcsOutput{
		Vpcs: []ec2types.Vpc{{IsDefault: aws.Bool(false)}},
	}, nil
}

type s3APIStub struct{}

func (s *s3APIStub) CreateBucket(_ context.Context, params *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	if *params.Bucket == "existing" {
		return nil, &s3types.BucketAlreadyOwnedByYou{}
	}
	return nil, nil
}

type s3UploaderAPIStub struct {
	err error
}

func (s *s3UploaderAPIStub) Upload(context.Context, *s3.PutObjectInput, ...func(*manager.Uploader)) (*manager.UploadOutput, error) {
	return &manager.UploadOutput{Location: "url"}, s.err
}

type cloudFormationAPIStub struct {
	cloudformationAPI
	statusMutex         *sync.Mutex
	status              cftypes.StackStatus
	statusReason        *string
	onDescribeStackChan chan struct{}
	createErr           error
	updateErr           error
	deleteErr           error
	onCreate            func(input *cloudformation.CreateStackInput)
}

func (c *cloudFormationAPIStub) DescribeStacks(context.Context, *cloudformation.DescribeStacksInput, ...func(*cloudformation.Options)) (*cloudformation.DescribeStacksOutput, error) {
	name := "foobar"
	s := cftypes.Stack{StackName: aws.String(name), StackStatus: c.getStatus(), StackStatusReason: c.getStatusReason()}
	if c.onDescribeStackChan != nil {
		c.onDescribeStackChan <- struct{}{}
	}
	return &cloudformation.DescribeStacksOutput{Stacks: []cftypes.Stack{s}}, nil
}

func (c *cloudFormationAPIStub) CreateStack(_ context.Context, params *cloudformation.CreateStackInput, _ ...func(*cloudformation.Options)) (*cloudformation.CreateStackOutput, error) {
	if c.onCreate != nil {
		c.onCreate(params)
	}
	return nil, c.createErr
}

func (c *cloudFormationAPIStub) UpdateStack(context.Context, *cloudformation.UpdateStackInput, ...func(*cloudformation.Options)) (*cloudformation.UpdateStackOutput, error) {
	return nil, c.updateErr
}

func (c *cloudFormationAPIStub) DeleteStack(context.Context, *cloudformation.DeleteStackInput, ...func(*cloudformation.Options)) (*cloudformation.DeleteStackOutput, error) {
	return nil, c.deleteErr
}

func (c *cloudFormationAPIStub) UpdateTerminationProtection(context.Context, *cloudformation.UpdateTerminationProtectionInput, ...func(*cloudformation.Options)) (*cloudformation.UpdateTerminationProtectionOutput, error) {
	return nil, nil
}

func (c *cloudFormationAPIStub) setStatus(status cftypes.StackStatus) {
	c.statusMutex.Lock()
	c.status = status
	c.statusMutex.Unlock()
}

func (c *cloudFormationAPIStub) getStatus() cftypes.StackStatus {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	return c.status
}

func (c *cloudFormationAPIStub) getStatusReason() *string {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	return c.statusReason
}

type cloudFormationAPIStub2 struct {
	cloudformationAPI
	stacks []cftypes.Stack
	err    error
}

func (cf *cloudFormationAPIStub2) DescribeStacks(context.Context, *cloudformation.DescribeStacksInput, ...func(*cloudformation.Options)) (*cloudformation.DescribeStacksOutput, error) {
	return &cloudformation.DescribeStacksOutput{
		Stacks: cf.stacks,
	}, cf.err
}

type autoscalingAPIStub struct {
	iface.AutoScalingAPI
	groupName string
}

func (a *autoscalingAPIStub) DescribeAutoScalingGroups(context.Context, *autoscaling.DescribeAutoScalingGroupsInput, ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	group := autoscalingtypes.AutoScalingGroup{AutoScalingGroupName: aws.String(a.groupName)}
	return &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: []autoscalingtypes.AutoScalingGroup{group}}, nil
}

func (a *autoscalingAPIStub) UpdateAutoScalingGroup(context.Context, *autoscaling.UpdateAutoScalingGroupInput, ...func(*autoscaling.Options)) (*autoscaling.UpdateAutoScalingGroupOutput, error) {
	return nil, nil
}
func (a *autoscalingAPIStub) SuspendProcesses(context.Context, *autoscaling.SuspendProcessesInput, ...func(*autoscaling.Options)) (*autoscaling.SuspendProcessesOutput, error) {
	return nil, nil
}
func (a *autoscalingAPIStub) ResumeProcesses(context.Context, *autoscaling.ResumeProcessesInput, ...func(*autoscaling.Options)) (*autoscaling.ResumeProcessesOutput, error) {
	return nil, nil
}
func (a *autoscalingAPIStub) TerminateInstanceInAutoScalingGroup(context.Context, *autoscaling.TerminateInstanceInAutoScalingGroupInput, ...func(*autoscaling.Options)) (*autoscaling.TerminateInstanceInAutoScalingGroupOutput, error) {
	return nil, nil
}

func newAWSAdapterWithStubs(status cftypes.StackStatus, groupName string) *awsAdapter {
	logger := log.WithField("cluster", "foobar")

	return &awsAdapter{
		// session:              &session.Session{Config: &aws.Config{Region: aws.String("")}},
		cloudformationClient: &cloudFormationAPIStub{statusMutex: &sync.Mutex{}, status: status},
		s3Client:             &s3APIStub{},
		s3Uploader:           &s3UploaderAPIStub{},
		ec2Client:            &ec2APIStub{},
		autoscalingClient:    &autoscalingAPIStub{groupName: groupName},
		dryRun:               false,
		logger:               logger,
	}
}

func TestWaitForStack(t *testing.T) {
	t.Run("WaitForStackWithComplete", testWaitForStackWithComplete)
	t.Run("WaitForStackWithTimeout", testWaitForStackWithTimeout)
	t.Run("WaitForStackWithRollback", testWaitForStackWithRollback)
}

func testWaitForStackWithComplete(t *testing.T) {
	// Happy Case uses default stub
	awsMock := newAWSAdapterWithStubs(cftypes.StackStatusCreateComplete, "123")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := awsMock.waitForStack(ctx, 100*time.Millisecond, "foobar")
	if err != nil {
		t.Error(err)
	}
}

func testWaitForStackWithTimeout(t *testing.T) {
	// Timeout Case
	awsMock := newAWSAdapterWithStubs(cftypes.StackStatusCreateComplete, "123")
	onDescribeStackChan := make(chan struct{})
	stub := &cloudFormationAPIStub{statusMutex: &sync.Mutex{}, status: cftypes.StackStatusCreateInProgress, onDescribeStackChan: onDescribeStackChan}
	awsMock.cloudformationClient = stub

	ctx, cancel := context.WithCancel(context.Background())
	counter := 0
	go func() {
		for range onDescribeStackChan {
			counter++
			if counter == 2 {
				cancel()
				return
			}
		}
	}()
	err := awsMock.waitForStack(ctx, 100*time.Millisecond, "foobar")
	if err != errTimeoutExceeded {
		t.Errorf("should return timeout exceeded, got: %v", err)
	}
	if counter != 2 {
		t.Error("should be called twice")
	}
}

func testWaitForStackWithRollback(t *testing.T) {
	// Wait and Rollback Case
	awsMock := newAWSAdapterWithStubs(cftypes.StackStatusCreateComplete, "123")
	onDescribeStackChan := make(chan struct{}, 2)
	stub := &cloudFormationAPIStub{statusMutex: &sync.Mutex{}, status: cftypes.StackStatusCreateInProgress,
		statusReason:        aws.String("The following resource(s) failed to update: [AutoScalingGroup1b, AutoScalingGroup1c]."),
		onDescribeStackChan: onDescribeStackChan}
	awsMock.cloudformationClient = stub

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	counter := 0
	go func() {
		for range onDescribeStackChan {
			counter++
			if counter == 2 {
				//change the status of the stack
				stub.setStatus(cftypes.StackStatusRollbackComplete)
				return
			}
		}
	}()

	err := awsMock.waitForStack(ctx, 100*time.Millisecond, "foobar")
	if err == nil || !strings.Contains(err.Error(), errRollbackComplete.Error()) {
		t.Errorf("should return rollback complete, got: %v", err)
	}
	if counter != 2 {
		t.Error("should be called twice")
	}
}

func TestGetStackByName(t *testing.T) {
	a := newAWSAdapterWithStubs("", "GroupName")
	s, err := a.getStackByName(context.Background(), "foobar")
	if err != nil {
		t.FailNow()
	}
	if *s.StackName != "foobar" {
		t.Fatalf("expected %s, found %s", "foobar", *s.StackName)
	}
}

func TestGetDefaultVPC(t *testing.T) {
	a := newAWSAdapterWithStubs("", "GroupName")
	vpc, err := a.GetDefaultVPC(context.Background())
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}

	if vpc == nil {
		t.Fatal("Fail: no VPC returned")
	}
}

func TestCreateS3Client(t *testing.T) {
	var tests = []struct {
		bucketName string
	}{
		{""},
		{"existing"},
	}

	for _, testCase := range tests {
		a := newAWSAdapterWithStubs("", "GroupName")
		err := a.createS3Bucket(context.Background(), testCase.bucketName)
		if err != nil {
			t.Fatalf("fail: %v", err)
		}
	}
}

func TestCreateOrUpdateClusterStack(t *testing.T) {
	awsAdapter := newAWSAdapterWithStubs(cftypes.StackStatusCreateComplete, "123")
	cluster := &api.Cluster{
		ID:                    "cluster-id",
		InfrastructureAccount: "account-id",
		Region:                "eu-central-1",
	}
	s3Bucket := "s3-bucket"

	// test creating stack with small stack template
	err := awsAdapter.applyClusterStack(context.Background(), "stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.NoError(t, err)

	templateValue := make([]string, stackMaxSize+1)
	for i := range templateValue {
		templateValue[i] = "x"
	}
	hugeTemplate := fmt.Sprintf("{\"stack\": \"%s\"}", strings.Join(templateValue, ""))

	// test create when template is too big and must be uploaded to s3
	awsAdapter.s3Uploader = &s3UploaderAPIStub{}
	err = awsAdapter.applyClusterStack(context.Background(), "stack-name", hugeTemplate, cluster, s3Bucket)
	assert.NoError(t, err)

	// test create bucket failing when s3 upload fails
	awsAdapter.s3Uploader = &s3UploaderAPIStub{errors.New("error")}
	err = awsAdapter.applyClusterStack(context.Background(), "stack-name", hugeTemplate, cluster, s3Bucket)
	assert.Error(t, err)

	// test updating existing stack
	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		statusMutex: &sync.Mutex{},
		createErr:   &cftypes.AlreadyExistsException{},
	}
	err = awsAdapter.applyClusterStack(context.Background(), "stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.NoError(t, err)

	// test create failing
	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		statusMutex: &sync.Mutex{},
		createErr:   errors.New("error"),
	}
	err = awsAdapter.applyClusterStack(context.Background(), "stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.Error(t, err)

	// test updating when stack is already up to date
	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		statusMutex: &sync.Mutex{},
		createErr:   &cftypes.AlreadyExistsException{},
		updateErr: &smithy.GenericAPIError{
			Code:    "ValidationError",
			Message: cloudformationNoUpdateMsg,
		},
	}
	err = awsAdapter.applyClusterStack(context.Background(), "stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.NoError(t, err)

	// test update failing
	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		statusMutex: &sync.Mutex{},
		createErr:   &cftypes.AlreadyExistsException{},
		updateErr:   errors.New("error"),
	}
	err = awsAdapter.applyClusterStack(context.Background(), "stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.Error(t, err)
}

func TestApplyStack(t *testing.T) {
	awsAdapter := newAWSAdapterWithStubs(cftypes.StackStatusCreateComplete, "123")

	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		onCreate: func(input *cloudformation.CreateStackInput) {
			require.EqualValues(t, []cftypes.Tag{
				{
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				},
			}, input.Tags)
		},
		statusMutex: &sync.Mutex{},
	}

	err := awsAdapter.applyStack(context.Background(), "stack-name", `{"Metadata": {"Tags": {"foo":"bar"}}}`, "", nil, true, nil)
	require.NoError(t, err)
}

func TestIsStackDeleting(t *testing.T) {
	stack := &cftypes.Stack{
		StackStatus: cftypes.StackStatusDeleteInProgress,
	}

	assert.True(t, isStackDeleting(stack))

	stack.StackStatus = cftypes.StackStatusCreateComplete
	assert.False(t, isStackDeleting(stack))
}

func TestListStacks(tt *testing.T) {
	for _, tc := range []struct {
		msg            string
		cloudformation cloudformationAPI
		includeTags    map[string]string
		excludeTags    map[string]string
		expectedStacks []cftypes.Stack
	}{
		{
			msg: "no tag filter should return all stacks",
			cloudformation: &cloudFormationAPIStub2{
				stacks: []cftypes.Stack{
					{
						StackName: aws.String("x"),
					},
				},
			},
			includeTags: nil,
			excludeTags: nil,
			expectedStacks: []cftypes.Stack{
				{
					StackName: aws.String("x"),
				},
			},
		},
		{
			msg: "include tags + exclude tags should limit the returned stacks",
			cloudformation: &cloudFormationAPIStub2{
				stacks: []cftypes.Stack{
					{
						StackName: aws.String("x"),
						Tags: []cftypes.Tag{
							{
								Key:   aws.String("a"),
								Value: aws.String("b"),
							},
						},
					},
					{
						StackName: aws.String("y"),
						Tags: []cftypes.Tag{
							{
								Key:   aws.String("a"),
								Value: aws.String("b"),
							},
							{
								Key:   aws.String("c"),
								Value: aws.String("d"),
							},
						},
					},
				},
			},
			includeTags: map[string]string{
				"a": "b",
			},
			excludeTags: map[string]string{
				"c": "d",
			},
			expectedStacks: []cftypes.Stack{
				{
					StackName: aws.String("x"),
					Tags: []cftypes.Tag{
						{
							Key:   aws.String("a"),
							Value: aws.String("b"),
						},
					},
				},
			},
		},
	} {
		tt.Run(tc.msg, func(t *testing.T) {
			adapter := awsAdapter{
				cloudformationClient: tc.cloudformation,
			}
			stacks, _ := adapter.ListStacks(context.Background(), tc.includeTags, tc.excludeTags)
			assert.Equal(t, tc.expectedStacks, stacks)
		})
	}
}

type mockKMSAPI struct {
	kmsAPI
	expectedKeyID string
	expectedValue []byte
	encryptResult []byte
	keyARN        string
	fail          bool
}

func (mock mockKMSAPI) Encrypt(_ context.Context, params *kms.EncryptInput, _ ...func(*kms.Options)) (*kms.EncryptOutput, error) {
	keyID := aws.ToString(params.KeyId)
	if keyID != mock.expectedKeyID {
		return nil, fmt.Errorf("unexpected key ID %s", keyID)
	}
	if !reflect.DeepEqual(params.Plaintext, mock.expectedValue) {
		return nil, fmt.Errorf("unexpected value: %v", params.Plaintext)
	}
	if mock.fail {
		return nil, errors.New("KMS operation failed")
	}
	return &kms.EncryptOutput{
		CiphertextBlob: mock.encryptResult,
	}, nil
}

func (mock mockKMSAPI) DescribeKey(_ context.Context, params *kms.DescribeKeyInput, _ ...func(*kms.Options)) (*kms.DescribeKeyOutput, error) {
	keyID := aws.ToString(params.KeyId)
	if keyID != mock.expectedKeyID {
		return nil, fmt.Errorf("unexpected key ID %s", keyID)
	}
	if mock.fail {
		return nil, errors.New("KMS operation failed")
	}

	return &kms.DescribeKeyOutput{
		KeyMetadata: &kmstypes.KeyMetadata{
			Arn: aws.String(mock.keyARN),
		},
	}, nil
}

func TestKMSEncryptForTaupage(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail bool
	}{
		{
			name: "encryption succeeds",
			fail: false,
		},
		{
			name: "encryption fails",
			fail: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := awsAdapter{
				kmsClient: mockKMSAPI{
					expectedKeyID: "key-id",
					expectedValue: []byte("test"),
					encryptResult: []byte("foobar"),
					fail:          tc.fail,
				},
			}

			result, err := adapter.kmsEncryptForTaupage(context.Background(), "key-id", "test")

			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "aws:kms:Zm9vYmFy", result)
			}
		})
	}
}

func TestKMSKeyARN(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail bool
	}{
		{
			name: "lookup succeeds",
			fail: false,
		},
		{
			name: "lookup fails",
			fail: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := awsAdapter{
				kmsClient: mockKMSAPI{
					expectedKeyID: "key-id",
					expectedValue: []byte("test"),
					keyARN:        "arn:aws:key/1234",
					fail:          tc.fail,
				},
			}

			result, err := adapter.resolveKeyID(context.Background(), "key-id")

			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "arn:aws:key/1234", result)
			}
		})
	}
}
