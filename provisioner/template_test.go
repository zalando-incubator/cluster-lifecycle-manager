package provisioner

import (
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func render(t *testing.T, templates map[string]string, templateName string, data interface{}) (string, error) {
	basedir, err := ioutil.TempDir(os.TempDir(), t.Name())
	require.NoError(t, err, "unable to create temp dir")

	defer os.RemoveAll(basedir)

	for name, content := range templates {
		fullPath := path.Join(basedir, name)
		parentDir := path.Dir(fullPath)
		err := os.MkdirAll(parentDir, 0755)
		require.NoError(t, err, "error while creating %s", parentDir)
		err = ioutil.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err, "error while writing %s", fullPath)
	}

	context := newTemplateContext(basedir)
	return renderTemplate(context, path.Join(basedir, templateName), data)
}

func TestTemplating(t *testing.T) {
	result, err := render(
		t,
		map[string]string{"dir/foo.yaml": "foo {{ . }}"},
		"dir/foo.yaml",
		"1")

	require.NoError(t, err)
	require.EqualValues(t, "foo 1", result)
}

func TestBase64(t *testing.T) {
	result, err := render(
		t,
		map[string]string{"dir/foo.yaml": "{{ . | base64 }}"},
		"dir/foo.yaml",
		"abc123")

	require.NoError(t, err)
	require.EqualValues(t, "YWJjMTIz", result)
}

func TestManifestHash(t *testing.T) {
	result, err := render(
		t,
		map[string]string{
			"dir/config.yaml": "foo {{ . }}",
			"dir/foo.yaml":    `{{ manifestHash "config.yaml" }}`,
		},
		"dir/foo.yaml",
		"abc123")

	require.NoError(t, err)
	require.EqualValues(t, "82b883f3662dfed3357ba6c497a77684b1d84468c6aa49bf89c4f209889ddc77", result)
}

func TestManifestHashMissingFile(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"dir/foo.yaml": `{{ manifestHash "missing.yaml" }}`,
		},
		"dir/foo.yaml",
		"abc123")

	require.Error(t, err)
}

func TestManifestHashRecursiveInclude(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"dir/config.yaml": `{{ manifestHash "foo.yaml" }}`,
			"dir/foo.yaml":    `{{ manifestHash "config.yaml" }}`,
		},
		"dir/foo.yaml",
		"abc123")

	require.Error(t, err)
}
