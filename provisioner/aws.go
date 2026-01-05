package provisioner

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/cenkalti/backoff"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws/iface"
	awsutil "github.com/zalando-incubator/kube-ingress-aws-controller/aws"
	"github.com/zalando-incubator/kube-ingress-aws-controller/certs"
	yaml "gopkg.in/yaml.v2"
)

const (
	waitTime                  = 15 * time.Second
	stackMaxSize              = 51200
	cloudformationNoUpdateMsg = "No updates are to be performed."
	clmCFBucketPattern        = "cluster-lifecycle-manager-%s-%s"
	stackUpdateRetryDuration  = time.Duration(5) * time.Minute
)

var (
	maxWaitTimeout            = 25 * time.Minute
	errCreateFailed           = fmt.Errorf("wait for stack failed with %s", cftypes.StackStatusCreateFailed)
	errRollbackComplete       = fmt.Errorf("wait for stack failed with %s", cftypes.StackStatusRollbackComplete)
	errUpdateRollbackComplete = fmt.Errorf("wait for stack failed with %s", cftypes.StackStatusUpdateRollbackComplete)
	errRollbackFailed         = fmt.Errorf("wait for stack failed with %s", cftypes.StackStatusRollbackFailed)
	errUpdateRollbackFailed   = fmt.Errorf("wait for stack failed with %s", cftypes.StackStatusUpdateRollbackFailed)
	errDeleteFailed           = fmt.Errorf("wait for stack failed with %s", cftypes.StackStatusDeleteFailed)
	errTimeoutExceeded        = fmt.Errorf("wait for stack timeout exceeded")
)

type (
	cloudformationAPI interface {
		cloudformation.DescribeStacksAPIClient
		CreateStack(ctx context.Context, params *cloudformation.CreateStackInput, optFns ...func(*cloudformation.Options)) (*cloudformation.CreateStackOutput, error)
		UpdateTerminationProtection(ctx context.Context, params *cloudformation.UpdateTerminationProtectionInput, optFns ...func(*cloudformation.Options)) (*cloudformation.UpdateTerminationProtectionOutput, error)
		UpdateStack(ctx context.Context, params *cloudformation.UpdateStackInput, optFns ...func(*cloudformation.Options)) (*cloudformation.UpdateStackOutput, error)
		DeleteStack(ctx context.Context, params *cloudformation.DeleteStackInput, optFns ...func(*cloudformation.Options)) (*cloudformation.DeleteStackOutput, error)
	}
	// s3API is a minimal interface containing only the methods we use from the
	// S3 API
	s3API interface {
		CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	}

	s3UploaderAPI interface {
		Upload(ctx context.Context, input *s3.PutObjectInput, opts ...func(*manager.Uploader)) (*manager.UploadOutput, error)
	}

	iamAPI interface {
		iam.GetGroupAPIClient // TODO: update
	}

	acmAPI interface {
		acm.ListCertificatesAPIClient
		GetCertificate(ctx context.Context, params *acm.GetCertificateInput, optFns ...func(*acm.Options)) (*acm.GetCertificateOutput, error)
	}

	eksAPI interface {
		eks.DescribeClusterAPIClient // TODO: update
	}

	kmsAPI interface {
		DescribeKey(ctx context.Context, params *kms.DescribeKeyInput, optFns ...func(*kms.Options)) (*kms.DescribeKeyOutput, error)
		Encrypt(ctx context.Context, params *kms.EncryptInput, optFns ...func(*kms.Options)) (*kms.EncryptOutput, error)
	}

	awsAdapter struct {
		config               aws.Config
		cloudformationClient cloudformationAPI
		s3Client             s3API
		s3Uploader           s3UploaderAPI
		autoscalingClient    iface.AutoScalingAPI
		iamClient            iamAPI
		ec2Client            iface.EC2API
		acmClient            acmAPI
		eksClient            eksAPI
		region               string
		dryRun               bool
		logger               *log.Entry
		kmsClient            kmsAPI
	}

	// awsInterface is an interface containing methods of an AWS Adapter.
	//
	// CreationHook uses this interface in the Execute method.
	awsInterface interface {
		GetEKSClusterDetails(ctx context.Context, cluster *api.Cluster) (*EKSClusterDetails, error)
	}
)

// newAWSAdapter initializes a new awsAdapter.
func newAWSAdapter(logger *log.Entry, region string, cfg aws.Config, dryRun bool) *awsAdapter {
	s3Client := s3.NewFromConfig(cfg)
	return &awsAdapter{
		config:               cfg,
		cloudformationClient: cloudformation.NewFromConfig(cfg),
		iamClient:            iam.NewFromConfig(cfg),
		s3Client:             s3Client,
		s3Uploader:           manager.NewUploader(s3Client),
		autoscalingClient:    autoscaling.NewFromConfig(cfg),
		ec2Client:            ec2.NewFromConfig(cfg),
		acmClient:            acm.NewFromConfig(cfg),
		eksClient:            eks.NewFromConfig(cfg),
		region:               region,
		dryRun:               dryRun,
		logger:               logger,
		kmsClient:            kms.NewFromConfig(cfg),
	}
}

func (a *awsAdapter) VerifyAccount(ctx context.Context, accountID string) error {
	stsService := sts.NewFromConfig(a.config)
	response, err := stsService.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return err
	}
	effectiveAccount := aws.ToString(response.Account)
	expectedAccount := getAWSAccountID(accountID)
	if effectiveAccount != expectedAccount {
		return fmt.Errorf("invalid AWS account, expected %s, found %s", expectedAccount, effectiveAccount)
	}
	return nil
}

// applyClusterStack creates or updates a stack specified by stackName and
// stackTemplate.
// If the stackTemplate exceeds the max size, it will automatically upload it
// to S3 before creating or updating the stack.
func (a *awsAdapter) applyClusterStack(ctx context.Context, stackName, stackTemplate string, cluster *api.Cluster, s3BucketName string) error {
	var templateURL string
	if len(stackTemplate) > stackMaxSize {
		// Upload the stack template to S3
		result, err := a.s3Uploader.Upload(ctx, &s3.PutObjectInput{
			Bucket: aws.String(s3BucketName),
			Key:    aws.String(fmt.Sprintf("%s.template", cluster.ID)),
			Body:   strings.NewReader(stackTemplate),
		})
		if err != nil {
			return err
		}
		templateURL = result.Location
	}

	stackTags := map[string]string{
		tagNameKubernetesClusterPrefix + cluster.ID: resourceLifecycleOwned,
		mainStackTagKey: stackTagValueTrue,
	}

	return a.applyStack(ctx, stackName, stackTemplate, templateURL, stackTags, true, nil)
}

func mergeTags(tags ...map[string]string) map[string]string {
	mergedTags := make(map[string]string)
	for _, tagMap := range tags {
		for k, v := range tagMap {
			mergedTags[k] = v
		}
	}
	return mergedTags
}

func tagMapToCloudformationTags(tags map[string]string) []cftypes.Tag {
	cfTags := make([]cftypes.Tag, 0, len(tags))
	for k, v := range tags {
		tag := cftypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		}
		cfTags = append(cfTags, tag)
	}
	return cfTags
}

func tagsFromStackTemplate(template string) (map[string]string, error) {
	var parsedTemplate struct {
		Metadata struct {
			Tags map[string]string `yaml:"Tags"`
		} `yaml:"Metadata"`
	}
	err := yaml.Unmarshal([]byte(template), &parsedTemplate)
	if err != nil {
		return nil, err
	}
	return parsedTemplate.Metadata.Tags, nil
}

// applyStack applies a cloudformation stack.
// Optionally parses tags specified under the Tags key in the template and
// merges those with the tags passed via the parameter.
func (a *awsAdapter) applyStack(ctx context.Context, stackName string, stackTemplate string, stackTemplateURL string, tags map[string]string, updateStack bool, updatePolicy *stackPolicy) error {
	// parse tags from stack template
	stackTemplateTags, err := tagsFromStackTemplate(stackTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse tags from stack template %s: %v", stackName, err)
	}

	tags = mergeTags(stackTemplateTags, tags)
	cfTags := tagMapToCloudformationTags(tags)

	createParams := &cloudformation.CreateStackInput{
		StackName:                   aws.String(stackName),
		OnFailure:                   cftypes.OnFailureDelete,
		Capabilities:                []cftypes.Capability{cftypes.CapabilityCapabilityNamedIam},
		EnableTerminationProtection: aws.Bool(true),
		Tags:                        cfTags,
	}

	if stackTemplateURL != "" {
		createParams.TemplateURL = aws.String(stackTemplateURL)
	} else {
		createParams.TemplateBody = aws.String(stackTemplate)
	}

	_, err = a.cloudformationClient.CreateStack(ctx, createParams)
	if err != nil {
		var alreadyExists *cftypes.AlreadyExistsException
		if errors.As(err, &alreadyExists) {
			// if creation fails because the stack already
			// exists, lets try to update it instead

			// ensure stack termination protection is enabled.
			terminationParams := &cloudformation.UpdateTerminationProtectionInput{
				StackName:                   aws.String(stackName),
				EnableTerminationProtection: aws.Bool(true),
			}

			_, err := a.cloudformationClient.UpdateTerminationProtection(ctx, terminationParams)
			if err != nil {
				return err
			}

			if updateStack {
				// update the stack
				updateParams := &cloudformation.UpdateStackInput{
					StackName:    createParams.StackName,
					Capabilities: createParams.Capabilities,
					Tags:         cfTags,
				}

				if updatePolicy != nil {
					policyBody, err := json.Marshal(updatePolicy)
					if err != nil {
						return err
					}
					updateParams.StackPolicyDuringUpdateBody = aws.String(string(policyBody))
				}

				if stackTemplateURL != "" {
					updateParams.TemplateURL = aws.String(stackTemplateURL)
				} else {
					updateParams.TemplateBody = aws.String(stackTemplate)
				}

				updateStackFunc := func() error {
					_, err = a.cloudformationClient.UpdateStack(ctx, updateParams)
					if err != nil {
						// if no update was needed
						// treat it as success
						if isStackNoUpdateNeededErr(err) {
							return nil
						}
						// if the stack is currently updating, keep trying.
						if isStackUpdateInProgressErr(err) {
							return err
						}
						// treat any other error as non-retriable
						return backoff.Permanent(err)
					}
					return nil
				}
				backoffCfg := backoff.NewExponentialBackOff()
				backoffCfg.MaxElapsedTime = stackUpdateRetryDuration
				return backoff.Retry(updateStackFunc, backoffCfg)
			}
			return nil
		}
		return err
	}

	return nil
}

func (a *awsAdapter) getStackByName(ctx context.Context, stackName string) (*cftypes.Stack, error) {
	params := &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	}
	resp, err := a.cloudformationClient.DescribeStacks(ctx, params)
	if err != nil {
		return nil, err
	}
	//we expect only one stack
	if len(resp.Stacks) != 1 {
		return nil, fmt.Errorf("unexpected response, got %d, expected 1 stack", len(resp.Stacks))
	}
	return &resp.Stacks[0], nil
}

func (a *awsAdapter) waitForStack(ctx context.Context, waitTime time.Duration, stackName string) error {
	for {
		stack, err := a.getStackByName(ctx, stackName)
		if err != nil {
			return err
		}

		switch stack.StackStatus {
		case cftypes.StackStatusUpdateComplete:
			return nil
		case cftypes.StackStatusCreateComplete:
			return nil
		case cftypes.StackStatusDeleteComplete:
			return nil
		case cftypes.StackStatusCreateFailed:
			err = errCreateFailed
		case cftypes.StackStatusDeleteFailed:
			err = errDeleteFailed
		case cftypes.StackStatusRollbackComplete:
			err = errRollbackComplete
		case cftypes.StackStatusRollbackFailed:
			err = errRollbackFailed
		case cftypes.StackStatusUpdateRollbackComplete:
			err = errUpdateRollbackComplete
		case cftypes.StackStatusUpdateRollbackFailed:
			err = errUpdateRollbackFailed
		}
		if err != nil {
			if stack.StackStatusReason != nil {
				return fmt.Errorf("%v, reason: %v", err, *stack.StackStatusReason)
			}
			return err
		}
		a.logger.Debugf("Stack '%s' - [%s]", stackName, stack.StackStatus)

		select {
		case <-ctx.Done():
			return errTimeoutExceeded
		case <-time.After(waitTime):
		}
	}
}

// ListStacks lists stacks filtered by tags.
func (a *awsAdapter) ListStacks(ctx context.Context, includeTags, excludeTags map[string]string) ([]cftypes.Stack, error) {
	params := &cloudformation.DescribeStacksInput{}

	stacks := make([]cftypes.Stack, 0)
	paginator := cloudformation.NewDescribeStacksPaginator(a.cloudformationClient, params)
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, stack := range resp.Stacks {
			if cloudformationHasTags(includeTags, stack.Tags) && cloudformationDoesNotHaveTags(excludeTags, stack.Tags) {
				stacks = append(stacks, stack)
			}
		}
	}

	return stacks, nil
}

// cloudformationHasTags returns true if the expected tags are found in the
// tags list.
func cloudformationHasTags(expected map[string]string, tags []cftypes.Tag) bool {
	if len(expected) > len(tags) {
		return false
	}

	tagsMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		tagsMap[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
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
func cloudformationDoesNotHaveTags(excluded map[string]string, tags []cftypes.Tag) bool {
	tagsMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		tagsMap[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}

	for key, val := range excluded {
		if v, ok := tagsMap[key]; ok && v == val {
			return false
		}
	}

	return true
}

func isStackDeleting(stack *cftypes.Stack) bool {
	switch stack.StackStatus {
	case cftypes.StackStatusDeleteInProgress,
		cftypes.StackStatusDeleteComplete:
		return true
	default:
		return false
	}
}

// DeleteStack deletes a cloudformation stack.
func (a *awsAdapter) DeleteStack(ctx context.Context, stack *cftypes.Stack) error {
	stackName := aws.ToString(stack.StackName)
	a.logger.Infof("Deleting stack '%s'", stackName)

	if !isStackDeleting(stack) {
		// disable termination protection on stack before deleting
		terminationParams := &cloudformation.UpdateTerminationProtectionInput{
			StackName:                   aws.String(stackName),
			EnableTerminationProtection: aws.Bool(false),
		}

		_, err := a.cloudformationClient.UpdateTerminationProtection(ctx, terminationParams)
		if err != nil {
			if isDoesNotExistsErr(err) {
				return nil
			}
			return err
		}

		deleteParams := &cloudformation.DeleteStackInput{
			StackName: aws.String(stackName),
		}

		_, err = a.cloudformationClient.DeleteStack(ctx, deleteParams)
		if err != nil {
			if isDoesNotExistsErr(err) {
				return nil
			}
			return err
		}
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, maxWaitTimeout)
	defer cancel()
	err := a.waitForStack(ctxWithTimeout, waitTime, stackName)
	if err != nil {
		if isDoesNotExistsErr(err) {
			return nil
		}
		return err
	}
	return nil
}

func certificateExpiryTime(certificate string) (time.Time, error) {
	block, _ := pem.Decode([]byte(certificate))
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM data found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter.UTC(), nil
}

// resolveKeyID resolved a local key ID (e.g. alias/etcd-cluster) into the ARN
func (a *awsAdapter) resolveKeyID(ctx context.Context, keyID string) (string, error) {
	output, err := a.kmsClient.DescribeKey(ctx, &kms.DescribeKeyInput{
		KeyId: aws.String(keyID),
	})
	if err != nil {
		return "", err
	}

	return aws.ToString(output.KeyMetadata.Arn), nil
}

// kmsEncryptForTaupage encrypts a string using a Taupage-compatible format (aws:kms:…)
func (a *awsAdapter) kmsEncryptForTaupage(ctx context.Context, keyID string, value string) (string, error) {
	output, err := a.kmsClient.Encrypt(ctx, &kms.EncryptInput{
		KeyId:     aws.String(keyID),
		Plaintext: []byte(value),
	})
	if err != nil {
		return "", err
	}
	return "aws:kms:" + base64.StdEncoding.EncodeToString(output.CiphertextBlob), nil
}

// createS3Bucket creates an s3 bucket if it doesn't exist.
func (a *awsAdapter) createS3Bucket(ctx context.Context, bucket string) error {
	params := &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
		CreateBucketConfiguration: &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(a.region),
		},
	}

	return backoff.Retry(
		func() error {
			_, err := a.s3Client.CreateBucket(ctx, params)
			if err != nil {
				var alreadyOwnedByYou *s3types.BucketAlreadyOwnedByYou
				if errors.As(err, &alreadyOwnedByYou) {
					// if the bucket already exists and is owned by us, we
					// don't treat it as an error.
					return nil
				}
			}
			return err
		},
		backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 10))
}

func (a *awsAdapter) GetVolumes(ctx context.Context, tags map[string]string) ([]ec2types.Volume, error) {
	var filters []ec2types.Filter

	for tagKey, tagValue := range tags {
		filters = append(filters, ec2types.Filter{
			Name:   aws.String(fmt.Sprintf("tag:%s", tagKey)),
			Values: []string{tagValue},
		})
	}

	result, err := a.ec2Client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{Filters: filters})
	if err != nil {
		return nil, err
	}
	return result.Volumes, nil
}

func (a *awsAdapter) DeleteVolume(ctx context.Context, id string) error {
	_, err := a.ec2Client.DeleteVolume(ctx, &ec2.DeleteVolumeInput{
		VolumeId: aws.String(id),
	})
	return err
}

// GetCertificates gets all available 'ISSUED' certificates from ACM.
func (a *awsAdapter) GetCertificates(ctx context.Context) ([]*certs.CertificateSummary, error) {
	params := &acm.ListCertificatesInput{
		CertificateStatuses: []acmtypes.CertificateStatus{
			acmtypes.CertificateStatusIssued,
		},
	}
	acmSummaries := make([]acmtypes.CertificateSummary, 0)
	paginator := acm.NewListCertificatesPaginator(a.acmClient, params)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		acmSummaries = append(acmSummaries, page.CertificateSummaryList...)
	}

	result := make([]*certs.CertificateSummary, 0)
	for _, o := range acmSummaries {
		summary, err := a.getCertificateSummaryFromACM(ctx, o.CertificateArn)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, nil
}

func (a *awsAdapter) getCertificateSummaryFromACM(ctx context.Context, arn *string) (*certs.CertificateSummary, error) {
	params := &acm.GetCertificateInput{CertificateArn: arn}
	resp, err := a.acmClient.GetCertificate(ctx, params)
	if err != nil {
		return nil, err
	}

	cert, err := awsutil.ParseCertificate(aws.ToString(resp.Certificate))
	if err != nil {
		return nil, err
	}

	var chain []*x509.Certificate
	if resp.CertificateChain != nil {
		chain, err = awsutil.ParseCertificates(aws.ToString(resp.CertificateChain))
		if err != nil {
			return nil, err
		}
	}

	return certs.NewCertificate(aws.ToString(arn), cert, chain), nil
}

// GetDefaultVPC gets the default VPC.
func (a *awsAdapter) GetDefaultVPC(ctx context.Context) (*ec2types.Vpc, error) {
	// find default VPC
	vpcResp, err := a.ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, err
	}

	if len(vpcResp.Vpcs) == 1 {
		return &vpcResp.Vpcs[0], nil
	}

	var defaultVpc *ec2types.Vpc
	for _, vpc := range vpcResp.Vpcs {
		if aws.ToBool(vpc.IsDefault) {
			defaultVpc = &vpc
			break
		}
	}

	if defaultVpc == nil {
		return nil, fmt.Errorf("default VPC not found in account")
	}

	return defaultVpc, nil
}

// GetVPC gets VPC details for vpc specified by vpcID.
func (a *awsAdapter) GetVPC(ctx context.Context, vpcID string) (*ec2types.Vpc, error) {
	// find default VPC
	vpcResp, err := a.ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []string{vpcID},
	})
	if err != nil {
		return nil, err
	}

	if len(vpcResp.Vpcs) != 1 {
		return nil, fmt.Errorf("found %d VPC for VPCID %s, expected 1", len(vpcResp.Vpcs), vpcID)
	}

	return &vpcResp.Vpcs[0], nil
}

// GetSubnets gets all subnets of the default VPC in the target account.
func (a *awsAdapter) GetSubnets(ctx context.Context, vpcID string) ([]ec2types.Subnet, error) {
	subnetParams := &ec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	}

	subnetResp, err := a.ec2Client.DescribeSubnets(ctx, subnetParams)
	if err != nil {
		return nil, err
	}

	return subnetResp.Subnets, nil
}

// CreateTags adds or updates tags of a kubernetes.Resource.
func (a *awsAdapter) CreateTags(ctx context.Context, resource string, tags []ec2types.Tag) error {
	params := &ec2.CreateTagsInput{
		Resources: []string{resource},
		Tags:      tags,
	}

	_, err := a.ec2Client.CreateTags(ctx, params)
	return err
}

// DeleteTags deletes tags from a kubernetes.Resource.
func (a *awsAdapter) DeleteTags(ctx context.Context, resource string, tags []ec2types.Tag) error {
	params := &ec2.DeleteTagsInput{
		Resources: []string{resource},
		Tags:      tags,
	}

	_, err := a.ec2Client.DeleteTags(ctx, params)
	return err
}

func isDoesNotExistsErr(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() == "ValidationError" && strings.Contains(apiErr.ErrorMessage(), "does not exist") {
			return true
		}
	}
	return false
}

// isStackNoUpdateNeededErr returns true if the error is of type
// smithy.APIError and describes a failure because the stack template has no
// changes.
func isStackNoUpdateNeededErr(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() == "ValidationError" &&
			strings.Contains(apiErr.ErrorMessage(), cloudformationNoUpdateMsg) {
			// Stack is already up to date
			return true // or return a custom error indicating no changes
		}
	}
	return false
}

// isStackUpdateInProgressErr returns true if the error is of type
// smithy.APIError and describes a failure because the stack is currently
// updating.
func isStackUpdateInProgressErr(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() == "ValidationException" &&
			(strings.Contains(apiErr.ErrorMessage(), string(cftypes.ResourceStatusUpdateInProgress)) || strings.Contains(apiErr.ErrorMessage(), string(cftypes.StackStatusUpdateCompleteCleanupInProgress))) {
			// Stack operation in progress
			return true
		}
	}
	return false
}

// tagsToMap converts a list of ec2 tags to a map.
func tagsToMap(tags []ec2types.Tag) map[string]string {
	tagMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		tagMap[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return tagMap
}

// EKSClusterDetails contains details of an EKS cluster that are only available after creation.
type EKSClusterDetails struct {
	Endpoint             string
	CertificateAuthority string
	OIDCIssuerURL        string
	ServiceCIDR          string
}

func (a *awsAdapter) GetEKSClusterDetails(ctx context.Context, cluster *api.Cluster) (*EKSClusterDetails, error) {
	resp, err := a.eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(cluster.Alias),
	})
	if err != nil {
		return nil, err
	}

	clusterDetails := &EKSClusterDetails{
		Endpoint:             aws.ToString(resp.Cluster.Endpoint),
		CertificateAuthority: aws.ToString(resp.Cluster.CertificateAuthority.Data),
		OIDCIssuerURL:        aws.ToString(resp.Cluster.Identity.Oidc.Issuer),
	}

	if resp.Cluster.KubernetesNetworkConfig.IpFamily == ekstypes.IpFamilyIpv4 {
		clusterDetails.ServiceCIDR = aws.ToString(resp.Cluster.KubernetesNetworkConfig.ServiceIpv4Cidr)
	} else {
		clusterDetails.ServiceCIDR = aws.ToString(resp.Cluster.KubernetesNetworkConfig.ServiceIpv6Cidr)
	}

	return clusterDetails, nil
}

// DeleteLeakedAWSVPCCNIENIs deletes leaked AWS VPC CNI ENIs for a cluster.
// Leaked ENIs are identified those having a tag
// `cluster.k8s.amazonaws.com/name` with the cluster name and status
// `available`.
func (a *awsAdapter) DeleteLeakedAWSVPCCNIENIs(ctx context.Context, cluster *api.Cluster) error {
	enis := make([]ec2types.NetworkInterface, 0)
	paginator := ec2.NewDescribeNetworkInterfacesPaginator(a.ec2Client, &ec2.DescribeNetworkInterfacesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:cluster.k8s.amazonaws.com/name"),
				Values: []string{cluster.Name()},
			},
			{
				Name:   aws.String("status"),
				Values: []string{"available"},
			},
		},
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list AWS VPC CNI ENIs for cluster %q: %w", cluster.Name(), err)
		}
		enis = append(enis, page.NetworkInterfaces...)
	}

	for _, eni := range enis {
		_, err := a.ec2Client.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{
			NetworkInterfaceId: eni.NetworkInterfaceId,
		})
		if err != nil {
			return fmt.Errorf("failed to delete ENI %s: %w", aws.ToString(eni.NetworkInterfaceId), err)
		}
		log.Infof("Deleted AWS VPC ENI %q, from cluster %q", aws.ToString(eni.NetworkInterfaceId), cluster.Name())
	}

	return nil
}
