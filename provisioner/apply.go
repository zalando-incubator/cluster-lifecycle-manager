package provisioner

import (
	"context"
	"fmt"
	"io"
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

const (
	crdApplyDelay = 5 * time.Second
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

type applier struct {
	restConfig         *rest.Config
	discoveryInterface discovery.CachedDiscoveryInterface
	openAPISchema      openapi.Resources
	validator          validation.ConjunctiveSchema

	schemaValid bool
}

func newApplier(restConfig *rest.Config, discoveryInterface discovery.CachedDiscoveryInterface) *applier {
	return &applier{
		restConfig:         restConfig,
		discoveryInterface: discoveryInterface,
	}
}

func (a *applier) decodeAndValidate(decoder *yaml.YAMLOrJSONDecoder) (runtime.Object, *schema.GroupVersionKind, error) {
	if err := a.ensureValid(); err != nil {
		return nil, nil, err
	}

	ext := runtime.RawExtension{}
	if err := decoder.Decode(&ext); err != nil {
		return nil, nil, err
	}

	if err := a.validator.ValidateBytes(ext.Raw); err != nil {
		return nil, nil, err
	}

	return unstructured.UnstructuredJSONScheme.Decode(ext.Raw, nil, nil)
}

func (a *applier) getOpenAPISchema() (openapi.Resources, error) {
	if err := a.ensureValid(); err != nil {
		return nil, err
	}
	return a.openAPISchema, nil
}

func (a *applier) ensureValid() error {
	if a.schemaValid {
		return nil
	}

	openAPISchema, err := openapi.NewOpenAPIGetter(a.discoveryInterface).Get()
	if err != nil {
		return err
	}
	a.openAPISchema = openAPISchema
	a.validator = validation.ConjunctiveSchema{
		openapivalidation.NewSchemaValidation(openAPISchema),
		validation.NoDoubleKeySchema{},
	}
	a.schemaValid = true
	return nil
}

func (a *applier) invalidateSchema() {
	a.schemaValid = false
}

func (a *applier) applySingleManifest(logger *log.Entry, manifest string) error {
	reader := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	for {
		err := a.applyNextResource(logger, reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (a *applier) applyNextResource(logger *log.Entry, decoder *yaml.YAMLOrJSONDecoder) error {
	openAPISchema, err := a.getOpenAPISchema()
	if err != nil {
		return err
	}

	obj, gvk, err := a.decodeAndValidate(decoder)
	if err != nil {
		return err
	}

	logger = logger.WithField("kind", gvk.Kind)

	mapping, err := getRESTMappingFor(a.discoveryInterface, gvk)
	if err != nil {
		return err
	}

	client, err := getRestClientFor(a.restConfig, gvk)
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

	if asUnstructured.GetNamespace() != "" {
		logger = logger.WithField("namespace", asUnstructured.GetNamespace())
	}

	description := objectDescription(gvk, asUnstructured.GetName())

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

		logger.Infof("%s created", description)
		return nil
	}

	patcher := newPatcher(mapping, helper, openAPISchema)
	patch, _, err := patcher.Patch(current, modified, "manifest.yaml", asUnstructured.GetNamespace(), asUnstructured.GetName(), os.Stderr)
	if err != nil {
		return err
	}

	if string(patch) == "{}" {
		logger.Infof("%s unchanged", description)
	} else {
		logger.Infof("%s configured", description)
	}

	// Hack to make sure that the CRD is applied by the API server
	if gvk.Kind == "CustomResourceDefinition" {
		time.Sleep(crdApplyDelay)
		a.invalidateSchema()
	}

	return nil
}

func (p *clusterpyProvisioner) applyCLM(ctx context.Context, logger *log.Entry, cluster *api.Cluster, renderedManifests []manifestPackage) error {
	restConfig := kubernetes.NewConfig(cluster.APIServerURL, p.tokenSource)
	restConfig.QPS = 100

	discoveryCacheDir := path.Join(os.TempDir(), "discovery-cache", cluster.Alias)
	httpCacheDir := path.Join(os.TempDir(), "http-cache", cluster.Alias)

	discoveryInterface, err := disk.NewCachedDiscoveryClientForConfig(restConfig, discoveryCacheDir, httpCacheDir, 5*time.Minute)
	if err != nil {
		return err
	}

	applier := newApplier(restConfig, discoveryInterface)

	for _, pkg := range renderedManifests {
		logger := logger.WithField("module", pkg.name)

		for _, manifest := range pkg.manifests {
			if err := ctx.Err(); err != nil {
				return err
			}

			err = applier.applySingleManifest(logger, manifest)
			if err != nil {
				return errors.Wrapf(err, "apply failed for %s", pkg.name)
			}
		}
	}

	return nil
}

func objectDescription(gvk *schema.GroupVersionKind, name string) string {
	if len(gvk.Group) == 0 {
		return fmt.Sprintf("%s/%s", strings.ToLower(gvk.Kind), name)
	}

	return fmt.Sprintf("%s.%s/%s", strings.ToLower(gvk.Kind), gvk.Group, name)
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
