package reconciler

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Stage is how far the reconciler got for a cluster.
type Stage string

const (
	// StageProvisioning: the infrastructure resources are applied, but the
	// cluster is not ready yet, so nothing is delivered to it.
	StageProvisioning Stage = "Provisioning"
	// StageDelivering: the cluster exists, the kubeconfig is in place and
	// the delivery resources are applied.
	StageDelivering Stage = "Delivering"
)

// ObjectResult is the result of applying one object.
type ObjectResult struct {
	Kind string
	Name string
	ApplyResult
}

// Result is the result of reconciling one cluster.
type Result struct {
	Cluster string
	Stage   Stage
	// Waiting says what the reconciler waits for, when it stopped early.
	Waiting string
	Objects []ObjectResult
}

// Reconciler applies the resources of clusters.
type Reconciler struct {
	Client Client
	Config RenderConfig
	DryRun bool
}

func (r *Reconciler) apply(ctx context.Context, res *Result, objs ...*unstructured.Unstructured) error {
	for _, obj := range objs {
		ar, err := r.Client.Apply(ctx, obj, r.DryRun)
		if err != nil {
			return err
		}
		res.Objects = append(res.Objects, ObjectResult{Kind: obj.GetKind(), Name: obj.GetName(), ApplyResult: ar})
	}
	return nil
}

// Reconcile applies the resources of one cluster in order:
//
//  1. OCIRepository, Terraform and the status ConfigMap, always.
//  2. The kubeconfig Secret, once the outputs Secret of the Terraform resource
//     exists with the cluster endpoint and CA certificate.
//  3. HelmRepository and HelmRelease, only after the kubeconfig exists. Nothing
//     is delivered to a cluster that is not running and reachable.
//
// The kubeconfig is rebuilt on every call, so it follows the renewal of the
// token. Applying an unchanged object changes nothing.
func (r *Reconciler) Reconcile(ctx context.Context, spec *ClusterSpec) (*Result, error) {
	if err := r.Config.validateNames(spec); err != nil {
		return nil, err
	}
	n := resourceNames(spec)
	res := &Result{Cluster: spec.Alias, Stage: StageProvisioning}

	if err := r.apply(ctx, res, r.Config.ProvisionSource(spec), r.Config.Terraform(spec), r.Config.Status(spec)); err != nil {
		return res, err
	}

	outputs, err := r.Client.Get(ctx, gvrSecret, r.Config.Namespace, n.outputs)
	if err != nil {
		return res, fmt.Errorf("get outputs Secret %s: %w", n.outputs, err)
	}
	if outputs == nil {
		res.Waiting = fmt.Sprintf("outputs Secret %s does not exist yet", n.outputs)
		return res, nil
	}
	endpoint, _ := secretValue(outputs, OutputEndpoint)
	ca, _ := secretValue(outputs, OutputCACertificate)
	if endpoint == "" || ca == "" {
		res.Waiting = fmt.Sprintf("outputs Secret %s has no %s or %s yet", n.outputs, OutputEndpoint, OutputCACertificate)
		return res, nil
	}

	tokenRef := r.Config.ClusterToken
	tokenSecret, err := r.Client.Get(ctx, gvrSecret, tokenRef.Namespace, tokenRef.Name)
	if err != nil {
		return res, fmt.Errorf("get token Secret %s/%s: %w", tokenRef.Namespace, tokenRef.Name, err)
	}
	if tokenSecret == nil {
		return res, fmt.Errorf("token Secret %s/%s does not exist", tokenRef.Namespace, tokenRef.Name)
	}
	token, _ := secretValue(tokenSecret, tokenRef.Key)
	if token == "" {
		return res, fmt.Errorf("token Secret %s/%s has no key %q", tokenRef.Namespace, tokenRef.Name, tokenRef.Key)
	}

	kubeconfig, err := BuildKubeconfig(spec.Alias, endpoint, ca, token)
	if err != nil {
		return res, fmt.Errorf("build kubeconfig for %s: %w", spec.Alias, err)
	}
	if err := r.apply(ctx, res, r.Config.Kubeconfig(spec, kubeconfig)); err != nil {
		return res, err
	}

	if err := r.apply(ctx, res, r.Config.DeliverySource(spec), r.Config.Release(spec)); err != nil {
		return res, err
	}
	res.Stage = StageDelivering
	return res, nil
}

// secretValue returns the decoded value of a key of a Secret object.
func secretValue(secret *unstructured.Unstructured, key string) (string, bool) {
	if data, ok := secret.Object["data"].(map[string]interface{}); ok {
		if v, ok := data[key].(string); ok {
			decoded, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return "", false
			}
			return strings.TrimSpace(string(decoded)), true
		}
	}
	if data, ok := secret.Object["stringData"].(map[string]interface{}); ok {
		if v, ok := data[key].(string); ok {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}
