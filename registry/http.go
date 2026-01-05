package registry

import (
	"fmt"
	"net/url"
	"sort"
	"time"

	"golang.org/x/oauth2"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/samber/lo"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	apiclient "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/client"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/client/clusters"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/client/config_items"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/client/infrastructure_accounts"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
)

type httpRegistry struct {
	apiClient   *apiclient.ClusterRegistry
	tokenSource oauth2.TokenSource
}

// Options are options which can be used to configure the httpRegistry when it
// is initialized.
type Options struct {
	Debug bool
}

// NewHTTPRegistry initializes a new http based registry source.
func NewHTTPRegistry(server *url.URL, tokenSource oauth2.TokenSource, options *Options) Registry {
	registry := &httpRegistry{
		apiClient:   newClient(server, options),
		tokenSource: tokenSource,
	}

	return registry
}

// ListClusters lists filtered clusters from the registry.
func (r *httpRegistry) ListClusters(filter Filter) ([]*api.Cluster, error) {
	authInfo, err := newAuthInfo(r.tokenSource)
	if err != nil {
		return nil, err
	}

	resp, err := r.apiClient.Clusters.ListClusters(
		clusters.NewListClustersParams(),
		authInfo,
	)
	if err != nil {
		return nil, err
	}

	// sort by creation time
	sort.Slice(resp.Payload.Items, func(i, j int) bool {
		return time.Time(resp.Payload.Items[i].CreatedAt).Before(time.Time(resp.Payload.Items[j].CreatedAt))
	})

	// get all ready infrastructure accounts to lookup owner for clusters
	accounts, err := r.getReadyInfrastructureAccounts()
	if err != nil {
		return nil, err
	}

	var result []*api.Cluster
	clustersByAccount := map[string][]*api.Cluster{}

	for _, cluster := range resp.Payload.Items {
		if !filter.Includes(cluster) {
			continue
		}

		c, err := convertFromClusterModel(cluster)
		if err != nil {
			return nil, err
		}
		if account, ok := accounts[c.InfrastructureAccount]; ok {
			c.Owner = *account.Owner
			c.AccountName = *account.Name
		}
		result = append(result, c)
		clustersByAccount[c.InfrastructureAccount] = append(clustersByAccount[c.InfrastructureAccount], c)
	}
	for _, cluster := range result {
		cluster.AccountClusters = clustersByAccount[cluster.InfrastructureAccount]
		if err := cluster.InitOIDCProvider(); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// UpdateLifecycleStatus updates the lifecycle_status and status field of the
// given cluster in the registry.
func (r *httpRegistry) UpdateLifecycleStatus(cluster *api.Cluster) error {
	authInfo, err := newAuthInfo(r.tokenSource)
	if err != nil {
		return err
	}

	_, err = r.apiClient.Clusters.UpdateCluster(
		clusters.NewUpdateClusterParams().WithClusterID(
			cluster.ID,
		).WithCluster(
			&models.ClusterUpdate{
				LifecycleStatus: cluster.LifecycleStatus,
				Status:          convertToClusterStatusModel(cluster.Status),
			},
		),
		authInfo,
	)

	return err
}

// UpdateConfigItems updates the config items of the given cluster in the
// registry.
func (r *httpRegistry) UpdateConfigItems(
	cluster *api.Cluster,
	configItems map[string]string,
) error {
	authInfo, err := newAuthInfo(r.tokenSource)
	if err != nil {
		return err
	}

	if cluster.ConfigItems == nil {
		cluster.ConfigItems = map[string]string{}
	}

	for key, value := range configItems {
		_, err = r.apiClient.ConfigItems.AddOrUpdateConfigItem(
			config_items.NewAddOrUpdateConfigItemParams().WithClusterID(
				cluster.ID,
			).WithConfigKey(key).WithValue(&models.ConfigValue{Value: &value}),
			authInfo,
		)

		if err != nil {
			return err
		}

		cluster.ConfigItems[key] = value
	}

	return nil
}

// getReadyInfrastructureAccounts gets all ready infrastructure accounts from
// the registry and converts the list to a map.
func (r *httpRegistry) getReadyInfrastructureAccounts() (map[string]*models.InfrastructureAccount, error) {
	authInfo, err := newAuthInfo(r.tokenSource)
	if err != nil {
		return nil, err
	}

	resp, err := r.apiClient.InfrastructureAccounts.ListInfrastructureAccounts(
		infrastructure_accounts.NewListInfrastructureAccountsParams().
			WithLifecycleStatus(aws.String(models.InfrastructureAccountLifecycleStatusReady)),
		authInfo,
	)
	if err != nil {
		return nil, err
	}

	accounts := make(map[string]*models.InfrastructureAccount)
	for _, account := range resp.Payload.Items {
		accounts[*account.ID] = account
	}

	return accounts, nil
}

func newClient(server *url.URL, options *Options) *apiclient.ClusterRegistry {
	// initialize options if not provided
	if options == nil {
		options = &Options{}
	}

	// create the transport
	transport := httptransport.New(server.Host, server.Path, []string{server.Scheme})
	transport.Debug = options.Debug

	// create the API client, with the transport
	client := apiclient.New(transport, strfmt.Default)

	// return the client
	return client
}

func newAuthInfo(tokenSource oauth2.TokenSource) (runtime.ClientAuthInfoWriter, error) {
	token, err := tokenSource.Token()
	if err != nil {
		return nil, err
	}

	return httptransport.BearerToken(token.AccessToken), nil
}

// converts a Cluster model generated from the cluster-registry swagger spec
// into an *api.Cluster struct.
func convertFromClusterModel(cluster *models.Cluster) (*api.Cluster, error) {
	nodePools := make([]*api.NodePool, 0, len(cluster.NodePools))
	for _, pool := range cluster.NodePools {
		converted, err := convertFromNodePoolModel(pool)
		if err != nil {
			return nil, err
		}
		nodePools = append(nodePools, converted)
	}

	return &api.Cluster{
		Alias:                 *cluster.Alias,
		APIServerURL:          *cluster.APIServerURL,
		Channel:               *cluster.Channel,
		ConfigItems:           cluster.ConfigItems,
		CriticalityLevel:      *cluster.CriticalityLevel,
		Environment:           *cluster.Environment,
		ID:                    *cluster.ID,
		InfrastructureAccount: *cluster.InfrastructureAccount,
		LifecycleStatus:       *cluster.LifecycleStatus,
		LocalID:               *cluster.LocalID,
		NodePools:             nodePools,
		Provider:              api.ProviderID(*cluster.Provider),
		Region:                *cluster.Region,
		Status:                convertFromClusterStatusModel(cluster.Status),
	}, nil

}

// converts a NodePool model generated from the cluster-registry swagger spec
// into an *api.NodePool struct.
func convertFromNodePoolModel(nodePool *models.NodePool) (*api.NodePool, error) {
	var instanceType string
	if lo.FromPtr(nodePool.Profile) == "worker-karpenter" && len(nodePool.InstanceTypes) == 0 {
		instanceType = ""
	} else if len(nodePool.InstanceTypes) == 0 {
		return nil, fmt.Errorf("no instance types for pool %s", *nodePool.Name)
	} else {
		instanceType = nodePool.InstanceTypes[0]
	}
	return &api.NodePool{
		DiscountStrategy: *nodePool.DiscountStrategy,
		InstanceTypes: lo.Map(nodePool.InstanceTypes, func(s string, _ int) types.InstanceType {
			return types.InstanceType(s)
		}),
		InstanceType: types.InstanceType(instanceType),
		Name:         *nodePool.Name,
		Profile:      *nodePool.Profile,
		MinSize:      *nodePool.MinSize,
		MaxSize:      *nodePool.MaxSize,
		ConfigItems:  nodePool.ConfigItems,
	}, nil
}

// converts a ClusterStatus model generated from the cluster-registry swagger
// spec into an *api.ClusterStatus struct.
func convertFromClusterStatusModel(status *models.ClusterStatus) *api.ClusterStatus {
	problems := make([]*api.Problem, 0, len(status.Problems))

	for _, problem := range status.Problems {
		problems = append(problems, convertFromProblemModel(problem))
	}

	return &api.ClusterStatus{
		CurrentVersion: status.CurrentVersion,
		LastVersion:    status.LastVersion,
		NextVersion:    status.NextVersion,
		Problems:       problems,
	}
}

// converts a ClusterStatusProblemsItems0 model generated from the
// cluster-registry swagger spec into an *api.Problem struct.
func convertFromProblemModel(problem *models.ClusterStatusProblemsItems0) *api.Problem {
	return &api.Problem{
		Detail:   problem.Detail,
		Instance: problem.Instance,
		Status:   problem.Status,
		Title:    *problem.Title,
		Type:     *problem.Type,
	}
}

// converts a *api.ClusterStatus struct to the corresponding model generated
// from the cluster-registry swagger spec.
func convertToClusterStatusModel(status *api.ClusterStatus) *models.ClusterStatus {
	problems := make([]*models.ClusterStatusProblemsItems0, 0, len(status.Problems))

	for _, problem := range status.Problems {
		problems = append(problems, convertToProblemModel(problem))
	}

	return &models.ClusterStatus{
		CurrentVersion: status.CurrentVersion,
		LastVersion:    status.LastVersion,
		NextVersion:    status.NextVersion,
		Problems:       problems,
	}
}

// converts a *api.Problem struct to the corresponding model generated from the
// cluster-registry swagger spec.
func convertToProblemModel(problem *api.Problem) *models.ClusterStatusProblemsItems0 {
	return &models.ClusterStatusProblemsItems0{
		Detail:   problem.Detail,
		Instance: problem.Instance,
		Status:   problem.Status,
		Title:    &problem.Title,
		Type:     &problem.Type,
	}
}
