package provisioner

import "github.com/zalando-incubator/cluster-lifecycle-manager/channel"

const (
	provenanceOwnerKey          = "deployment.zalando.org/owner"
	provenanceOwnerValue        = "cluster-lifecycle-manager"
	provenanceRepositoryNameKey = "deployment.zalando.org/repository-name"
	provenanceRepositoryRefKey  = "deployment.zalando.org/repository-branch"
	provenanceFilePathKey       = "deployment.zalando.org/repository-file-path"
)

// provenance returns the annotations (Kubernetes resources) or tags
// (CloudFormation stacks) describing who manages a resource and which
// manifest it was created from.
func provenance(origin channel.Origin) map[string]string {
	result := map[string]string{provenanceOwnerKey: provenanceOwnerValue}
	if origin.Repository != "" {
		result[provenanceRepositoryNameKey] = origin.Repository
	}
	if origin.Ref != "" {
		result[provenanceRepositoryRefKey] = origin.Ref
	}
	if origin.FilePath != "" {
		result[provenanceFilePathKey] = origin.FilePath
	}
	return result
}
