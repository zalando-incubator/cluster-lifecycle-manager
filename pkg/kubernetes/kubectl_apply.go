package kubernetes

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/jonboulle/clockwork"
	"github.com/sirupsen/logrus"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/kubectl/pkg/cmd/apply"
	kubectlutil "k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/openapi"
	"k8s.io/kubectl/pkg/validation"
)

type Applier struct {
	restConfig *rest.Config
	discovery  discovery.CachedDiscoveryInterface
	kubeClient kubernetes.Interface
	maxRetries uint64
}

func NewApplier(restConfig *rest.Config, maxRetries uint64) (*Applier, error) {
	discoveryCacheDir, err := os.MkdirTemp("", "discovery-cache")
	if err != nil {
		return nil, err
	}
	httpCacheDir, err := os.MkdirTemp("", "http-cache")
	if err != nil {
		return nil, err
	}

	discoveryInterface, err := disk.NewCachedDiscoveryClientForConfig(restConfig, discoveryCacheDir, httpCacheDir, 5*time.Minute)
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return &Applier{
		restConfig: restConfig,
		discovery:  discoveryInterface,
		kubeClient: client,
		maxRetries: maxRetries,
	}, nil
}

func (d *Applier) Apply(ctx context.Context, manifest *ResourceManifest) error {
	return d.applyKubernetesManifest(ctx, manifest)
}

func (d *Applier) applyKubernetesManifest(ctx context.Context, manifest *ResourceManifest) error {
	validator := validation.ConjunctiveSchema{
		validation.NewSchemaValidation(d),
		validation.NoDoubleKeySchema{},
	}

	obj, gvk, err := decodeAndValidate(manifest, validator)
	if err != nil {
		return err
	}

	mapping, err := d.getRESTMappingFor(gvk)
	if err != nil {
		return err
	}

	client, err := d.getRestClientFor(gvk)
	if err != nil {
		return err
	}

	applyManifest := func() error {
		return d.applyKubernetesResource(ctx, manifest, obj, client, mapping)
	}
	return backoff.Retry(applyManifest, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), d.maxRetries))
}

func (d *Applier) applyKubernetesResource(_ context.Context, manifest *ResourceManifest, obj runtime.Object, client *rest.RESTClient, mapping *meta.RESTMapping) error {
	helper := resource.NewHelper(client, mapping).WithFieldManager(apply.FieldManagerClientSideApply)

	// Get the modified configuration of the object. Embed the result
	// as an annotation in the modified configuration, so that it will appear
	// in the patch sent to the server.
	modified, err := kubectlutil.GetModifiedConfiguration(obj, true, unstructured.UnstructuredJSONScheme)
	if err != nil {
		return err
	}

	current, err := helper.Get(manifest.Namespace, manifest.Name)
	if err != nil {
		if !apiErrors.IsNotFound(err) {
			return err
		}

		// Create the resource if it doesn't exist
		// First, update the annotation used by kubectl apply
		if err := kubectlutil.CreateApplyAnnotation(obj, unstructured.UnstructuredJSONScheme); err != nil {
			return err
		}

		// Then create the resource and skip the three-way merge
		_, err := helper.Create(manifest.Namespace, true, obj)
		if err != nil {
			return err
		}

		return nil
	}

	patcher := newPatcher(mapping, helper)
	_, _, err = patcher.Patch(current, modified, manifest.Name, manifest.Namespace, manifest.Name, os.Stderr)
	if err != nil {
		return err
	}

	return nil
}

func newPatcher(mapping *meta.RESTMapping, helper *resource.Helper) *apply.Patcher {
	return &apply.Patcher{
		Mapping:           mapping,
		Helper:            helper,
		Overwrite:         true, // kubectl default
		BackOff:           clockwork.NewRealClock(),
		Force:             false,                              // kubectl default
		CascadingStrategy: metav1.DeletePropagationBackground, // kubectl default, only used if Force is set to true
		Timeout:           0,                                  // kubectl default, only used if Force is set to true
		GracePeriod:       -1,                                 // kubectl default
		ResourceVersion:   nil,
		Retries:           5, // kubectl default
	}
}

func decodeAndValidate(manifest *ResourceManifest, validator validation.ConjunctiveSchema) (runtime.Object, *schema.GroupVersionKind, error) {
	manifestYaml, err := manifest.ToYaml()
	if err != nil {
		return nil, nil, err
	}

	dec := kubeyaml.NewYAMLOrJSONDecoder(strings.NewReader(manifestYaml), 4096)
	ext := runtime.RawExtension{}
	if err := dec.Decode(&ext); err != nil {
		return nil, nil, err
	}

	if err := validator.ValidateBytes(ext.Raw); err != nil {
		return nil, nil, err
	}

	decoder := unstructured.UnstructuredJSONScheme
	return decoder.Decode(ext.Raw, nil, nil)
}

func (d *Applier) getRESTMappingFor(gvk *schema.GroupVersionKind) (*meta.RESTMapping, error) {
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(d.discovery)
	expander := restmapper.NewShortcutExpander(mapper, d.discovery, func(a string) {
		logrus.Warn(a)
	})

	restMapping, err := expander.RESTMapping(gvk.GroupKind(), gvk.Version)
	if meta.IsNoMatchError(err) {
		mapper.Reset()
		return expander.RESTMapping(gvk.GroupKind(), gvk.Version)
	}
	return restMapping, err
}

func (d *Applier) getRestClientFor(gvk *schema.GroupVersionKind) (*rest.RESTClient, error) {
	dynamicConfig := dynamic.ConfigFor(d.restConfig)
	dynamicConfig.GroupVersion = &schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}
	dynamicConfig.APIPath = dynamic.LegacyAPIPathResolverFunc(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	})

	return rest.RESTClientFor(dynamicConfig)
}

// OpenAPISchema implements openapi.OpenAPIResourcesGetter
func (d *Applier) OpenAPISchema() (openapi.Resources, error) {
	return openapi.NewOpenAPIParser(openapi.NewOpenAPIGetter(d.discovery)).Parse()
}
