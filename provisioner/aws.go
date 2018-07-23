package provisioner

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os/exec"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/coreos/container-linux-config-transpiler/config"
	"github.com/coreos/container-linux-config-transpiler/config/platform"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/kube-ingress-aws-controller/certs"
	"golang.org/x/oauth2"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/acm"
	"github.com/aws/aws-sdk-go/service/acm/acmiface"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

const (
	waitTime                     = 15 * time.Second
	stackMaxSize                 = 51200
	cloudformationValidationErr  = "ValidationError"
	cloudformationNoUpdateMsg    = "No updates are to be performed."
	clmCFBucketPattern           = "cluster-lifecycle-manager-%s-%s"
	lifecycleStatusReady         = "ready"
	etcdInstanceTypeKey          = "etcd_instance_type"
	etcdS3BackupBucketKey        = "etcd_s3_backup_bucket"
	discountStrategyNone         = "none"
	discountStrategySpotMaxPrice = "spot_max_price"
	ignitionBaseTemplate         = `{
  "ignition": {
    "version": "2.1.0",
    "config": {
      "replace": {
        "source": "%s"
      }
    }
  }
}`
)

var (
	maxWaitTimeout            = 15 * time.Minute
	errCreateFailed           = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusCreateFailed)
	errRollbackComplete       = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusRollbackComplete)
	errUpdateRollbackComplete = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusUpdateRollbackComplete)
	errRollbackFailed         = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusRollbackFailed)
	errUpdateRollbackFailed   = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusUpdateRollbackFailed)
	errDeleteFailed           = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusDeleteFailed)
	errTimeoutExceeded        = fmt.Errorf("wait for stack timeout exceeded")
)

// cloudFormationAPI is a minimal interface containing only the methods we use from the AWS SDK for cloudformation
type cloudFormationAPI interface {
	DescribeStacks(input *cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error)
	CreateStack(input *cloudformation.CreateStackInput) (*cloudformation.CreateStackOutput, error)
	UpdateStack(input *cloudformation.UpdateStackInput) (*cloudformation.UpdateStackOutput, error)
	DeleteStack(input *cloudformation.DeleteStackInput) (*cloudformation.DeleteStackOutput, error)
	UpdateTerminationProtection(intput *cloudformation.UpdateTerminationProtectionInput) (*cloudformation.UpdateTerminationProtectionOutput, error)
	DescribeStacksPages(input *cloudformation.DescribeStacksInput, fn func(resp *cloudformation.DescribeStacksOutput, lastPage bool) bool) error
}

// s3API is a minimal interface containing only the methods we use from the S3 API
type s3API interface {
	CreateBucket(input *s3.CreateBucketInput) (*s3.CreateBucketOutput, error)
}

type autoscalingAPI interface {
	DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
	DescribeLaunchConfigurations(input *autoscaling.DescribeLaunchConfigurationsInput) (*autoscaling.DescribeLaunchConfigurationsOutput, error)
	UpdateAutoScalingGroup(input *autoscaling.UpdateAutoScalingGroupInput) (*autoscaling.UpdateAutoScalingGroupOutput, error)
	SuspendProcesses(input *autoscaling.ScalingProcessQuery) (*autoscaling.SuspendProcessesOutput, error)
	ResumeProcesses(*autoscaling.ScalingProcessQuery) (*autoscaling.ResumeProcessesOutput, error)
	TerminateInstanceInAutoScalingGroup(*autoscaling.TerminateInstanceInAutoScalingGroupInput) (*autoscaling.TerminateInstanceInAutoScalingGroupOutput, error)
}

type iamAPI interface {
	ListAccountAliases(input *iam.ListAccountAliasesInput) (*iam.ListAccountAliasesOutput, error)
}

type ec2API interface {
	DescribeInstanceAttribute(input *ec2.DescribeInstanceAttributeInput) (*ec2.DescribeInstanceAttributeOutput, error)
	DescribeSpotInstanceRequests(input *ec2.DescribeSpotInstanceRequestsInput) (*ec2.DescribeSpotInstanceRequestsOutput, error)
	DescribeVpcs(input *ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error)
	DescribeVolumes(input *ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error)
	DescribeSubnets(input *ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error)

	CreateTags(input *ec2.CreateTagsInput) (*ec2.CreateTagsOutput, error)
	DeleteTags(input *ec2.DeleteTagsInput) (*ec2.DeleteTagsOutput, error)

	DeleteVolume(input *ec2.DeleteVolumeInput) (*ec2.DeleteVolumeOutput, error)
}

type s3UploaderAPI interface {
	Upload(input *s3manager.UploadInput, options ...func(*s3manager.Uploader)) (*s3manager.UploadOutput, error)
}

type awsAdapter struct {
	session              *session.Session
	cloudformationClient cloudFormationAPI
	s3Client             s3API
	s3Uploader           s3UploaderAPI
	autoscalingClient    autoscalingAPI
	iamClient            iamAPI
	ec2Client            ec2API
	acmClient            acmiface.ACMAPI
	region               string
	apiServer            string
	tokenSrc             oauth2.TokenSource
	dryRun               bool
	logger               *log.Entry
}

// newAWSAdapter initializes a new awsAdapter.
func newAWSAdapter(logger *log.Entry, apiServer string, region string, sess *session.Session, tokenSrc oauth2.TokenSource, dryRun bool) (*awsAdapter, error) {
	return &awsAdapter{
		session:              sess,
		cloudformationClient: cloudformation.New(sess),
		iamClient:            iam.New(sess),
		s3Client:             s3.New(sess),
		s3Uploader:           s3manager.NewUploader(sess),
		autoscalingClient:    autoscaling.New(sess),
		ec2Client:            ec2.New(sess),
		acmClient:            acm.New(sess),
		region:               region,
		apiServer:            apiServer,
		tokenSrc:             tokenSrc,
		dryRun:               dryRun,
		logger:               logger,
	}, nil
}

// encodeUserData gzip compresses and base64 encodes a userData string.
func encodeUserData(userData string) (string, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write([]byte(userData))
	if err != nil {
		return "", err
	}
	err = gz.Close()
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// decodeUserData decodes base64 encoded + gzip compressed data.
func decodeUserData(encodedUserData string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(encodedUserData)
	if err != nil {
		return "", err
	}

	gz, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return "", err
	}
	defer gz.Close()

	data, err := ioutil.ReadAll(gz)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// CreateOrUpdateClusterStackSenza creates or updates a cluster cloudformation
// stack. This function is idempotent.
func (a *awsAdapter) CreateOrUpdateClusterStackSenza(parentCtx context.Context, stackName, stackDefinitionPath string, cluster *api.Cluster) error {
	name, version, err := splitStackName(stackName)
	if err != nil {
		return err
	}

	// create bucket name with aws account ID to ensure uniqueness across
	// accounts.
	s3BucketName := fmt.Sprintf(clmCFBucketPattern, strings.TrimPrefix(cluster.InfrastructureAccount, "aws:"), cluster.Region)

	hostedZone, err := getHostedZone(cluster.APIServerURL)
	if err != nil {
		return err
	}

	args := []string{
		"print",
		stackDefinitionPath,
		version,
		"KmsKey=*",
		fmt.Sprintf("StackName=%s", name),
		fmt.Sprintf("HostedZone=%s", hostedZone),
		fmt.Sprintf("ClusterID=%s", cluster.ID),
	}

	if bucket, ok := cluster.ConfigItems[etcdS3BackupBucketKey]; ok {
		args = append(args, fmt.Sprintf("EtcdS3BackupBucket=%s", bucket))
	}

	cmd := exec.Command("senza", args...)

	if a.dryRun {
		cmd.Args = append(cmd.Args, "--dry-run")
	}

	enVars, err := a.getEnvVars()
	if err != nil {
		return err
	}

	cmd.Env = enVars

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%v: %s", err, string(exitErr.Stderr))
		}
		return err
	}

	var stackBuffer bytes.Buffer
	// save as many bytes as possible
	err = json.Compact(&stackBuffer, output)
	if err != nil {
		return err
	}

	err = a.applyClusterStack(stackName, stackBuffer.String(), cluster, s3BucketName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(parentCtx, maxWaitTimeout)
	defer cancel()
	err = a.waitForStack(ctx, waitTime, stackName)
	if err != nil {
		return err
	}

	return nil
}

// applyClusterStack creates or updates a stack specified by stackName and
// stackTemplate.
// If the stackTemplate exceeds the max size, it will automatically upload it
// to S3 before creating or updating the stack.
func (a *awsAdapter) applyClusterStack(stackName, stackTemplate string, cluster *api.Cluster, s3BucketName string) error {
	var templateURL string
	if len(stackTemplate) > stackMaxSize {
		// create S3 bucket if it doesn't exist
		err := a.createS3Bucket(s3BucketName)
		if err != nil {
			return err
		}

		// Upload the stack template to S3
		result, err := a.s3Uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(s3BucketName),
			Key:    aws.String(fmt.Sprintf("%s.template", cluster.ID)),
			Body:   strings.NewReader(stackTemplate),
		})
		if err != nil {
			return err
		}
		templateURL = result.Location
	}

	return a.applyStack(stackName, stackTemplate, templateURL, nil, true)
}

// applyStack applies a cloudformation stack.
func (a *awsAdapter) applyStack(stackName string, stackTemplate string, stackTemplateURL string, tags []*cloudformation.Tag, updateStack bool) error {
	createParams := &cloudformation.CreateStackInput{
		StackName:                   aws.String(stackName),
		OnFailure:                   aws.String(cloudformation.OnFailureDelete),
		Capabilities:                []*string{aws.String(cloudformation.CapabilityCapabilityNamedIam)},
		EnableTerminationProtection: aws.Bool(true),
		Tags: tags,
	}

	if stackTemplateURL != "" {
		createParams.TemplateURL = aws.String(stackTemplateURL)
	} else {
		createParams.TemplateBody = aws.String(stackTemplate)
	}

	_, err := a.cloudformationClient.CreateStack(createParams)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case cloudformation.ErrCodeAlreadyExistsException:
				// if create failed because the stack already
				// exists, update instead

				// ensure stack termination protection is enabled.
				terminationParams := &cloudformation.UpdateTerminationProtectionInput{
					StackName:                   aws.String(stackName),
					EnableTerminationProtection: aws.Bool(true),
				}

				_, err := a.cloudformationClient.UpdateTerminationProtection(terminationParams)
				if err != nil {
					return err
				}

				if updateStack {
					// update the stack
					updateParams := &cloudformation.UpdateStackInput{
						StackName:    createParams.StackName,
						Capabilities: createParams.Capabilities,
						Tags:         tags,
					}

					if stackTemplateURL != "" {
						updateParams.TemplateURL = aws.String(stackTemplateURL)
					} else {
						updateParams.TemplateBody = aws.String(stackTemplate)
					}

					_, err = a.cloudformationClient.UpdateStack(updateParams)
					if err != nil {
						if aerr, ok := err.(awserr.Error); ok {
							// if no update was needed
							// treat it as success
							if aerr.Code() == cloudformationValidationErr && aerr.Message() == cloudformationNoUpdateMsg {
								return nil
							}
						}
						return err
					}
				}
				return nil
			}
		}
		return err
	}

	return nil
}

func (a *awsAdapter) getStackByName(stackName string) (*cloudformation.Stack, error) {
	params := &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	}
	resp, err := a.cloudformationClient.DescribeStacks(params)
	if err != nil {
		return nil, err
	}
	//we expect only one stack
	if len(resp.Stacks) != 1 {
		return nil, fmt.Errorf("unexpected response, got %d, expected 1 stack", len(resp.Stacks))
	}
	return resp.Stacks[0], nil
}

func (a *awsAdapter) waitForStack(ctx context.Context, waitTime time.Duration, stackName string) error {
	for {
		stack, err := a.getStackByName(stackName)
		if err != nil {
			return err
		}
		switch *stack.StackStatus {
		case cloudformation.StackStatusUpdateComplete:
			return nil
		case cloudformation.StackStatusCreateComplete:
			return nil
		case cloudformation.StackStatusDeleteComplete:
			return nil
		case cloudformation.StackStatusCreateFailed:
			return errCreateFailed
		case cloudformation.StackStatusDeleteFailed:
			return errDeleteFailed
		case cloudformation.StackStatusRollbackComplete:
			return errRollbackComplete
		case cloudformation.StackStatusRollbackFailed:
			return errRollbackFailed
		case cloudformation.StackStatusUpdateRollbackComplete:
			return errUpdateRollbackComplete
		case cloudformation.StackStatusUpdateRollbackFailed:
			return errUpdateRollbackFailed
		}
		a.logger.Debugf("Stack '%s' - [%s]", stackName, *stack.StackStatus)

		select {
		case <-ctx.Done():
			return errTimeoutExceeded
		case <-time.After(waitTime):
		}
	}
}

// ListStacks lists stacks filtered by tags.
func (a *awsAdapter) ListStacks(tags map[string]string) ([]*cloudformation.Stack, error) {
	params := &cloudformation.DescribeStacksInput{}

	stacks := make([]*cloudformation.Stack, 0)
	err := a.cloudformationClient.DescribeStacksPages(params, func(resp *cloudformation.DescribeStacksOutput, lastPage bool) bool {
		for _, stack := range resp.Stacks {
			if cloudformationHasTags(tags, stack.Tags) {
				stacks = append(stacks, stack)
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	return stacks, nil
}

// cloudformationHasTags returns true if the expected tags are found in the
// tags list.
func cloudformationHasTags(expected map[string]string, tags []*cloudformation.Tag) bool {
	if len(expected) > len(tags) {
		return false
	}

	tagsMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		tagsMap[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
	}

	for key, val := range expected {
		if v, ok := tagsMap[key]; !ok || v != val {
			return false
		}
	}

	return true

}

// DeleteStack deletes a cloudformation stack.
func (a *awsAdapter) DeleteStack(parentCtx context.Context, stackName string) error {
	a.logger.Infof("Deleting stack '%s'", stackName)

	// disable termination protection on stack before deleting
	terminationParams := &cloudformation.UpdateTerminationProtectionInput{
		StackName:                   aws.String(stackName),
		EnableTerminationProtection: aws.Bool(false),
	}

	_, err := a.cloudformationClient.UpdateTerminationProtection(terminationParams)
	if err != nil {
		if isDoesNotExistsErr(err) {
			return nil
		}
		return err
	}

	deleteParams := &cloudformation.DeleteStackInput{
		StackName: aws.String(stackName),
	}

	_, err = a.cloudformationClient.DeleteStack(deleteParams)
	if err != nil {
		if isDoesNotExistsErr(err) {
			return nil
		}
		return err
	}

	ctx, cancel := context.WithTimeout(parentCtx, maxWaitTimeout)
	defer cancel()
	err = a.waitForStack(ctx, waitTime, stackName)
	if err != nil {
		if isDoesNotExistsErr(err) {
			return nil
		}
		return err
	}
	return nil
}

// CreateOrUpdateEtcdStack creates or updates an etcd stack.
func (a *awsAdapter) CreateOrUpdateEtcdStack(parentCtx context.Context, stackName string, stackDefinitionPath string, cluster *api.Cluster) error {
	bucketName := fmt.Sprintf("zalando-kubernetes-etcd-%s-%s", getAWSAccountID(cluster.InfrastructureAccount), cluster.Region)

	if bucket, ok := cluster.ConfigItems[etcdS3BackupBucketKey]; ok {
		bucketName = bucket
	}

	hostedZone, err := getHostedZone(cluster.APIServerURL)
	if err != nil {
		return err
	}

	args := []string{
		"print",
		stackDefinitionPath,
		"etcd",
		fmt.Sprintf("HostedZone=%s", hostedZone),
		fmt.Sprintf("EtcdS3Backup=%s", bucketName),
	}

	if instanceType, ok := cluster.ConfigItems[etcdInstanceTypeKey]; ok {
		args = append(args, fmt.Sprintf("InstanceType=%s", instanceType))
	}

	cmd := exec.Command(
		"senza",
		args...,
	)

	if a.dryRun {
		cmd.Args = append(cmd.Args, "--dry-run")
	}

	enVars, err := a.getEnvVars()
	if err != nil {
		return err
	}

	cmd.Env = enVars

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%v: %s", err, string(exitErr.Stderr))
		}
		return err
	}

	err = a.applyStack(stackName, string(output), "", nil, false)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(parentCtx, maxWaitTimeout)
	defer cancel()
	err = a.waitForStack(ctx, waitTime, stackName)
	if err != nil {
		return err
	}

	return nil
}

// createS3Bucket creates an s3 bucket if it doesn't exist.
func (a *awsAdapter) createS3Bucket(bucket string) error {
	params := &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
		CreateBucketConfiguration: &s3.CreateBucketConfiguration{
			LocationConstraint: aws.String(a.region),
		},
	}

	return backoff.Retry(
		func() error {
			_, err := a.s3Client.CreateBucket(params)
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					// if the bucket already exists and is owned by us, we
					// don't treat it as an error.
					case s3.ErrCodeBucketAlreadyOwnedByYou:
						return nil
					}
				}
			}
			return err
		},
		backoff.WithMaxTries(backoff.NewExponentialBackOff(), 10))
}

func clcToIgnition(data []byte) ([]byte, error) {
	cfg, ast, report := config.Parse(data)
	if len(report.Entries) > 0 {
		return nil, errors.New(report.String())
	}

	ignCfg, report := config.Convert(cfg, platform.EC2, ast)
	if len(report.Entries) > 0 {
		return nil, fmt.Errorf("failed to convert to ignition: %s", report.String())
	}

	return json.Marshal(&ignCfg)
}

// getEnvVars gets AWS credentials from the session and returns the
// corresponding environment variables.
// only used for senza (TODO:think about not storing session in the awsAdapter)
func (a *awsAdapter) getEnvVars() ([]string, error) {
	creds, err := a.session.Config.Credentials.Get()
	if err != nil {
		return nil, err
	}

	return []string{
		"AWS_ACCESS_KEY_ID=" + creds.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + creds.SecretAccessKey,
		"AWS_SESSION_TOKEN=" + creds.SessionToken,
		"AWS_DEFAULT_REGION=" + *a.session.Config.Region,
		"LC_ALL=en_US.UTF-8",
		"LANG=en_US.UTF-8",
		"PATH=/usr/local/bin:/usr/bin:/bin",
	}, nil
}

// asgHasTags returns true if the asg tags matches the expected tags.
// autoscaling tag keys are unique
func asgHasTags(expected, tags []*autoscaling.TagDescription) bool {
	if len(expected) > len(tags) {
		return false
	}

	matching := 0

	for _, e := range expected {
		for _, tag := range tags {
			if *e.Key == *tag.Key && *e.Value == *tag.Value {
				matching++
			}
		}
	}

	return matching == len(expected)
}

func (a *awsAdapter) GetVolumes(tags map[string]string) ([]*ec2.Volume, error) {
	var filters []*ec2.Filter

	for tagKey, tagValue := range tags {
		filters = append(filters, &ec2.Filter{
			Name:   aws.String(fmt.Sprintf("tag:%s", tagKey)),
			Values: []*string{aws.String(tagValue)},
		})
	}

	result, err := a.ec2Client.DescribeVolumes(&ec2.DescribeVolumesInput{Filters: filters})
	if err != nil {
		return nil, err
	}
	return result.Volumes, nil
}

func (a *awsAdapter) DeleteVolume(id string) error {
	_, err := a.ec2Client.DeleteVolume(&ec2.DeleteVolumeInput{
		VolumeId: aws.String(id),
	})
	return err
}

// GetCertificates gets all available 'ISSUED' certificates from ACM.
func (a *awsAdapter) GetCertificates() ([]*certs.CertificateSummary, error) {
	params := &acm.ListCertificatesInput{
		CertificateStatuses: []*string{
			aws.String(acm.CertificateStatusIssued),
		},
	}
	acmSummaries := make([]*acm.CertificateSummary, 0)
	err := a.acmClient.ListCertificatesPages(params, func(page *acm.ListCertificatesOutput, lastPage bool) bool {
		for _, cert := range page.CertificateSummaryList {
			acmSummaries = append(acmSummaries, cert)
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	result := make([]*certs.CertificateSummary, 0)
	for _, o := range acmSummaries {
		summary, err := a.getCertificateSummaryFromACM(o.CertificateArn)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, nil
}

func (a *awsAdapter) getCertificateSummaryFromACM(arn *string) (*certs.CertificateSummary, error) {
	params := &acm.DescribeCertificateInput{CertificateArn: arn}
	resp, err := a.acmClient.DescribeCertificate(params)
	if err != nil {
		return nil, err
	}
	return certs.NewCertificate(
		aws.StringValue(resp.Certificate.CertificateArn),
		append(aws.StringValueSlice(resp.Certificate.SubjectAlternativeNames), aws.StringValue(resp.Certificate.DomainName)),
		aws.TimeValue(resp.Certificate.NotBefore),
		aws.TimeValue(resp.Certificate.NotAfter)), nil
}

// GetDefaultVPC gets the default VPC.
func (a *awsAdapter) GetDefaultVPC() (*ec2.Vpc, error) {
	// find default VPC
	vpcResp, err := a.ec2Client.DescribeVpcs(&ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, err
	}

	var defaultVpc *ec2.Vpc
	for _, vpc := range vpcResp.Vpcs {
		if aws.BoolValue(vpc.IsDefault) {
			defaultVpc = vpc
			break
		}
	}

	if defaultVpc == nil {
		return nil, fmt.Errorf("default VPC not found in account")
	}

	return defaultVpc, nil
}

// GetVPC gets VPC details for vpc specified by vpcID.
func (a *awsAdapter) GetVPC(vpcID string) (*ec2.Vpc, error) {
	// find default VPC
	vpcResp, err := a.ec2Client.DescribeVpcs(&ec2.DescribeVpcsInput{
		VpcIds: []*string{aws.String(vpcID)},
	})
	if err != nil {
		return nil, err
	}

	if len(vpcResp.Vpcs) != 1 {
		return nil, fmt.Errorf("found %d VPC for VPCID %s, expected 1", len(vpcResp.Vpcs), vpcID)
	}

	return vpcResp.Vpcs[0], nil
}

// GetSubnets gets all subnets of the default VPC in the target account.
func (a *awsAdapter) GetSubnets(vpcID string) ([]*ec2.Subnet, error) {
	subnetParams := &ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{aws.String(vpcID)},
			},
		},
	}

	subnetResp, err := a.ec2Client.DescribeSubnets(subnetParams)
	if err != nil {
		return nil, err
	}

	return subnetResp.Subnets, nil
}

// CreateTags adds or updates tags of a resource.
func (a *awsAdapter) CreateTags(resource string, tags []*ec2.Tag) error {
	params := &ec2.CreateTagsInput{
		Resources: []*string{aws.String(resource)},
		Tags:      tags,
	}

	_, err := a.ec2Client.CreateTags(params)
	return err
}

// DeleteTags deletes tags from a resource.
func (a *awsAdapter) DeleteTags(resource string, tags []*ec2.Tag) error {
	params := &ec2.DeleteTagsInput{
		Resources: []*string{aws.String(resource)},
		Tags:      tags,
	}

	_, err := a.ec2Client.DeleteTags(params)
	return err
}

func isDoesNotExistsErr(err error) bool {
	if awsErr, ok := err.(awserr.Error); ok {
		if awsErr.Code() == "ValidationError" && strings.Contains(awsErr.Message(), "does not exist") {
			//we wanted to delete a stack and it does not exist (or was removed while we were waiting, we can hide the error)
			return true
		}
	}
	return false
}

// isWrongStackStatusErr returns true if the error is of type awserr.Error and
// describes a failure because of wrong Cloudformation stack status.
func isWrongStackStatusErr(err error) bool {
	if awsErr, ok := err.(awserr.Error); ok {
		if awsErr.Code() == "ValidationError" && strings.Contains(awsErr.Message(), "cannot be deleted while in status") {
			return true
		}
	}
	return false
}

// tagsToMap converts a list of ec2 tags to a map.
func tagsToMap(tags []*ec2.Tag) map[string]string {
	tagMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		tagMap[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
	}
	return tagMap
}
