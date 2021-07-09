package provisioner

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
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
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
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
	"golang.org/x/oauth2"
	yaml "gopkg.in/yaml.v2"
)

const (
	waitTime                    = 15 * time.Second
	stackMaxSize                = 51200
	cloudformationValidationErr = "ValidationError"
	cloudformationNoUpdateMsg   = "No updates are to be performed."
	clmCFBucketPattern          = "cluster-lifecycle-manager-%s-%s"
	lifecycleStatusReady        = "ready"

	etcdInstanceTypeConfigItem      = "etcd_instance_type"
	etcdInstanceCountConfigItem     = "etcd_instance_count"
	etcdScalyrKeyConfigItem         = "etcd_scalyr_key"
	etcdBackupBucketConfigItem      = "etcd_s3_backup_bucket"
	etcdClientCAConfigItem          = "etcd_client_ca_cert"
	etcdClientKeyConfigItem         = "etcd_client_server_key"
	etcdClientCertificateConfigItem = "etcd_client_server_cert"
	etcdClientTLSEnabledConfigItem  = "etcd_client_tls_enabled"
	etcdImageConfigItem             = "etcd_image"

	applicationTagKey = "application"
	componentTagKey   = "component"
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

type s3UploaderAPI interface {
	Upload(input *s3manager.UploadInput, options ...func(*s3manager.Uploader)) (*s3manager.UploadOutput, error)
}

type awsAdapter struct {
	session              *session.Session
	cloudformationClient cloudformationiface.CloudFormationAPI
	s3Client             s3API
	s3Uploader           s3UploaderAPI
	autoscalingClient    autoscalingAPI
	iamClient            iamiface.IAMAPI
	ec2Client            ec2iface.EC2API
	acmClient            acmiface.ACMAPI
	region               string
	apiServer            string
	tokenSrc             oauth2.TokenSource
	dryRun               bool
	logger               *log.Entry
	kmsClient            kmsiface.KMSAPI
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
		kmsClient:            kms.New(sess),
	}, nil
}

func (a *awsAdapter) VerifyAccount(accountId string) error {
	stsService := sts.New(a.session)
	response, err := stsService.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return err
	}
	effectiveAccount := aws.StringValue(response.Account)
	expectedAccount := getAWSAccountID(accountId)
	if effectiveAccount != expectedAccount {
		return fmt.Errorf("invalid AWS account, expected %s, found %s", expectedAccount, effectiveAccount)
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

	stackTags := map[string]string{
		tagNameKubernetesClusterPrefix + cluster.ID: resourceLifecycleOwned,
		mainStackTagKey: stackTagValueTrue,
	}

	return a.applyStack(stackName, stackTemplate, templateURL, stackTags, true, nil)
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

func tagMapToCloudformationTags(tags map[string]string) []*cloudformation.Tag {
	cfTags := make([]*cloudformation.Tag, 0, len(tags))
	for k, v := range tags {
		tag := &cloudformation.Tag{
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
func (a *awsAdapter) applyStack(stackName string, stackTemplate string, stackTemplateURL string, tags map[string]string, updateStack bool, updatePolicy *stackPolicy) error {
	// parse tags from stack template
	stackTemplateTags, err := tagsFromStackTemplate(stackTemplate)
	if err != nil {
		return err
	}

	tags = mergeTags(stackTemplateTags, tags)
	cfTags := tagMapToCloudformationTags(tags)

	createParams := &cloudformation.CreateStackInput{
		StackName:                   aws.String(stackName),
		OnFailure:                   aws.String(cloudformation.OnFailureDelete),
		Capabilities:                []*string{aws.String(cloudformation.CapabilityCapabilityNamedIam)},
		EnableTerminationProtection: aws.Bool(true),
		Tags:                        cfTags,
	}

	if stackTemplateURL != "" {
		createParams.TemplateURL = aws.String(stackTemplateURL)
	} else {
		createParams.TemplateBody = aws.String(stackTemplate)
	}

	_, err = a.cloudformationClient.CreateStack(createParams)
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
func (a *awsAdapter) ListStacks(includeTags, excludeTags map[string]string) ([]*cloudformation.Stack, error) {
	params := &cloudformation.DescribeStacksInput{}

	stacks := make([]*cloudformation.Stack, 0)
	err := a.cloudformationClient.DescribeStacksPages(params, func(resp *cloudformation.DescribeStacksOutput, lastPage bool) bool {
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
func (a *awsAdapter) DeleteStack(parentCtx context.Context, stack *cloudformation.Stack) error {
	stackName := aws.StringValue(stack.StackName)
	a.logger.Infof("Deleting stack '%s'", stackName)

	if !isStackDeleting(stack) {
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
	}

	ctx, cancel := context.WithTimeout(parentCtx, maxWaitTimeout)
	defer cancel()
	err := a.waitForStack(ctx, waitTime, stackName)
	if err != nil {
		if isDoesNotExistsErr(err) {
			return nil
		}
		return err
	}
	return nil
}

// CreateOrUpdateEtcdStack creates or updates an etcd stack.
func (a *awsAdapter) CreateOrUpdateEtcdStack(parentCtx context.Context, stackName string, stackDefinition []byte, kmsKeyARN, networkCIDR, vpcID string, cluster *api.Cluster) error {
	bucketName := fmt.Sprintf("zalando-kubernetes-etcd-%s-%s", getAWSAccountID(cluster.InfrastructureAccount), cluster.Region)

	if bucket, ok := cluster.ConfigItems[etcdBackupBucketConfigItem]; ok {
		bucketName = bucket
	}

	hostedZone, err := getHostedZone(cluster.APIServerURL)
	if err != nil {
		return err
	}

	// check if stack exists
	// this is a hack to avoid calling senza to generate the etcd stack
	// which is only applied if the stack doesn't already exist
	describeParams := &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	}

	resp, err := a.cloudformationClient.DescribeStacks(describeParams)
	// Ignore the error because the error indicates that the stack is missing
	if err == nil && len(resp.Stacks) == 1 {
		return nil
	}

	encryptedScalyrKey, err := a.kmsEncryptForTaupage(kmsKeyARN, cluster.ConfigItems[etcdScalyrKeyConfigItem])
	if err != nil {
		return err
	}

	td, err := ioutil.TempDir("", "etcd-cluster")
	if err != nil {
		return err
	}
	defer os.RemoveAll(td)

	stackDefinitionPath := path.Join(td, "etcd-cluster.yaml")
	err = ioutil.WriteFile(stackDefinitionPath, stackDefinition, 0644)
	if err != nil {
		return err
	}

	args := []string{
		"print",
		stackDefinitionPath,
		"etcd",
		fmt.Sprintf("HostedZone=%s", hostedZone),
		fmt.Sprintf("EtcdS3Backup=%s", bucketName),
		fmt.Sprintf("NetworkCIDR=%s", networkCIDR),
		fmt.Sprintf("VpcID=%s", vpcID),
		fmt.Sprintf("InstanceType=%s", cluster.ConfigItems[etcdInstanceTypeConfigItem]),
		fmt.Sprintf("InstanceCount=%s", cluster.ConfigItems[etcdInstanceCountConfigItem]),
		fmt.Sprintf("KMSKey=%s", kmsKeyARN),
		fmt.Sprintf("ScalyrAccountKey=%s", encryptedScalyrKey),
	}

	for _, ci := range []struct {
		configItem    string
		senzaArgument string
		encrypt       bool
	}{
		{configItem: etcdClientCAConfigItem, senzaArgument: "ClientCACertificate", encrypt: true},
		{configItem: etcdClientKeyConfigItem, senzaArgument: "ClientKey", encrypt: true},
		{configItem: etcdClientCertificateConfigItem, senzaArgument: "ClientCertificate", encrypt: true},
		{configItem: etcdClientTLSEnabledConfigItem, senzaArgument: "ClientTLSEnabled"},
		{configItem: etcdImageConfigItem, senzaArgument: "EtcdImage"},
	} {
		if value, ok := cluster.ConfigItems[ci.configItem]; ok {
			if ci.encrypt {
				decoded, err := base64.StdEncoding.DecodeString(value)
				if err != nil {
					return err
				}
				encrypted, err := a.kmsEncryptForTaupage(kmsKeyARN, string(decoded))
				if err != nil {
					return err
				}
				value = encrypted
			}
			args = append(args, fmt.Sprintf("%s=%s", ci.senzaArgument, value))
		}
	}

	for configItem, senzaArg := range map[string]string{
		etcdClientCAConfigItem:          "CertificateExpiryCA",
		etcdClientCertificateConfigItem: "CertificateExpiryNode",
	} {
		if value, ok := cluster.ConfigItems[configItem]; ok && value != "" {
			// We store our certificates base64-encoded
			decoded, err := base64.StdEncoding.DecodeString(value)
			if err != nil {
				return err
			}
			expiry, err := certificateExpiryTime(string(decoded))
			if err != nil {
				return err
			}
			args = append(args, fmt.Sprintf("%s=%s", senzaArg, expiry.UTC().Format(time.RFC3339)))
		}
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

	tags := map[string]string{
		applicationTagKey: "kubernetes-etcd",
		componentTagKey:   "etcd-cluster",
	}

	err = a.applyStack(stackName, string(output), "", tags, false, nil)
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
func (a *awsAdapter) resolveKeyID(keyID string) (string, error) {
	output, err := a.kmsClient.DescribeKey(&kms.DescribeKeyInput{
		KeyId: aws.String(keyID),
	})
	if err != nil {
		return "", err
	}

	return aws.StringValue(output.KeyMetadata.Arn), nil
}

// kmsEncryptForTaupage encrypts a string using a Taupage-compatible format (aws:kms:…)
func (a *awsAdapter) kmsEncryptForTaupage(keyID string, value string) (string, error) {
	output, err := a.kmsClient.Encrypt(&kms.EncryptInput{
		KeyId:     aws.String(keyID),
		Plaintext: []byte(value),
	})
	if err != nil {
		return "", err
	}
	return "aws:kms:" + base64.StdEncoding.EncodeToString(output.CiphertextBlob), nil
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
		backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 10))
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

func (a *awsAdapter) getCertificateSummaryFromACM(arn *string) (*certs.CertificateSummary, error) {
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
func (a *awsAdapter) GetDefaultVPC() (*ec2.Vpc, error) {
	// find default VPC
	vpcResp, err := a.ec2Client.DescribeVpcs(&ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, err
	}

	if len(vpcResp.Vpcs) == 1 {
		return vpcResp.Vpcs[0], nil
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
