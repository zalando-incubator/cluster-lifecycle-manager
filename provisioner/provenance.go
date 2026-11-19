package provisioner

import (
	"strings"
	"unicode"

	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
)

const (
	provenanceOwnerKey          = "deployment.zalando.org/owner"
	provenanceOwnerValue        = "cluster-lifecycle-manager"
	provenanceRepositoryNameKey = "deployment.zalando.org/repository-name"
	provenanceRepositoryRefKey  = "deployment.zalando.org/repository-branch"
	provenanceFilePathKey       = "deployment.zalando.org/repository-file-path"

	// https://docs.aws.amazon.com/AWSCloudFormation/latest/APIReference/API_Tag.html
	tagSanitizationCharacter = "_"
	tagMaxValueLength        = 256
)

// provenance returns the annotations (Kubernetes resources) or tags
// (CloudFormation stacks) describing who manages a resource and which
// manifest it was created from. Values are sanitized to be valid CloudFormation
// tag values, the same way deployment-service does it for both resource kinds.
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
	for k, v := range result {
		result[k] = sanitizeTagValue(v)
	}
	return result
}

// sanitizeTagValue replaces characters not allowed in tag values with "_" and
// truncates the value to the maximum allowed length.
func sanitizeTagValue(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for i, r := range []rune(value) {
		if i >= tagMaxValueLength {
			break
		}
		if validTagRune(r) {
			builder.WriteRune(r)
		} else {
			builder.WriteString(tagSanitizationCharacter)
		}
	}
	return builder.String()
}

// validTagRune matches [\p{L}\p{Z}\p{N}_.:/=+\-@].
func validTagRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsSpace(r) || unicode.IsNumber(r) {
		return true
	}
	switch r {
	case '_', '.', ':', '/', '=', '+', '-', '@':
		return true
	}
	return false
}
