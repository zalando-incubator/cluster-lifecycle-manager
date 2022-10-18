package kubernetes

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/restmapper"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func newConfig(host string, tokenSrc oauth2.TokenSource) *rest.Config {
	return &rest.Config{
		Host: host,
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &oauth2.Transport{
				Source: tokenSrc,
				Base:   rt,
			}
		},
		Burst: 100,
	}
}

// NewClient initializes a Kubernetes client with the
// specified token source.
func NewClient(host string, tokenSrc oauth2.TokenSource) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(newConfig(host, tokenSrc))
}

// NewDynamicClient initializes a dynamic Kubernetes client with the
// specified token source.
func NewDynamicClient(host string, tokenSrc oauth2.TokenSource) (dynamic.Interface, error) {
	return dynamic.NewForConfig(newConfig(host, tokenSrc))
}

func InitClients(host string, tokenSrc oauth2.TokenSource) (kubernetes.Interface, dynamic.Interface, *restmapper.DeferredDiscoveryRESTMapper, error) {
	cfg := newConfig(host, tokenSrc)
	typedClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(typedClient.Discovery()))

	return typedClient, dynamicClient, mapper, nil
}

func ListResources(ctx context.Context, iface dynamic.ResourceInterface, deletion *Resource) ([]unstructured.Unstructured, error) {
	items, err := iface.List(ctx, v1.ListOptions{
		LabelSelector: v1.FormatLabelSelector(&v1.LabelSelector{
			MatchLabels: deletion.Labels,
		}),
	})
	if err != nil {
		return nil, err
	}

	if deletion.HasOwner != nil {
		var result []unstructured.Unstructured
		for _, item := range items.Items {
			itemHasOwner := len(item.GetOwnerReferences()) > 0
			if *deletion.HasOwner == itemHasOwner {
				result = append(result, item)
			}
		}
		return result, nil
	}
	return items.Items, nil
}

func DeleteResource(ctx context.Context, iface dynamic.ResourceInterface, logger *logrus.Entry, kind, name string, options v1.DeleteOptions) error {
	err := iface.Delete(ctx, name, options)
	if err != nil && errors.IsNotFound(err) {
		logger.Infof("Skipping deletion of %s %s: resource not found", kind, name)
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to delete: %w", err)
	}

	logger.Infof("%s %s deleted", kind, name)
	return nil
}

func ProcessDeletion(ctx context.Context, client dynamic.Interface, mapper meta.RESTMapper, logger *logrus.Entry, deletion *Resource) error {
	// Figure out the GVR
	gvr, err := ResolveKind(mapper, deletion.Kind)
	if err != nil {
		return err
	}
	return PerformDeletion(ctx, logger, client, gvr, deletion)
}

func PerformDeletion(ctx context.Context, logger *logrus.Entry, client dynamic.Interface, gvr schema.GroupVersionResource, deletion *Resource) error {
	logger = logger.WithFields(deletion.LogFields())

	// identify the resource to be deleted either by name or
	// labels. name AND labels cannot be defined at the same time,
	// but one of them MUST be defined.
	if deletion.Name != "" && len(deletion.Labels) > 0 {
		return fmt.Errorf("only one of 'name' or 'labels' must be specified")
	}

	if deletion.Name == "" && len(deletion.Labels) == 0 {
		return fmt.Errorf("either name or labels must be specified to identify a resource")
	}

	if deletion.HasOwner != nil && len(deletion.Labels) == 0 {
		return fmt.Errorf("'has_owner' requires 'labels' to be specified")
	}

	var iface dynamic.ResourceInterface
	if deletion.Namespace != "" {
		iface = client.Resource(gvr).Namespace(deletion.Namespace)
	} else {
		iface = client.Resource(gvr)
	}

	if deletion.Name != "" {
		return DeleteResource(ctx, iface, logger, deletion.Kind, deletion.Name, deletion.Options())
	} else if len(deletion.Labels) > 0 {
		items, err := ListResources(ctx, iface, deletion)
		if err != nil {
			return err
		}

		if len(items) == 0 {
			logger.Infof("No matching %s resources found", deletion.Kind)
		}

		for _, item := range items {
			err = DeleteResource(ctx, iface, logger, deletion.Kind, item.GetName(), deletion.Options())
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func ResolveKind(mapper meta.RESTMapper, kind string) (schema.GroupVersionResource, error) {
	var gvr schema.GroupVersionResource
	fullySpecifiedGVR, groupResource := schema.ParseResourceArg(kind)

	if fullySpecifiedGVR != nil {
		gvr, _ = mapper.ResourceFor(*fullySpecifiedGVR)
	}
	if gvr.Empty() {
		gvr, _ = mapper.ResourceFor(groupResource.WithVersion(""))
	}
	if gvr.Empty() {
		return schema.GroupVersionResource{}, fmt.Errorf("unable to resolve kind %s (use either name or name.version.group)", kind)
	}
	return gvr, nil
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
	Labels    Labels `yaml:"labels"`
	HasOwner  *bool  `yaml:"has_owner"`

	// See https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#DeleteOptions
	GracePeriodSeconds *int64                  `yaml:"grace_period_seconds"`
	PropagationPolicy  *v1.DeletionPropagation `yaml:"propagation_policy"`
}

func (r *Resource) Options() v1.DeleteOptions {
	return v1.DeleteOptions{
		GracePeriodSeconds: r.GracePeriodSeconds,
		PropagationPolicy:  r.PropagationPolicy,
	}
}

func (r *Resource) LogFields() logrus.Fields {
	fields := logrus.Fields{
		"kind": r.Kind,
	}
	if r.Namespace != "" {
		fields["namespace"] = r.Namespace
	}
	if len(r.Labels) > 0 {
		fields["selector"] = v1.FormatLabelSelector(&v1.LabelSelector{MatchLabels: r.Labels})
	}
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
