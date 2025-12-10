package api

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util"
)

// A provider ID is a string that identifies a cluster provider.
type ProviderID string

const (
	overrideChannelConfigItem = "override_channel"

	// ZalandoAWSProvider is the provider ID for Zalando managed AWS clusters.
	ZalandoAWSProvider ProviderID = "zalando-aws"
	// ZalandoEKSProvider is the provider ID for AWS EKS clusters.
	ZalandoEKSProvider ProviderID = "zalando-eks"
)

// Cluster describes a kubernetes cluster and related configuration.
type Cluster struct {
	Alias                 string            `json:"alias"                  yaml:"alias"`
	APIServerURL          string            `json:"api_server_url"         yaml:"api_server_url"`
	Channel               string            `json:"channel"                yaml:"channel"`
	ConfigItems           map[string]string `json:"config_items"           yaml:"config_items"`
	CriticalityLevel      int32             `json:"criticality_level"      yaml:"criticality_level"`
	Environment           string            `json:"environment"            yaml:"environment"`
	ID                    string            `json:"id"                     yaml:"id"`
	InfrastructureAccount string            `json:"infrastructure_account" yaml:"infrastructure_account"`
	LifecycleStatus       string            `json:"lifecycle_status"       yaml:"lifecycle_status"`
	LocalID               string            `json:"local_id"               yaml:"local_id"`
	NodePools             []*NodePool       `json:"node_pools"             yaml:"node_pools"`
	Provider              ProviderID        `json:"provider"               yaml:"provider"`
	Region                string            `json:"region"                 yaml:"region"`
	Status                *ClusterStatus    `json:"status"                 yaml:"status"`
	Owner                 string            `json:"owner"                  yaml:"owner"`
	AccountName           string            `json:"account_name"           yaml:"account_name"`
	CreatedAt             time.Time         `json:"created_at"             yaml:"created_at"`
	// Local fields to hold information about the OIDC provider.
	AccountClusters                  []*Cluster
	OIDCProvider                     string
	IAMRoleTrustRelationshipTemplate string
}

type AssumeRolePolicyDocument struct {
	Version   string
	Statement []Statement
}

type Statement struct {
	Effect    string
	Principal map[string]string
	Action    string
	Condition map[string]map[string]string `json:",omitempty"`
}

func (cluster *Cluster) InitOIDCProvider() error {
	if cluster.Provider == ZalandoEKSProvider {
		cluster.OIDCProvider = strings.TrimPrefix(cluster.ConfigItems["eks_oidc_issuer_url"], "https://")
	} else {
		hostedZone, err := util.GetHostedZone(cluster.APIServerURL)
		if err != nil {
			return fmt.Errorf("error while getting trust relationship for %s: %v", cluster.Alias, err)
		}
		cluster.OIDCProvider = fmt.Sprintf("%s.%s", cluster.LocalID, hostedZone)
	}

	trustRelationship := trustRelationship(cluster.AccountClusters)
	trustRelationshipJSON, err := json.Marshal(trustRelationship)
	if err != nil {
		return fmt.Errorf("error while marshalling trust relationship for %s: %v", cluster.Alias, err)
	}

	cluster.IAMRoleTrustRelationshipTemplate = string(trustRelationshipJSON)
	return nil
}

// Version returns the version derived from a sha1 hash of the cluster struct
// and the channel config version.
func (cluster *Cluster) Version(channelVersion channel.ConfigVersion) (*ClusterVersion, error) {
	state := new(bytes.Buffer)

	_, err := state.WriteString(cluster.ID)
	if err != nil {
		return nil, err
	}
	_, err = state.WriteString(cluster.InfrastructureAccount)
	if err != nil {
		return nil, err
	}
	_, err = state.WriteString(cluster.LocalID)
	if err != nil {
		return nil, err
	}
	_, err = state.WriteString(cluster.APIServerURL)
	if err != nil {
		return nil, err
	}
	_, err = state.WriteString(cluster.Channel)
	if err != nil {
		return nil, err
	}
	_, err = state.WriteString(cluster.Environment)
	if err != nil {
		return nil, err
	}
	err = binary.Write(state, binary.LittleEndian, cluster.CriticalityLevel)
	if err != nil {
		return nil, err
	}
	_, err = state.WriteString(cluster.LifecycleStatus)
	if err != nil {
		return nil, err
	}
	_, err = state.WriteString(string(cluster.Provider))
	if err != nil {
		return nil, err
	}
	_, err = state.WriteString(cluster.Region)
	if err != nil {
		return nil, err
	}

	// config items are sorted by key to produce a predictable string for
	// hashing.
	keys := make([]string, 0, len(cluster.ConfigItems))
	for key := range cluster.ConfigItems {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, err = state.WriteString(key)
		if err != nil {
			return nil, err
		}
		_, err = state.WriteString(cluster.ConfigItems[key])
		if err != nil {
			return nil, err
		}
	}

	// node pools
	for _, nodePool := range cluster.NodePools {
		_, err = state.WriteString(nodePool.Name)
		if err != nil {
			return nil, err
		}
		_, err = state.WriteString(nodePool.Profile)
		if err != nil {
			return nil, err
		}
		for _, instanceType := range nodePool.InstanceTypes {
			_, err = state.WriteString(instanceType)
			if err != nil {
				return nil, err
			}
		}
		_, err = state.WriteString(nodePool.DiscountStrategy)
		if err != nil {
			return nil, err
		}
		err = binary.Write(state, binary.LittleEndian, nodePool.MinSize)
		if err != nil {
			return nil, err
		}
		err = binary.Write(state, binary.LittleEndian, nodePool.MaxSize)
		if err != nil {
			return nil, err
		}
		// config items are sorted by key to produce a predictable string for
		// hashing.
		keys := make([]string, 0, len(nodePool.ConfigItems))
		for key := range nodePool.ConfigItems {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			_, err = state.WriteString(key)
			if err != nil {
				return nil, err
			}
			_, err = state.WriteString(nodePool.ConfigItems[key])
			if err != nil {
				return nil, err
			}
		}
	}

	// sha1 hash the cluster content
	hasher := sha1.New()
	_, err = hasher.Write(state.Bytes())
	if err != nil {
		return nil, err
	}

	result := &ClusterVersion{
		ConfigVersion: channelVersion.ID(),
		ClusterHash:   base64.RawURLEncoding.EncodeToString(hasher.Sum(nil)),
	}
	return result, nil
}

func (cluster *Cluster) ChannelOverrides() (map[string]string, error) {
	result := map[string]string{}
	if overrides, ok := cluster.ConfigItems[overrideChannelConfigItem]; ok {
		channelOverrides := strings.Split(overrides, ",")
		for _, override := range channelOverrides {
			parts := strings.SplitN(override, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid override definition: %s", override)
			}
			result[parts[0]] = parts[1]
		}
	}
	return result, nil
}

func (cluster *Cluster) KarpenterPools() []*NodePool {
	var kp []*NodePool
	for _, n := range cluster.NodePools {
		if n.IsKarpenter() {
			kp = append(kp, n)
		}
	}
	return kp
}

func (cluster *Cluster) ASGBackedPools() []*NodePool {
	var cp []*NodePool
	for _, n := range cluster.NodePools {
		if !n.IsKarpenter() {
			cp = append(cp, n)
		}
	}
	return cp
}

func (cluster Cluster) Name() string {
	if cluster.Provider == ZalandoEKSProvider {
		return cluster.Alias
	}
	return cluster.ID
}

func (cluster Cluster) InfrastructureAccountID() string {
	return strings.TrimPrefix(cluster.InfrastructureAccount, "aws:")
}

func (cluster Cluster) WorkerRoleARN() string {
	return fmt.Sprintf("arn:aws:iam::%s:role/%s-worker", cluster.InfrastructureAccountID(), cluster.LocalID)
}

func (cluster Cluster) OIDCProviderARN() string {
	return fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s", cluster.InfrastructureAccountID(), cluster.OIDCProvider)
}

func (cluster Cluster) OIDCSubjectKey() string {
	return fmt.Sprintf("%s:sub", cluster.OIDCProvider)
}

func trustRelationship(clusters []*Cluster) AssumeRolePolicyDocument {
	policyDocument := AssumeRolePolicyDocument{
		Version: "2012-10-17",
		Statement: []Statement{
			{
				Effect: "Allow",
				Principal: map[string]string{
					"Service": "ec2.amazonaws.com",
				},
				Action: "sts:AssumeRole",
			},
		},
	}

	for _, cluster := range clusters {
		if cluster.LifecycleStatus == models.ClusterLifecycleStatusReady {
			policyDocument.Statement = append(policyDocument.Statement, policyStatements(cluster.WorkerRoleARN(), cluster.OIDCProviderARN(), cluster.OIDCSubjectKey())...)
		}
	}

	return policyDocument
}

func policyStatements(workerRole string, identityProvider string, subjectKey string) []Statement {
	return []Statement{
		{
			Effect: "Allow",
			Principal: map[string]string{
				"AWS": workerRole,
			},
			Action: "sts:AssumeRole",
		},
		{
			Effect: "Allow",
			Principal: map[string]string{
				"Federated": identityProvider,
			},
			Action: "sts:AssumeRoleWithWebIdentity",
			Condition: map[string]map[string]string{
				"StringLike": {
					subjectKey: "system:serviceaccount:${SERVICE_ACCOUNT}",
				},
			},
		},
	}
}

// IsOldestReadyCluster returns true if the cluster is the oldest ready cluster in its account.
// It assumes that AccountClusters is sorted by creation time.
// If there are no other clusters, the cluster is considered the oldest ready cluster by default.
func (cluster Cluster) IsOldestReadyCluster() bool {
	for _, c := range cluster.AccountClusters {
		if c.LifecycleStatus == models.ClusterLifecycleStatusReady {
			return cluster.ID == c.ID
		}
	}

	return len(cluster.AccountClusters) == 0
}
