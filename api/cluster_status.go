package api

// ClusterStatus describes the status of a cluster.
type ClusterStatus struct {
	CurrentVersion string     `json:"current_version" yaml:"current_version"`
	LastVersion    string     `json:"last_version"    yaml:"last_version"`
	NextVersion    string     `json:"next_version"    yaml:"next_version"`
	Problems       []*Problem `json:"problems"        yaml:"problems"`
}
