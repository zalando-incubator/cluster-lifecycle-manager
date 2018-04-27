package provisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/stretchr/testify/assert"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"

	log "github.com/sirupsen/logrus"
)

type s3APIStub struct{}

func (s *s3APIStub) CreateBucket(input *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
	return nil, nil
}

type cloudFormationAPIStub struct {
	statusMutex         *sync.Mutex
	status              *string
	onDescribeStackChan chan struct{}
	createErr           error
	updateErr           error
	deleteErr           error
}

func (c *cloudFormationAPIStub) DescribeStacks(input *cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error) {
	name := "foobar"
	s := cloudformation.Stack{StackName: aws.String(name), StackStatus: c.getStatus()}
	if c.onDescribeStackChan != nil {
		c.onDescribeStackChan <- struct{}{}
	}
	return &cloudformation.DescribeStacksOutput{Stacks: []*cloudformation.Stack{&s}}, nil
}

func (c *cloudFormationAPIStub) CreateStack(input *cloudformation.CreateStackInput) (*cloudformation.CreateStackOutput, error) {
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
	_, err := awsMock.waitForStack(ctx, 100*time.Millisecond, "foobar")
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
	_, err := awsMock.waitForStack(ctx, 100*time.Millisecond, "foobar")
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
	stub := &cloudFormationAPIStub{statusMutex: &sync.Mutex{}, status: aws.String(cloudformation.StackStatusCreateInProgress), onDescribeStackChan: onDescribeStackChan}
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

	_, err := awsMock.waitForStack(ctx, 100*time.Millisecond, "foobar")
	if err != errRollbackComplete {
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

func TestEncodeUserdata(t *testing.T) {
	encoded, err := encodeUserData("foobar")
	if err != nil {
		t.FailNow()
	}
	decoded, _ := decodeUserData(encoded)
	if "foobar" != decoded {
		t.Fatalf("expected %s, got %s", "foobar", decoded)
	}
}

func TestCreateS3Client(t *testing.T) {
	a := newAWSAdapterWithStubs("", "GroupName")
	err := a.createS3Bucket("")
	if err != nil {
		t.Fatalf("fail: %v", err)
	}
}

func TestDescribeCorrectASG(t *testing.T) {
	a := newAWSAdapterWithStubs("", "GroupName")
	group, err := a.describeASG("GroupName")
	if err != nil {
		t.Fatal(err.Error())
	}
	if *group.AutoScalingGroupName != "GroupName" {
		t.Fatalf("expected %s, got %s", "GroupName", *group.AutoScalingGroupName)
	}
}

func TestAsgHasTags(t *testing.T) {
	expected := []*autoscaling.TagDescription{{Key: aws.String("key-1"), Value: aws.String("value-1")},
		{Key: aws.String("key-2"), Value: aws.String("value-2")}}
	tags := []*autoscaling.TagDescription{{Key: aws.String("key-2"), Value: aws.String("value-2")},
		{Key: aws.String("key-1"), Value: aws.String("value-1")}}
	hasTags := asgHasTags(expected, tags)
	if !hasTags {
		t.Fatalf("expected %v, tags %v should succeed", expected, tags)
	}
}

func TestNotMatchingAsgHasTags(t *testing.T) {
	expected := []*autoscaling.TagDescription{{Key: aws.String("key"), Value: aws.String("value")}}
	tags := []*autoscaling.TagDescription{{Key: aws.String("key"), Value: aws.String("bar")}}
	hasTags := asgHasTags(expected, tags)
	if hasTags {
		t.Fatalf("expected %v, tags %v should not succeed", expected, hasTags)
	}
}

func TestCreateOrUpdateClusterStack(t *testing.T) {
	awsAdapter := newAWSAdapterWithStubs(cloudformation.StackStatusCreateComplete, "123")
	cluster := &api.Cluster{
		ID: "cluster-id",
		InfrastructureAccount: "account-id",
		Region:                "eu-central-1",
	}
	s3Bucket := "s3-bucket"

	// test creating stack with small stack template
	err := awsAdapter.applyClusterStack("stack-name", []byte(`{"stack": "template"}`), cluster, s3Bucket)
	assert.NoError(t, err)

	// test invalid stack template data
	err = awsAdapter.applyClusterStack("stack-name", []byte(`{"stack": "template"`), cluster, s3Bucket)
	assert.Error(t, err)

	templateValue := make([]string, stackMaxSize+1)
	for i := range templateValue {
		templateValue[i] = "x"
	}
	hugeTemplate := []byte(fmt.Sprintf("{\"stack\": \"%s\"}", strings.Join(templateValue, "")))

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
	err = awsAdapter.applyClusterStack("stack-name", []byte(`{"stack": "template"}`), cluster, s3Bucket)
	assert.NoError(t, err)

	// test create failing
	awsAdapter.cloudformationClient = &cloudFormationAPIStub{
		statusMutex: &sync.Mutex{},
		createErr:   errors.New("error"),
	}
	err = awsAdapter.applyClusterStack("stack-name", []byte(`{"stack": "template"}`), cluster, s3Bucket)
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
	err = awsAdapter.applyClusterStack("stack-name", []byte(`{"stack": "template"}`), cluster, s3Bucket)
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
	err = awsAdapter.applyClusterStack("stack-name", []byte(`{"stack": "template"}`), cluster, s3Bucket)
	assert.Error(t, err)
}
