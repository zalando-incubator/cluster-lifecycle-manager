package provisioner

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
)

func TestSourceInformation(t *testing.T) {
	require.Equal(t, map[string]string{
		"deployment.zalando.org/owner": "cluster-lifecycle-manager",
	}, sourceInformation(channel.Origin{}))

	require.Equal(t, map[string]string{
		"deployment.zalando.org/owner":                "cluster-lifecycle-manager",
		"deployment.zalando.org/repository-name":      "org/repo",
		"deployment.zalando.org/repository-branch":    "main",
		"deployment.zalando.org/repository-file-path": "cluster/stack.yaml",
	}, sourceInformation(channel.Origin{Repository: "org/repo", Ref: "main", FilePath: "cluster/stack.yaml"}))
}

func TestSetAnnotations(t *testing.T) {
	manifests, err := kubernetes.ParseResourceManifest("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  annotations:\n    keep: me\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n", "x.yaml")
	require.NoError(t, err)
	for _, m := range manifests {
		m.SetAnnotations(map[string]string{"foo": "bar"})
	}
	a, err := manifests[0].ToYaml()
	require.NoError(t, err)
	require.Contains(t, a, "keep: me")
	require.Contains(t, a, "foo: bar")
	b, err := manifests[1].ToYaml()
	require.NoError(t, err)
	require.Contains(t, b, "foo: bar")
}

func TestSetAnnotationsOverridesExisting(t *testing.T) {
	manifests, err := kubernetes.ParseResourceManifest("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  annotations:\n    deployment.zalando.org/owner: someone-else\n", "x.yaml")
	require.NoError(t, err)
	manifests[0].SetAnnotations(sourceInformation(channel.Origin{}))
	out, err := manifests[0].ToYaml()
	require.NoError(t, err)
	require.Contains(t, out, "deployment.zalando.org/owner: cluster-lifecycle-manager")
	require.NotContains(t, out, "someone-else")
}

func TestSourceInformationOverridesTemplateTags(t *testing.T) {
	templateTags, err := tagsFromStackTemplate("Metadata:\n  Tags:\n    deployment.zalando.org/owner: someone-else\n    other: kept\n")
	require.NoError(t, err)
	merged := mergeTags(templateTags, sourceInformation(channel.Origin{}))
	require.Equal(t, "cluster-lifecycle-manager", merged["deployment.zalando.org/owner"])
	require.Equal(t, "kept", merged["other"])
}

func TestSourceInformationSanitizesValues(t *testing.T) {
	tags := sourceInformation(channel.Origin{Repository: "org/repo", Ref: "feature/a b#c", FilePath: "cluster/ünï$.yaml"})
	require.Equal(t, "feature/a b_c", tags["deployment.zalando.org/repository-branch"])
	require.Equal(t, "cluster/ünï_.yaml", tags["deployment.zalando.org/repository-file-path"])

	long := strings.Repeat("a", 300)
	tags = sourceInformation(channel.Origin{FilePath: long})
	require.Len(t, tags["deployment.zalando.org/repository-file-path"], 256)
}
