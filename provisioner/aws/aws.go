package aws

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/acm"
	"github.com/aws/aws-sdk-go/service/acm/acmiface"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/kms/kmsiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/cenkalti/backoff"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	awsutil "github.com/zalando-incubator/kube-ingress-aws-controller/aws"
	"github.com/zalando-incubator/kube-ingress-aws-controller/certs"
)

const (
	waitTime                    = 15 * time.Second
	maxWaitTimeout              = 15 * time.Minute
	stackMaxSize                = 51200
	cloudformationValidationErr = "ValidationError"
	cloudformationNoUpdateMsg   = "No updates are to be performed."
	CLMCFBucketPattern          = "cluster-lifecycle-manager-%s-%s"
	ResourceLifecycleShared     = "shared"
	ResourceLifecycleOwned      = "owned"
	KubernetesClusterTagPrefix  = "kubernetes.io/cluster/"
)

var (
	errCreateFailed           = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusCreateFailed)
	errRollbackComplete       = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusRollbackComplete)
	errUpdateRollbackComplete = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusUpdateRollbackComplete)
	errRollbackFailed         = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusRollbackFailed)
	errUpdateRollbackFailed   = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusUpdateRollbackFailed)
	errDeleteFailed           = fmt.Errorf("wait for stack failed with %s", cloudformation.StackStatusDeleteFailed)
	errTimeoutExceeded        = fmt.Errorf("wait for stack timeout exceeded")
)

// s3API is a minimal interface containing only the methods we use from the S3 API
type s3API interface {
	CreateBucket(input *s3.CreateBucketInput) (*s3.CreateBucketOutput, error)
}

type autoscalingAPI interface {
	DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
	UpdateAutoScalingGroup(input *autoscaling.UpdateAutoScalingGroupInput) (*autoscaling.UpdateAutoScalingGroupOutput, error)
	SuspendProcesses(input *autoscaling.ScalingProcessQuery) (*autoscaling.SuspendProcessesOutput, error)
	ResumeProcesses(*autoscaling.ScalingProcessQuery) (*autoscaling.ResumeProcessesOutput, error)
	TerminateInstanceInAutoScalingGroup(*autoscaling.TerminateInstanceInAutoScalingGroupInput) (*autoscaling.TerminateInstanceInAutoScalingGroupOutput, error)
}

type iamAPI interface {
	ListAccountAliases(input *iam.ListAccountAliasesInput) (*iam.ListAccountAliasesOutput, error)
}

type s3UploaderAPI interface {
	Upload(input *s3manager.UploadInput, options ...func(*s3manager.Uploader)) (*s3manager.UploadOutput, error)
}

type AWSAdapter struct {
	Session              *session.Session
	CloudformationClient cloudformationiface.CloudFormationAPI
	s3Client             s3API
	S3Uploader           s3UploaderAPI
	autoscalingClient    autoscalingAPI
	iamClient            iamAPI
	EC2Client            ec2iface.EC2API
	acmClient            acmiface.ACMAPI
	region               string
	logger               *log.Entry
	KMSClient            kmsiface.KMSAPI
}

// NewAWSAdapter initializes a new AWSAdapter.
func NewAWSAdapter(logger *log.Entry, region string, sess *session.Session) (*AWSAdapter, error) {
	return &AWSAdapter{
		Session:              sess,
		CloudformationClient: cloudformation.New(sess),
		iamClient:            iam.New(sess),
		s3Client:             s3.New(sess),
		S3Uploader:           s3manager.NewUploader(sess),
		autoscalingClient:    autoscaling.New(sess),
		EC2Client:            ec2.New(sess),
		acmClient:            acm.New(sess),
		region:               region,
		logger:               logger,
		KMSClient:            kms.New(sess),
	}, nil
}

func (a *AWSAdapter) VerifyAccount(accountID string) error {
	stsService := sts.New(a.Session)
	response, err := stsService.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return err
	}
	effectiveAccount := aws.StringValue(response.Account)
	if effectiveAccount != accountID {
		return fmt.Errorf("invalid AWS account, expected %s, found %s", accountID, effectiveAccount)
	}
	return nil
}

// ApplyClusterStack creates or updates a stack specified by stackName and
// stackTemplate.
// If the stackTemplate exceeds the max size, it will automatically upload it
// to S3 before creating or updating the stack.
func (a *AWSAdapter) ApplyClusterStack(stackName, stackTemplate string, cluster *api.Cluster, s3BucketName string, tags []*cloudformation.Tag) error {
	var templateURL string
	if len(stackTemplate) > stackMaxSize {
		// create S3 bucket if it doesn't exist
		err := a.CreateS3Bucket(s3BucketName)
		if err != nil {
			return err
		}

		// Upload the stack template to S3
		result, err := a.S3Uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(s3BucketName),
			Key:    aws.String(fmt.Sprintf("%s.template", cluster.ID)),
			Body:   strings.NewReader(stackTemplate),
		})
		if err != nil {
			return err
		}
		templateURL = result.Location
	}

	return a.ApplyStack(stackName, stackTemplate, templateURL, tags, true)
}

// ApplyStack applies a cloudformation stack.
func (a *AWSAdapter) ApplyStack(stackName string, stackTemplate string, stackTemplateURL string, tags []*cloudformation.Tag, updateStack bool) error {
	createParams := &cloudformation.CreateStackInput{
		StackName:                   aws.String(stackName),
		OnFailure:                   aws.String(cloudformation.OnFailureDelete),
		Capabilities:                []*string{aws.String(cloudformation.CapabilityCapabilityNamedIam)},
		EnableTerminationProtection: aws.Bool(true),
		Tags:                        tags,
	}

	if stackTemplateURL != "" {
		createParams.TemplateURL = aws.String(stackTemplateURL)
	} else {
		createParams.TemplateBody = aws.String(stackTemplate)
	}

	_, err := a.CloudformationClient.CreateStack(createParams)
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

				_, err := a.CloudformationClient.UpdateTerminationProtection(terminationParams)
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

					_, err = a.CloudformationClient.UpdateStack(updateParams)
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

func (a *AWSAdapter) GetStackByName(stackName string) (*cloudformation.Stack, error) {
	params := &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	}
	resp, err := a.CloudformationClient.DescribeStacks(params)
	if err != nil {
		return nil, err
	}
	//we expect only one stack
	if len(resp.Stacks) != 1 {
		return nil, fmt.Errorf("unexpected response, got %d, expected 1 stack", len(resp.Stacks))
	}
	return resp.Stacks[0], nil
}

// WaitForStack waits for a CloudFormation stack to be ready.
func (a *AWSAdapter) WaitForStack(ctx context.Context, waitTime time.Duration, stackName string) error {
	for {
		stack, err := a.GetStackByName(stackName)
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
			err = errCreateFailed
		case cloudformation.StackStatusDeleteFailed:
			err = errDeleteFailed
		case cloudformation.StackStatusRollbackComplete:
			err = errRollbackComplete
		case cloudformation.StackStatusRollbackFailed:
			err = errRollbackFailed
		case cloudformation.StackStatusUpdateRollbackComplete:
			err = errUpdateRollbackComplete
		case cloudformation.StackStatusUpdateRollbackFailed:
			err = errUpdateRollbackFailed
		}
		if err != nil {
			if stack.StackStatusReason != nil {
				return fmt.Errorf("%v, reason: %v", err, *stack.StackStatusReason)
			}
			return err
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
func (a *AWSAdapter) ListStacks(includeTags, excludeTags map[string]string) ([]*cloudformation.Stack, error) {
	params := &cloudformation.DescribeStacksInput{}

	stacks := make([]*cloudformation.Stack, 0)
	err := a.CloudformationClient.DescribeStacksPages(params, func(resp *cloudformation.DescribeStacksOutput, lastPage bool) bool {
		for _, stack := range resp.Stacks {
			if cloudformationHasTags(includeTags, stack.Tags) && cloudformationDoesNotHaveTags(excludeTags, stack.Tags) {
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

// cloudformationDoesNotHaveTags returns true if the excluded tags are not
// found in the tags list.
func cloudformationDoesNotHaveTags(excluded map[string]string, tags []*cloudformation.Tag) bool {
	tagsMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		tagsMap[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
	}

	for key, val := range excluded {
		if v, ok := tagsMap[key]; ok && v == val {
			return false
		}
	}

	return true
}

func isStackDeleting(stack *cloudformation.Stack) bool {
	switch aws.StringValue(stack.StackStatus) {
	case cloudformation.StackStatusDeleteInProgress,
		cloudformation.StackStatusDeleteComplete:
		return true
	default:
		return false
	}
}

// DeleteStack deletes a cloudformation stack.
func (a *AWSAdapter) DeleteStack(parentCtx context.Context, stack *cloudformation.Stack) error {
	stackName := aws.StringValue(stack.StackName)
	a.logger.Infof("Deleting stack '%s'", stackName)

	if !isStackDeleting(stack) {
		// disable termination protection on stack before deleting
		terminationParams := &cloudformation.UpdateTerminationProtectionInput{
			StackName:                   aws.String(stackName),
			EnableTerminationProtection: aws.Bool(false),
		}

		_, err := a.CloudformationClient.UpdateTerminationProtection(terminationParams)
		if err != nil {
			if isDoesNotExistsErr(err) {
				return nil
			}
			return err
		}

		deleteParams := &cloudformation.DeleteStackInput{
			StackName: aws.String(stackName),
		}

		_, err = a.CloudformationClient.DeleteStack(deleteParams)
		if err != nil {
			if isDoesNotExistsErr(err) {
				return nil
			}
			return err
		}
	}

	ctx, cancel := context.WithTimeout(parentCtx, maxWaitTimeout)
	defer cancel()
	err := a.WaitForStack(ctx, waitTime, stackName)
	if err != nil {
		if isDoesNotExistsErr(err) {
			return nil
		}
		return err
	}
	return nil
}

// ResolveKMSKeyID resolved a local key ID (e.g. alias/etcd-cluster) into the ARN
func (a *AWSAdapter) ResolveKMSKeyID(keyID string) (string, error) {
	output, err := a.KMSClient.DescribeKey(&kms.DescribeKeyInput{
		KeyId: aws.String(keyID),
	})
	if err != nil {
		return "", err
	}

	return aws.StringValue(output.KeyMetadata.Arn), nil
}

// KMSEncryptForTaupage encrypts a string using a Taupage-compatible format (aws:kms:…)
func (a *AWSAdapter) KMSEncryptForTaupage(keyID string, value string) (string, error) {
	output, err := a.KMSClient.Encrypt(&kms.EncryptInput{
		KeyId:     aws.String(keyID),
		Plaintext: []byte(value),
	})
	if err != nil {
		return "", err
	}
	return "aws:kms:" + base64.StdEncoding.EncodeToString(output.CiphertextBlob), nil
}

// createS3Bucket creates an s3 bucket if it doesn't exist.
func (a *AWSAdapter) CreateS3Bucket(bucket string) error {
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
		backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 10))
}

// GetEnvVars gets AWS credentials from the session and returns the
// corresponding environment variables.
// only used for senza (TODO:think about not storing session in the AWSAdapter)
func (a *AWSAdapter) GetEnvVars() ([]string, error) {
	creds, err := a.Session.Config.Credentials.Get()
	if err != nil {
		return nil, err
	}

	return []string{
		"AWS_ACCESS_KEY_ID=" + creds.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + creds.SecretAccessKey,
		"AWS_SESSION_TOKEN=" + creds.SessionToken,
		"AWS_DEFAULT_REGION=" + *a.Session.Config.Region,
		"LC_ALL=en_US.UTF-8",
		"LANG=en_US.UTF-8",
		"PATH=/usr/local/bin:/usr/bin:/bin",
	}, nil
}

func (a *AWSAdapter) GetVolumes(tags map[string]string) ([]*ec2.Volume, error) {
	var filters []*ec2.Filter

	for tagKey, tagValue := range tags {
		filters = append(filters, &ec2.Filter{
			Name:   aws.String(fmt.Sprintf("tag:%s", tagKey)),
			Values: []*string{aws.String(tagValue)},
		})
	}

	result, err := a.EC2Client.DescribeVolumes(&ec2.DescribeVolumesInput{Filters: filters})
	if err != nil {
		return nil, err
	}
	return result.Volumes, nil
}

func (a *AWSAdapter) DeleteVolume(id string) error {
	_, err := a.EC2Client.DeleteVolume(&ec2.DeleteVolumeInput{
		VolumeId: aws.String(id),
	})
	return err
}

// GetCertificates gets all available 'ISSUED' certificates from ACM.
func (a *AWSAdapter) GetCertificates() ([]*certs.CertificateSummary, error) {
	params := &acm.ListCertificatesInput{
		CertificateStatuses: []*string{
			aws.String(acm.CertificateStatusIssued),
		},
	}
	acmSummaries := make([]*acm.CertificateSummary, 0)
	err := a.acmClient.ListCertificatesPages(params, func(page *acm.ListCertificatesOutput, lastPage bool) bool {
		acmSummaries = append(acmSummaries, page.CertificateSummaryList...)
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

func (a *AWSAdapter) getCertificateSummaryFromACM(arn *string) (*certs.CertificateSummary, error) {
	params := &acm.GetCertificateInput{CertificateArn: arn}
	resp, err := a.acmClient.GetCertificate(params)
	if err != nil {
		return nil, err
	}

	cert, err := awsutil.ParseCertificate(aws.StringValue(resp.Certificate))
	if err != nil {
		return nil, err
	}

	var chain []*x509.Certificate
	if resp.CertificateChain != nil {
		chain, err = awsutil.ParseCertificates(aws.StringValue(resp.CertificateChain))
		if err != nil {
			return nil, err
		}
	}

	return certs.NewCertificate(aws.StringValue(arn), cert, chain), nil
}

// GetDefaultVPC gets the default VPC.
func (a *AWSAdapter) GetDefaultVPC() (*ec2.Vpc, error) {
	// find default VPC
	vpcResp, err := a.EC2Client.DescribeVpcs(&ec2.DescribeVpcsInput{})
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
func (a *AWSAdapter) GetVPC(vpcID string) (*ec2.Vpc, error) {
	// find default VPC
	vpcResp, err := a.EC2Client.DescribeVpcs(&ec2.DescribeVpcsInput{
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
func (a *AWSAdapter) GetSubnets(vpcID string) ([]*ec2.Subnet, error) {
	subnetParams := &ec2.DescribeSubnetsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []*string{aws.String(vpcID)},
			},
		},
	}

	subnetResp, err := a.EC2Client.DescribeSubnets(subnetParams)
	if err != nil {
		return nil, err
	}

	return subnetResp.Subnets, nil
}

// CreateTags adds or updates tags of a resource.
func (a *AWSAdapter) CreateTags(resource string, tags []*ec2.Tag) error {
	params := &ec2.CreateTagsInput{
		Resources: []*string{aws.String(resource)},
		Tags:      tags,
	}

	_, err := a.EC2Client.CreateTags(params)
	return err
}

// DeleteTags deletes tags from a resource.
func (a *AWSAdapter) DeleteTags(resource string, tags []*ec2.Tag) error {
	params := &ec2.DeleteTagsInput{
		Resources: []*string{aws.String(resource)},
		Tags:      tags,
	}

	_, err := a.EC2Client.DeleteTags(params)
	return err
}

// TagSubnets tags all subnets in the specified VPC with the kubernetes cluster
// id tag.
func (a *AWSAdapter) TagSubnets(vpcID string, cluster *api.Cluster) error {
	subnets, err := a.GetSubnets(vpcID)
	if err != nil {
		return err
	}

	tag := &ec2.Tag{
		Key:   aws.String(KubernetesClusterTagPrefix + cluster.ID),
		Value: aws.String(ResourceLifecycleShared),
	}

	for _, subnet := range subnets {
		if !hasTag(subnet.Tags, tag) {
			err = a.CreateTags(
				aws.StringValue(subnet.SubnetId),
				[]*ec2.Tag{tag},
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// UntagSubnets removes the kubernetes cluster id tag from all subnets in the
// specified VPC.
func (a *AWSAdapter) UntagSubnets(vpcID string, cluster *api.Cluster) error {
	subnets, err := a.GetSubnets(vpcID)
	if err != nil {
		return err
	}

	tag := &ec2.Tag{
		Key:   aws.String(KubernetesClusterTagPrefix + cluster.ID),
		Value: aws.String(ResourceLifecycleShared),
	}

	for _, subnet := range subnets {
		if hasTag(subnet.Tags, tag) {
			err = a.DeleteTags(
				aws.StringValue(subnet.SubnetId),
				[]*ec2.Tag{tag},
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// hasTag returns true if tag is found in list of tags.
func hasTag(tags []*ec2.Tag, tag *ec2.Tag) bool {
	for _, t := range tags {
		if aws.StringValue(t.Key) == aws.StringValue(tag.Key) &&
			aws.StringValue(t.Value) == aws.StringValue(tag.Value) {
			return true
		}
	}
	return false
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

// IsWrongStackStatusErr returns true if the error is of type awserr.Error and
// describes a failure because of wrong Cloudformation stack status.
func IsWrongStackStatusErr(err error) bool {
	if awsErr, ok := err.(awserr.Error); ok {
		if awsErr.Code() == "ValidationError" && strings.Contains(awsErr.Message(), "cannot be deleted while in status") {
			return true
		}
	}
	return false
}
