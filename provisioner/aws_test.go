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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/kms/kmsiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"

	log "github.com/sirupsen/logrus"
)

type ec2APIStub struct {
	ec2iface.EC2API
}

func (e *ec2APIStub) DescribeVpcs(*ec2.DescribeVpcsInput) (
	*ec2.DescribeVpcsOutput,
	error,
) {
	return &ec2.DescribeVpcsOutput{
		Vpcs: []*ec2.Vpc{&ec2.Vpc{IsDefault: aws.Bool(false)}},
	}, nil
}

type s3APIStub struct{}

func (s *s3APIStub) CreateBucket(input *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
	return nil, nil
}

type cloudFormationAPIStub struct {
	cloudformationiface.CloudFormationAPI
	statusMutex         *sync.Mutex
	status              *string
	statusReason        *string
	onDescribeStackChan chan struct{}
	createErr           error
	updateErr           error
	deleteErr           error
	onCreate            func(input *cloudformation.CreateStackInput)
}

func (c *cloudFormationAPIStub) DescribeStacks(input *cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error) {
	name := "foobar"
	s := cloudformation.Stack{StackName: aws.String(name), StackStatus: c.getStatus(), StackStatusReason: c.getStatusReason()}
	if c.onDescribeStackChan != nil {
		c.onDescribeStackChan <- struct{}{}
	}
	return &cloudformation.DescribeStacksOutput{Stacks: []*cloudformation.Stack{&s}}, nil
}

func (c *cloudFormationAPIStub) CreateStack(input *cloudformation.CreateStackInput) (*cloudformation.CreateStackOutput, error) {
	if c.onCreate != nil {
		c.onCreate(input)
	}
	return nil, c.createErr
}

func (c *cloudFormationAPIStub) UpdateStack(input *cloudformation.UpdateStackInput) (*cloudformation.UpdateStackOutput, error) {
	return nil, c.updateErr
}

func (c *cloudFormationAPIStub) DeleteStack(input *cloudformation.DeleteStackInput) (*cloudformation.DeleteStackOutput, error) {
	return nil, c.deleteErr
}

func (c *cloudFormationAPIStub) UpdateTerminationProtection(input *cloudformation.UpdateTerminationProtectionInput) (*cloudformation.UpdateTerminationProtectionOutput, error) {
	return nil, nil
}

func (c *cloudFormationAPIStub) DescribeStacksPages(input *cloudformation.DescribeStacksInput, fn func(resp *cloudformation.DescribeStacksOutput, lastPage bool) bool) error {
	return nil
}

func (c *cloudFormationAPIStub) setStatus(status string) {
	c.statusMutex.Lock()
	c.status = &status
	c.statusMutex.Unlock()
}

func (c *cloudFormationAPIStub) getStatus() *string {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	return c.status
}

func (c *cloudFormationAPIStub) setStatusReason(statusReason string) {
	c.statusMutex.Lock()
	c.statusReason = &statusReason
	c.statusMutex.Unlock()
}

func (c *cloudFormationAPIStub) getStatusReason() *string {
	c.statusMutex.Lock()
	defer c.statusMutex.Unlock()
	return c.statusReason
}

type cloudFormationAPIStub2 struct {
	cloudformationiface.CloudFormationAPI
	stacks []*cloudformation.Stack
	err    error
}

func (cf *cloudFormationAPIStub2) DescribeStacksPages(input *cloudformation.DescribeStacksInput, fn func(*cloudformation.DescribeStacksOutput, bool) bool) error {
	if len(cf.stacks) > 0 {
		fn(&cloudformation.DescribeStacksOutput{
			Stacks: cf.stacks,
		}, true)
		return nil
	}
	return cf.err
}

type autoscalingAPIStub struct {
	groupName string
}

func (a *autoscalingAPIStub) DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	group := &autoscaling.Group{AutoScalingGroupName: aws.String(a.groupName)}
	return &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: []*autoscaling.Group{group}}, nil
}

func (a *autoscalingAPIStub) DescribeLaunchConfigurations(input *autoscaling.DescribeLaunchConfigurationsInput) (*autoscaling.DescribeLaunchConfigurationsOutput, error) {
	return nil, nil
}
func (a *autoscalingAPIStub) UpdateAutoScalingGroup(input *autoscaling.UpdateAutoScalingGroupInput) (*autoscaling.UpdateAutoScalingGroupOutput, error) {
	return nil, nil
}
func (a *autoscalingAPIStub) SuspendProcesses(input *autoscaling.ScalingProcessQuery) (*autoscaling.SuspendProcessesOutput, error) {
	return nil, nil
}
func (a *autoscalingAPIStub) ResumeProcesses(*autoscaling.ScalingProcessQuery) (*autoscaling.ResumeProcessesOutput, error) {
	return nil, nil
}
func (a *autoscalingAPIStub) TerminateInstanceInAutoScalingGroup(*autoscaling.TerminateInstanceInAutoScalingGroupInput) (*autoscaling.TerminateInstanceInAutoScalingGroupOutput, error) {
	return nil, nil
}

type s3UploaderAPIStub struct {
	err error
}

func (s *s3UploaderAPIStub) Upload(input *s3manager.UploadInput, options ...func(*s3manager.Uploader)) (*s3manager.UploadOutput, error) {
	return &s3manager.UploadOutput{Location: "url"}, s.err
}

func newAWSAdapterWithStubs(status string, groupName string) *awsAdapter {
	logger := log.WithField("cluster", "foobar")

	return &awsAdapter{
		session:              &session.Session{Config: &aws.Config{Region: aws.String("")}},
		cloudformationClient: &cloudFormationAPIStub{statusMutex: &sync.Mutex{}, status: aws.String(status)},
		s3Client:             &s3APIStub{},
		ec2Client:            &ec2APIStub{},
		autoscalingClient:    &autoscalingAPIStub{groupName: groupName},
		apiServer:            "",
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
	awsMock := newAWSAdapterWithStubs(cloudformation.StackStatusCreateComplete, "123")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := awsMock.waitForStack(ctx, 100*time.Millisecond, "foobar")
	if err != nil {
		t.Error(err)
	}
}

func testWaitForStackWithTimeout(t *testing.T) {
	// Timeout Case
	awsMock := newAWSAdapterWithStubs(cloudformation.StackStatusCreateComplete, "123")
	onDescribeStackChan := make(chan struct{})
	stub := &cloudFormationAPIStub{statusMutex: &sync.Mutex{}, status: aws.String(cloudformation.StackStatusCreateInProgress), onDescribeStackChan: onDescribeStackChan}
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
	awsMock := newAWSAdapterWithStubs(cloudformation.StackStatusCreateComplete, "123")
	onDescribeStackChan := make(chan struct{}, 2)
	stub := &cloudFormationAPIStub{statusMutex: &sync.Mutex{}, status: aws.String(cloudformation.StackStatusCreateInProgress),
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
				stub.setStatus(cloudformation.StackStatusRollbackComplete)
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
	s, err := a.getStackByName("foobar")
	if err != nil {
		t.FailNow()
	}
	if *s.StackName != "foobar" {
		t.Fatalf("expected %s, found %s", "foobar", *s.StackName)
	}
}

func TestGetDefaultVPC(t *testing.T) {
	a := newAWSAdapterWithStubs("", "GroupName")
	vpc, err := a.GetDefaultVPC()
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}

	if vpc == nil {
		t.Fatal("Fail: no VPC returned")
	}
}

func TestCreateS3Client(t *testing.T) {
	a := newAWSAdapterWithStubs("", "GroupName")
	err := a.createS3Bucket("")
	if err != nil {
		t.Fatalf("fail: %v", err)
	}
}

func TestCreateOrUpdateClusterStack(t *testing.T) {
	awsAdapter := newAWSAdapterWithStubs(cloudformation.StackStatusCreateComplete, "123")
	cluster := &api.Cluster{
		ID:                    "cluster-id",
		InfrastructureAccount: "account-id",
		Region:                "eu-central-1",
	}
	s3Bucket := "s3-bucket"

	// test creating stack with small stack template
	err := awsAdapter.applyClusterStack("stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.NoError(t, err)

	templateValue := make([]string, stackMaxSize+1)
	for i := range templateValue {
		templateValue[i] = "x"
	}
	hugeTemplate := fmt.Sprintf("{\"stack\": \"%s\"}", strings.Join(templateValue, ""))

	// test create when template is too big and must be uploaded to s3
	awsAdapter.s3Uploader = &s3UploaderAPIStub{}
	err = awsAdapter.applyClusterStack("stack-name", hugeTemplate, cluster, s3Bucket)
	assert.NoError(t, err)

	// test create bucket failing when s3 upload fails
	awsAdapter.s3Uploader = &s3UploaderAPIStub{errors.New("error")}
	err = awsAdapter.applyClusterStack("stack-name", hugeTemplate, cluster, s3Bucket)
	assert.Error(t, err)

	// test updating existing stack
	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		statusMutex: &sync.Mutex{},
		createErr: awserr.New(
			cloudformation.ErrCodeAlreadyExistsException,
			"",
			errors.New("base error"),
		),
	}
	err = awsAdapter.applyClusterStack("stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.NoError(t, err)

	// test create failing
	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		statusMutex: &sync.Mutex{},
		createErr:   errors.New("error"),
	}
	err = awsAdapter.applyClusterStack("stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.Error(t, err)

	// test updating when stack is already up to date
	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		statusMutex: &sync.Mutex{},
		createErr: awserr.New(
			cloudformation.ErrCodeAlreadyExistsException,
			"",
			errors.New("base error"),
		),
		updateErr: awserr.New(
			cloudformationValidationErr,
			cloudformationNoUpdateMsg,
			errors.New("base error"),
		),
	}
	err = awsAdapter.applyClusterStack("stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.NoError(t, err)

	// test update failing
	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		statusMutex: &sync.Mutex{},
		createErr: awserr.New(
			cloudformation.ErrCodeAlreadyExistsException,
			"",
			errors.New("base error"),
		),
		updateErr: errors.New("error"),
	}
	err = awsAdapter.applyClusterStack("stack-name", `{"stack": "template"}`, cluster, s3Bucket)
	assert.Error(t, err)
}

func TestApplyStack(t *testing.T) {
	awsAdapter := newAWSAdapterWithStubs(cloudformation.StackStatusCreateComplete, "123")

	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		onCreate: func(input *cloudformation.CreateStackInput) {
			require.EqualValues(t, []*cloudformation.Tag{
				{
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				},
			}, input.Tags)
		},
		statusMutex: &sync.Mutex{},
	}

	err := awsAdapter.applyStack("stack-name", `{"Metadata": {"Tags": {"foo":"bar"}}}`, "", nil, true, nil)
	require.NoError(t, err)
}

func TestIsStackDeleting(t *testing.T) {
	stack := &cloudformation.Stack{
		StackStatus: aws.String(cloudformation.StackStatusDeleteInProgress),
	}

	assert.True(t, isStackDeleting(stack))

	stack.StackStatus = aws.String(cloudformation.StackStatusCreateComplete)
	assert.False(t, isStackDeleting(stack))
}

func TestListStacks(tt *testing.T) {
	for _, tc := range []struct {
		msg            string
		cloudformation cloudformationiface.CloudFormationAPI
		includeTags    map[string]string
		excludeTags    map[string]string
		expectedStacks []*cloudformation.Stack
	}{
		{
			msg: "no tag filter should return all stacks",
			cloudformation: &cloudFormationAPIStub2{
				stacks: []*cloudformation.Stack{
					{
						StackName: aws.String("x"),
					},
				},
			},
			includeTags: nil,
			excludeTags: nil,
			expectedStacks: []*cloudformation.Stack{
				{
					StackName: aws.String("x"),
				},
			},
		},
		{
			msg: "include tags + exclude tags should limit the returned stacks",
			cloudformation: &cloudFormationAPIStub2{
				stacks: []*cloudformation.Stack{
					{
						StackName: aws.String("x"),
						Tags: []*cloudformation.Tag{
							{
								Key:   aws.String("a"),
								Value: aws.String("b"),
							},
						},
					},
					{
						StackName: aws.String("y"),
						Tags: []*cloudformation.Tag{
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
			expectedStacks: []*cloudformation.Stack{
				{
					StackName: aws.String("x"),
					Tags: []*cloudformation.Tag{
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
			stacks, _ := adapter.ListStacks(tc.includeTags, tc.excludeTags)
			assert.Equal(t, tc.expectedStacks, stacks)
		})
	}
}

type mockKMSAPI struct {
	kmsiface.KMSAPI
	expectedKeyID string
	expectedValue []byte
	encryptResult []byte
	keyARN        string
	fail          bool
}

func (mock mockKMSAPI) Encrypt(input *kms.EncryptInput) (*kms.EncryptOutput, error) {
	keyID := aws.StringValue(input.KeyId)
	if keyID != mock.expectedKeyID {
		return nil, fmt.Errorf("unexpected key ID %s", keyID)
	}
	if !reflect.DeepEqual(input.Plaintext, mock.expectedValue) {
		return nil, fmt.Errorf("unexpected value: %v", input.Plaintext)
	}
	if mock.fail {
		return nil, errors.New("KMS operation failed")
	}
	return &kms.EncryptOutput{
		CiphertextBlob: mock.encryptResult,
	}, nil
}

func (mock mockKMSAPI) DescribeKey(input *kms.DescribeKeyInput) (*kms.DescribeKeyOutput, error) {
	keyID := aws.StringValue(input.KeyId)
	if keyID != mock.expectedKeyID {
		return nil, fmt.Errorf("unexpected key ID %s", keyID)
	}
	if mock.fail {
		return nil, errors.New("KMS operation failed")
	}

	return &kms.DescribeKeyOutput{
		KeyMetadata: &kms.KeyMetadata{
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

			result, err := adapter.kmsEncryptForTaupage("key-id", "test")

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

			result, err := adapter.resolveKeyID("key-id")

			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "arn:aws:key/1234", result)
			}
		})
	}
}
