package provisioner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/jonboulle/clockwork"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/kubernetes"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	kuberesource "k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/disk"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/kubectl/pkg/cmd/apply"
	kubectlutil "k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/openapi"
	openapivalidation "k8s.io/kubectl/pkg/util/openapi/validation"
	"k8s.io/kubectl/pkg/validation"
)

func (p *clusterpyProvisioner) applyKubectl(ctx context.Context, logger *log.Entry, cluster *api.Cluster, renderedManifests []manifestPackage) error {
	token, err := p.tokenSource.Token()
	if err != nil {
		return errors.Wrapf(err, "no valid token")
	}

	for _, m := range renderedManifests {
		logger := logger.WithField("module", m.name)

		args := []string{
			"kubectl",
			"apply",
			fmt.Sprintf("--server=%s", cluster.APIServerURL),
			fmt.Sprintf("--token=%s", token.AccessToken),
			"-f",
			"-",
		}

		newApplyCommand := func() *exec.Cmd {
			cmd := exec.Command(args[0], args[1:]...)
			// prevent kubectl to find the in-cluster config
			cmd.Env = []string{}
			return cmd
		}

		if p.dryRun {
			logger.Debug(newApplyCommand())
		} else {
			applyManifest := func() error {
				cmd := newApplyCommand()
				cmd.Stdin = strings.NewReader(strings.Join(m.manifests, "---\n"))
				_, err := p.execManager.Run(ctx, logger, cmd)
				return err
			}
			err = backoff.Retry(applyManifest, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), maxApplyRetries))
			if err != nil {
				return errors.Wrapf(err, "kubectl apply failed for %s", m.name)
			}
		}
	}

	return nil
}

func (p *clusterpyProvisioner) applyCLM(ctx context.Context, logger *log.Entry, cluster *api.Cluster, renderedManifests []manifestPackage) error {
	restConfig := kubernetes.NewConfig(cluster.APIServerURL, p.tokenSource)

	discoveryCacheDir := path.Join(os.TempDir(), "discovery-cache", cluster.Alias)
	httpCacheDir := path.Join(os.TempDir(), "http-cache", cluster.Alias)

	discoveryInterface, err := disk.NewCachedDiscoveryClientForConfig(restConfig, discoveryCacheDir, httpCacheDir, 5*time.Minute)
	if err != nil {
		return err
	}

	for _, pkg := range renderedManifests {
		for _, manifest := range pkg.manifests {
			err := applySingleManifest(ctx, logger, restConfig, discoveryInterface, manifest)
			if err != nil {
				return errors.Wrapf(err, "apply failed for %s", pkg.name)
			}
		}
	}

	return nil
}

func applySingleManifest(ctx context.Context, logger *log.Entry, restConfig *rest.Config, discoveryInterface discovery.CachedDiscoveryInterface, manifest string) error {
	openAPISchema, err := openapi.NewOpenAPIGetter(discoveryInterface).Get()
	if err != nil {
		return err
	}

	validator := validation.ConjunctiveSchema{
		openapivalidation.NewSchemaValidation(openAPISchema),
		validation.NoDoubleKeySchema{},
	}

	obj, gvk, err := decodeAndValidate(manifest, validator)
	if err != nil {
		return err
	}

	mapping, err := getRESTMappingFor(discoveryInterface, gvk)
	if err != nil {
		return err
	}

	client, err := getRestClientFor(restConfig, gvk)
	if err != nil {
		return err
	}

	asUnstructured, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("expected *unstructured.Unstructured, got %T", obj)
	}

	helper := kuberesource.NewHelper(client, mapping).WithFieldManager(apply.FieldManagerClientSideApply)

	// Get the modified configuration of the object. Embed the result
	// as an annotation in the modified configuration, so that it will appear
	// in the patch sent to the server.
	modified, err := kubectlutil.GetModifiedConfiguration(obj, true, unstructured.UnstructuredJSONScheme)
	if err != nil {
		return err
	}

	current, err := helper.Get(asUnstructured.GetNamespace(), asUnstructured.GetName())
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
		_, err := helper.Create(asUnstructured.GetNamespace(), true, obj)
		if err != nil {
			return err
		}

		logger.Infof("created %s/%s", asUnstructured.GetNamespace(), asUnstructured.GetName())

		return nil
	}

	patcher := newPatcher(mapping, helper, openAPISchema)
	_, _, err = patcher.Patch(current, modified, asUnstructured.GetName(), asUnstructured.GetNamespace(), asUnstructured.GetName(), os.Stderr)
	if err != nil {
		return err
	}

	logger.Infof("patched %s/%s", asUnstructured.GetNamespace(), asUnstructured.GetName())
	return nil
}

func decodeAndValidate(manifest string, validator validation.ConjunctiveSchema) (runtime.Object, *schema.GroupVersionKind, error) {
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
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

func getRESTMappingFor(discoveryInterface discovery.CachedDiscoveryInterface, gvk *schema.GroupVersionKind) (*meta.RESTMapping, error) {
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryInterface)
	expander := restmapper.NewShortcutExpander(mapper, discoveryInterface)

	restMapping, err := expander.RESTMapping(gvk.GroupKind(), gvk.Version)
	if meta.IsNoMatchError(err) {
		mapper.Reset()
		return expander.RESTMapping(gvk.GroupKind(), gvk.Version)
	}
	return restMapping, err
}

func getRestClientFor(restConfig *rest.Config, gvk *schema.GroupVersionKind) (*rest.RESTClient, error) {
	dynamicConfig := dynamic.ConfigFor(restConfig)
	dynamicConfig.GroupVersion = &schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}
	dynamicConfig.APIPath = dynamic.LegacyAPIPathResolverFunc(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	})

	return rest.RESTClientFor(dynamicConfig)
}

func newPatcher(mapping *meta.RESTMapping, helper *kuberesource.Helper, openAPISchema openapi.Resources) *apply.Patcher {
	return &apply.Patcher{
		Mapping:         mapping,
		Helper:          helper,
		Overwrite:       true, // kubectl default
		BackOff:         clockwork.NewRealClock(),
		Force:           false, // kubectl default
		Cascade:         true,  // kubectl default, only used if Force is set to true
		Timeout:         0,     // kubectl default, only used if Force is set to true
		GracePeriod:     -1,    // kubectl default
		ResourceVersion: nil,
		Retries:         5, // kubectl default
		OpenapiSchema:   openAPISchema,
	}
}
