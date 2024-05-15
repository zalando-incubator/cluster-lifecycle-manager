package kubernetes

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
)

var (
	errUnsupportedManifest = errors.New("unsupported manifest, expecting kind/apiVersion/metadata.name for Kubernetes")
)

type ResourceManifest struct {
	Content    interface{}
	Name       string
	Namespace  string
	Kind       string
	APIVersion string
	SourceFile string
}

// ToYaml serializes the manifest back to Yaml
func (m *ResourceManifest) ToYaml() (string, error) {
	result, err := yaml.Marshal(m.Content)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

// FixupYamlValue converts the map types in a parsed opaque manifest
// from map[interface{}]interface{}. Won't be needed once yaml.v3 is released.
func FixupYamlValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[interface{}]interface{}:
		res := make(map[string]interface{}, len(v))
		for key, elem := range v {
			res[fmt.Sprintf("%s", key)] = FixupYamlValue(elem)
		}
		return res
	case []interface{}:
		res := make([]interface{}, 0)
		for _, elem := range v {
			res = append(res, FixupYamlValue(elem))
		}
		return res
	default:
		return v
	}
}

func ParseResourceManifest(manifestContent, manifestPath string) ([]*ResourceManifest, error) {
	manifestBytes := []byte(manifestContent)

	var result []*ResourceManifest
	decoder := yaml.NewDecoder(bytes.NewReader(manifestBytes))
	for {
		var rawContent interface{}
		err := decoder.Decode(&rawContent)
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return nil, err
		}

		manifest := &ResourceManifest{
			SourceFile: manifestPath,
			Content:    FixupYamlValue(rawContent),
		}

		object, err := manifest.ToYaml()
		if err != nil {
			return nil, err
		}
		serializedObject := []byte(object)

		kubeMeta, err := unmashalAndDetectManifestKind(serializedObject)
		if err != nil {
			return nil, err
		}

		manifest.APIVersion = kubeMeta.APIVersion
		manifest.Kind = kubeMeta.Kind
		manifest.Name = kubeMeta.Metadata.Name
		manifest.Namespace = kubeMeta.Metadata.Namespace

		result = append(result, manifest)
	}
}

// kubernetesMeta defines yaml tags to parse yaml files into metav1 object
type kubernetesMeta struct {
	Kind       string `yaml:"kind"`
	APIVersion string `yaml:"apiVersion"`
	Metadata   struct {
		Name        string            `yaml:"name"`
		Namespace   string            `yaml:"namespace"`
		Annotations map[string]string `yaml:"annotations"`
	} `yaml:"metadata"`
}

// unmashalAndDetectManifestKind parses either Kubernetes or CloudFormation manifests and returns its Kind as well as the
// parsed metadata. Note that only one returned metadata struct will be filled depending on which Kind was detected.
func unmashalAndDetectManifestKind(manifestBytes []byte) (kubernetesMeta, error) {
	var (
		kubeMeta kubernetesMeta
	)

	if err := yaml.Unmarshal(manifestBytes, &kubeMeta); err != nil {
		return kubeMeta, err
	}
	if kubeMeta.Kind != "" && kubeMeta.APIVersion != "" && kubeMeta.Metadata.Name != "" {
		if kubeMeta.Metadata.Namespace == "" {
			kubeMeta.Metadata.Namespace = corev1.NamespaceDefault
		}
		return kubeMeta, nil
	}

	return kubeMeta, errUnsupportedManifest
}
