package api

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
	Provider              string            `json:"provider"               yaml:"provider"`
	Region                string            `json:"region"                 yaml:"region"`
	Status                *ClusterStatus    `json:"status"                 yaml:"status"`
	Outputs               map[string]string `json:"outputs"                yaml:"outputs"`
	Owner                 string            `json:"owner"                  yaml:"owner"`
}
