package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/sirupsen/logrus"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/util/command"
	"golang.org/x/oauth2"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

func newConfig(host string, tokenSrc oauth2.TokenSource, ca []byte) *rest.Config {
	config := &rest.Config{
		Host: host,
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &oauth2.Transport{
				Source: tokenSrc,
				Base:   rt,
			}
		},
		Burst: 100,
	}

	if len(ca) > 0 {
		config.TLSClientConfig = rest.TLSClientConfig{
			CAData: ca,
		}
	}

	return config
}

// NewClient initializes a Kubernetes client with the
// specified token source.
func NewClient(host string, tokenSrc oauth2.TokenSource, ca []byte) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(newConfig(host, tokenSrc, ca))
}

// NewDynamicClient initializes a dynamic Kubernetes client with the
// specified token source.
func NewDynamicClient(host string, tokenSrc oauth2.TokenSource, ca []byte) (dynamic.Interface, error) {
	return dynamic.NewForConfig(newConfig(host, tokenSrc, ca))
}

type Labels map[string]string

// String returns a string representation of the labels map.
func (l Labels) String() string {
	labels := make([]string, 0, len(l))
	for key, val := range l {
		labels = append(labels, fmt.Sprintf("%s=%s", key, val))
	}
	return strings.Join(labels, ",")
}

// Resource defines a minimal definition of a kubernetes Resource.
type Resource struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Kind      string `yaml:"kind"`
	Selector  string `yaml:"selector"`
	Labels    Labels `yaml:"labels"`
	HasOwner  *bool  `yaml:"has_owner"`

	// See https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#DeleteOptions
	GracePeriodSeconds *int64                      `yaml:"grace_period_seconds"`
	PropagationPolicy  *metav1.DeletionPropagation `yaml:"propagation_policy"`
}

func (r *Resource) Options() metav1.DeleteOptions {
	return metav1.DeleteOptions{
		GracePeriodSeconds: r.GracePeriodSeconds,
		PropagationPolicy:  r.PropagationPolicy,
	}
}

func (r *Resource) LabelSelector() string {
	if r.Selector != "" {
		return r.Selector
	}
	return metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: r.Labels})
}

func (r *Resource) LogFields() logrus.Fields {
	fields := logrus.Fields{
		"kind": r.Kind,
	}
	if r.Namespace != "" {
		fields["namespace"] = r.Namespace
	}

	fields["selector"] = r.LabelSelector()

	if r.HasOwner != nil {
		fields["has_owner"] = fmt.Sprintf("%t", *r.HasOwner)
	}
	if r.GracePeriodSeconds != nil {
		fields["grace_period_seconds"] = fmt.Sprintf("%d", *r.GracePeriodSeconds)
	}
	if r.PropagationPolicy != nil {
		fields["propagation_policy"] = *r.PropagationPolicy
	}
	return fields
}

type ClientsCollection struct {
	TypedClient   kubernetes.Interface
	DynamicClient dynamic.Interface
	Mapper        meta.RESTMapper
}

// NewClientsCollection returns a collection with dynamic and typed Kubernetes
// clients, configured for the specified host.
//
// caData is an optional CA certificate data for the Kubernetes API server.
func NewClientsCollection(
	host string,
	tokenSrc oauth2.TokenSource,
	caData []byte,
) (*ClientsCollection, error) {
	cfg := newConfig(host, tokenSrc, caData)
	typedClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(typedClient.Discovery()))

	return &ClientsCollection{
		TypedClient:   typedClient,
		DynamicClient: dynamicClient,
		Mapper:        mapper,
	}, nil
}

func (c *ClientsCollection) getResourceClient(kind, namespace string) (dynamic.ResourceInterface, error) {
	gvr, err := c.ResolveKind(kind)
	if err != nil {
		return nil, err
	}
	if namespace != "" {
		return c.DynamicClient.Resource(gvr).Namespace(namespace), nil
	}

	return c.DynamicClient.Resource(gvr), nil

}

func (c *ClientsCollection) ResolveKind(kind string) (schema.GroupVersionResource, error) {
	var gvr schema.GroupVersionResource
	fullySpecifiedGVR, groupResource := schema.ParseResourceArg(kind)

	if fullySpecifiedGVR != nil {
		gvr, _ = c.Mapper.ResourceFor(*fullySpecifiedGVR)
	}
	if gvr.Empty() {
		gvr, _ = c.Mapper.ResourceFor(groupResource.WithVersion(""))
	}
	if gvr.Empty() {
		return schema.GroupVersionResource{}, fmt.Errorf("unable to resolve kind %s (use either name or name.version.group)", kind)
	}
	return gvr, nil
}

func (c *ClientsCollection) Create(ctx context.Context, kind, namespace string, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {

	client, err := c.getResourceClient(kind, namespace)
	if err != nil {
		return nil, err
	}
	return client.Create(ctx, obj, options, subresources...)
}

func (c *ClientsCollection) Update(ctx context.Context, kind, namespace string, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	client, err := c.getResourceClient(kind, namespace)
	if err != nil {
		return nil, err
	}
	return client.Update(ctx, obj, options, subresources...)
}

func (c *ClientsCollection) UpdateStatus(ctx context.Context, kind, namespace string, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	client, err := c.getResourceClient(kind, namespace)
	if err != nil {
		return nil, err
	}
	return client.UpdateStatus(ctx, obj, options)
}

func (c *ClientsCollection) Get(ctx context.Context, kind, namespace, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	client, err := c.getResourceClient(kind, namespace)
	if err != nil {
		return nil, err
	}
	return client.Get(ctx, name, options, subresources...)
}

func (c *ClientsCollection) Exists(ctx context.Context, kind, namespace, name string, options metav1.GetOptions, subresources ...string) (bool, error) {
	_, err := c.Get(ctx, kind, namespace, name, options, subresources...)

	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *ClientsCollection) List(ctx context.Context, kind, namespace string, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	client, err := c.getResourceClient(kind, namespace)
	if err != nil {
		return nil, err
	}
	return client.List(ctx, opts)
}

func (c *ClientsCollection) Watch(ctx context.Context, kind, namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	client, err := c.getResourceClient(kind, namespace)
	if err != nil {
		return nil, err
	}
	return client.Watch(ctx, opts)
}

func (c *ClientsCollection) Patch(ctx context.Context, kind, namespace, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	client, err := c.getResourceClient(kind, namespace)
	if err != nil {
		return nil, err
	}
	return client.Patch(ctx, name, pt, data, options, subresources...)
}

func (c *ClientsCollection) Delete(ctx context.Context, kind, namespace, name string, options metav1.DeleteOptions, subresources ...string) error {
	client, err := c.getResourceClient(kind, namespace)
	if err != nil {
		return err
	}
	return client.Delete(ctx, name, options, subresources...)
}

func (c *ClientsCollection) DeleteCollection(ctx context.Context, kind, namespace string, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	client, err := c.getResourceClient(kind, namespace)
	if err != nil {
		return err
	}
	return client.DeleteCollection(ctx, options, listOptions)
}

func (c *ClientsCollection) deleteIfFound(ctx context.Context, logger *logrus.Entry, kind, namespace, name string, options metav1.DeleteOptions) error {
	err := c.Delete(ctx, kind, namespace, name, options)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Infof("Skipping deletion of %s %s: resource not found", kind, name)
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to delete: %w", err)
	}

	logger.Infof("%s %s deleted", kind, name)
	return nil
}

func (c *ClientsCollection) DeleteResource(ctx context.Context, logger *logrus.Entry, deletion *Resource) error {
	logger = logger.WithFields(deletion.LogFields())

	// identify the resource to be deleted either by name, selector or labels.
	// Only one of them must be defined.
	resourceIdentifiers := 0
	if deletion.Name != "" {
		resourceIdentifiers++
	}
	if deletion.Selector != "" {
		resourceIdentifiers++
	}
	if len(deletion.Labels) > 0 {
		resourceIdentifiers++
	}

	if resourceIdentifiers == 0 {
		return fmt.Errorf("either 'name', 'selector' or 'labels' must be specified to identify a resource")
	} else if resourceIdentifiers > 1 {
		return fmt.Errorf("only one of 'name', 'selector' or 'labels' must be specified to identify a resource")
	}

	if deletion.HasOwner != nil && deletion.Selector == "" && len(deletion.Labels) == 0 {
		return fmt.Errorf("'has_owner' requires 'selector' or 'labels' to be specified")
	}

	if deletion.Name != "" {
		err := c.overrideDeletionProtection(ctx, logger, deletion.Kind, deletion.Namespace, deletion.Name)
		if err != nil {
			return err
		}
		return c.deleteIfFound(ctx, logger, deletion.Kind, deletion.Namespace, deletion.Name, deletion.Options())
	}

	items, err := c.ListResources(ctx, deletion)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		logger.Infof("No matching %s resources found", deletion.Kind)
	}

	for _, item := range items {
		err := c.overrideDeletionProtection(ctx, logger, deletion.Kind, deletion.Namespace, deletion.Name)
		if err != nil {
			return err
		}
		err = c.deleteIfFound(ctx, logger, deletion.Kind, item.GetNamespace(), item.GetName(), deletion.Options())
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *ClientsCollection) ListResources(ctx context.Context, rsrc *Resource) ([]unstructured.Unstructured, error) {
	items, err := c.List(ctx, rsrc.Kind, rsrc.Namespace, metav1.ListOptions{LabelSelector: rsrc.LabelSelector()})
	if err != nil {
		return nil, err
	}

	if rsrc.HasOwner != nil {
		var result []unstructured.Unstructured
		for _, item := range items.Items {
			itemHasOwner := len(item.GetOwnerReferences()) > 0
			if *rsrc.HasOwner == itemHasOwner {
				result = append(result, item)
			}
		}
		return result, nil
	}
	return items.Items, nil
}

func (c *ClientsCollection) overrideDeletionProtection(ctx context.Context, logger *logrus.Entry, kind, namespace, name string) error {
	if kind != "Namespace" {
		// only namespace resources are currently supported
		return nil
	}

	resource, err := c.Get(ctx, kind, namespace, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Infof("Skipping delete annotation of %s %s: resource not found", kind, name)
			return nil
		}
		return err
	}
	annotations := resource.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["zalando.org/delete-date"] = time.Now().Format("2006-01-02")
	annotations["zalando.org/delete-namespace"] = name
	resource.SetAnnotations(annotations)
	_, err = c.Update(ctx, kind, namespace, resource, metav1.UpdateOptions{})
	return err
}

type KubeCTLRunner struct {
	execManager *command.ExecManager
	tokenSource oauth2.TokenSource
	logger      *logrus.Entry
	k8sAPIURL   string
	clusterCA   []byte
	maxRetries  uint64
}

func NewKubeCTLRunner(
	e *command.ExecManager,
	ts oauth2.TokenSource,
	l *logrus.Entry,
	k8sAPIURL string,
	maxRetries uint64,
	clusterCA []byte,
) *KubeCTLRunner {
	return &KubeCTLRunner{
		execManager: e,
		tokenSource: ts,
		logger:      l,
		k8sAPIURL:   k8sAPIURL,
		clusterCA:   clusterCA,
		maxRetries:  maxRetries,
	}
}

func (k *KubeCTLRunner) KubectlExecute(ctx context.Context, args []string, stdin string, dryRun bool) (string, error) {
	token, err := k.tokenSource.Token()
	if err != nil {
		return "", err
	}

	kubeCtlOpts := []string{
		fmt.Sprintf("--server=%s", k.k8sAPIURL),
		fmt.Sprintf("--token=%s", token.AccessToken),
	}

	// Use custom CA if provided
	if len(k.clusterCA) != 0 {
		tmpfile, err := os.CreateTemp("", "cluster_*.ca.crt")
		if err != nil {
			return "", err
		}
		defer os.Remove(tmpfile.Name())

		_, err = tmpfile.Write(k.clusterCA)
		if err != nil {
			return "", err
		}

		err = tmpfile.Close()
		if err != nil {
			return "", err
		}

		kubeCtlOpts = append(
			kubeCtlOpts,
			fmt.Sprintf("--certificate-authority=%s", tmpfile.Name()),
		)
	}

	args = append(kubeCtlOpts, args...)
	if stdin != "" {
		args = append(args, "-f", "-")
	}

	newCommand := func() *exec.Cmd {
		cmd := exec.Command("kubectl", args[0:]...)
		// prevent kubectl to find the in-cluster config
		cmd.Env = []string{}
		return cmd
	}
	if dryRun {
		k.logger.Debug(newCommand())
		return "", nil
	}
	var output string
	applyManifest := func() error {
		cmd := newCommand()
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		output, err = k.execManager.Run(ctx, k.logger, cmd)
		return err
	}
	err = backoff.Retry(applyManifest, backoff.WithMaxRetries(backoff.NewExponentialBackOff(), k.maxRetries))
	if err != nil {
		return "", err
	}
	return output, nil
}

func ParseTaint(taintSpec string) (*v1.Taint, error) {
	var taint v1.Taint
	allowedTaints := []v1.TaintEffect{
		v1.TaintEffectNoSchedule, v1.TaintEffectNoExecute, v1.TaintEffectPreferNoSchedule,
	}
	parts := strings.Split(taintSpec, ":")
	switch len(parts) {
	case 1:
		taint.Key = parts[0]
		if strings.Contains(taint.Key, "=") {
			return nil, fmt.Errorf("invalid taint spec: %v", taintSpec)
		}
	case 2:
		taint.Effect = v1.TaintEffect(parts[1])
		if !util.Contains(allowedTaints, taint.Effect) {
			return nil, fmt.Errorf("invalid taint spec: %v", taintSpec)
		}
		partsKV := strings.Split(parts[0], "=")
		if len(partsKV) > 2 {
			return nil, fmt.Errorf("invalid taint spec: %v", taintSpec)
		}
		taint.Key = partsKV[0]
		if len(partsKV) == 2 {
			taint.Value = partsKV[1]
		}
	default:
		return nil, fmt.Errorf("invalid taint spec: %v", taintSpec)
	}

	return &taint, nil
}
