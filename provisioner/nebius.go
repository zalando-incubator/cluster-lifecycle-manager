package provisioner

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
)

// NebiusProvisioner applies manifests to an existing Nebius cluster.
type NebiusProvisioner struct {
	apply           *kubernetesApplyProvisioner
	setupAWSAdapter func(context.Context, *log.Entry, *api.Cluster) (*awsAdapter, error)
}

// NewNebiusProvisioner uses the current kubectl context until Nebius authentication is implemented.
func NewNebiusProvisioner(secretDecrypter decrypter.Decrypter, assumedRole string, awsConfigOptions []func(*awsconfig.LoadOptions) error, dryRun bool) Provisioner {
	// setupAWSAdapter appends a per-cluster region; do not share spare capacity
	// between concurrent provisioning calls or with other provisioners.
	awsConfigOptions = append([]func(*awsconfig.LoadOptions) error(nil), awsConfigOptions...)
	awsConfigOptions = awsConfigOptions[:len(awsConfigOptions):len(awsConfigOptions)]
	awsProvisioner := &clusterpyProvisioner{assumedRole: assumedRole, awsConfigOptions: awsConfigOptions, dryRun: dryRun}
	return &NebiusProvisioner{
		apply:           newKubernetesApplyProvisioner(api.ZalandoNebiusProvider, newCurrentContextKubernetesClient, secretDecrypter, dryRun),
		setupAWSAdapter: awsProvisioner.setupAWSAdapter,
	}
}

func (p *NebiusProvisioner) Supports(cluster *api.Cluster) bool {
	return cluster.Provider == api.ZalandoNebiusProvider
}

func (p *NebiusProvisioner) Provision(ctx context.Context, logger *log.Entry, cluster *api.Cluster, config channel.Config) error {
	if !p.Supports(cluster) {
		return ErrProviderNotSupported
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cluster.ID == "" || cluster.LocalID == "" {
		return fmt.Errorf("cluster requires id and local_id for the CloudFormation stack")
	}
	if cluster.Region == "" {
		return fmt.Errorf("cluster requires an AWS region for the CloudFormation stack")
	}
	account := strings.TrimPrefix(cluster.InfrastructureAccount, "aws:")
	if account == cluster.InfrastructureAccount || len(account) != 12 || strings.Trim(account, "0123456789") != "" {
		return fmt.Errorf("cluster requires an AWS infrastructure account in aws:<12-digit-account-id> format")
	}
	adapter, err := p.setupAWSAdapter(ctx, logger, cluster)
	if err != nil {
		return fmt.Errorf("failed to setup AWS adapter: %w", err)
	}
	if err := p.apply.prepareConfig(ctx, cluster, config, adapter, nil); err != nil {
		return err
	}
	values := map[string]any{}
	outputs, err := p.provisionClusterStack(ctx, logger, adapter, cluster, config, values)
	if err != nil {
		return err
	}
	values[clusterStackOutputKey] = outputs
	return p.apply.provisionPrepared(ctx, logger, cluster, config, values, adapter, nil)
}

// provisionClusterStack renders the provider-specific cluster.yaml and applies it
// before the Kubernetes manifests, making its outputs available to templates.
func (p *NebiusProvisioner) provisionClusterStack(ctx context.Context, logger *log.Entry, adapter *awsAdapter, cluster *api.Cluster, config channel.Config, values map[string]any) (map[string]string, error) {
	manifest, err := config.StackManifest(clusterStackFileName)
	if err != nil {
		return nil, fmt.Errorf("unable to read cluster stack: %w", err)
	}
	rendered, err := renderSingleTemplate(manifest, cluster, nil, values, adapter, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to render cluster stack %s: %w", manifest.Path, err)
	}
	if _, err := tagsFromStackTemplate(rendered); err != nil {
		return nil, fmt.Errorf("unable to parse cluster stack %s: %w", manifest.Path, err)
	}
	if p.apply.dryRun {
		logger.Infof("Dry run: skipping CloudFormation cluster stack %s", cluster.LocalID)
		return map[string]string{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logger.Infof("Applying CloudFormation cluster stack %s in %s (%s)", cluster.LocalID, cluster.InfrastructureAccount, cluster.Region)
	bucketName := fmt.Sprintf(clmCFBucketPattern, strings.TrimPrefix(cluster.InfrastructureAccount, "aws:"), cluster.Region)
	if len(rendered) > stackMaxSize {
		if err := adapter.createS3Bucket(ctx, bucketName); err != nil {
			return nil, fmt.Errorf("failed to create CloudFormation template bucket: %w", err)
		}
	}
	if err := adapter.applyClusterStack(ctx, cluster.LocalID, rendered, cluster, bucketName); err != nil {
		return nil, fmt.Errorf("failed to apply cluster stack %s: %w", cluster.LocalID, err)
	}
	ctx, cancel := context.WithTimeout(ctx, maxWaitTimeout)
	defer cancel()
	if err := adapter.waitForStack(ctx, waitTime, cluster.LocalID); err != nil {
		return nil, fmt.Errorf("failed to wait for cluster stack %s: %w", cluster.LocalID, err)
	}
	stack, err := adapter.getStackByName(ctx, cluster.LocalID)
	if err != nil {
		return nil, fmt.Errorf("failed to read cluster stack %s: %w", cluster.LocalID, err)
	}
	outputs := make(map[string]string, len(stack.Outputs))
	for _, output := range stack.Outputs {
		outputs[aws.ToString(output.OutputKey)] = aws.ToString(output.OutputValue)
	}
	return outputs, nil
}

func (p *NebiusProvisioner) Decommission(ctx context.Context, logger *log.Entry, cluster *api.Cluster) error {
	if !p.Supports(cluster) {
		return ErrProviderNotSupported
	}
	return p.apply.Decommission(ctx, logger, cluster)
}
