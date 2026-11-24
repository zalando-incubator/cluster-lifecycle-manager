package reconciler

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	sourceInterval  = "10m"
	releaseInterval = "10m"

	// Keys of the outputs Secret written by the Terraform resource. The
	// infrastructure module must provide at least these.
	OutputClusterID     = "cluster_id"
	OutputEndpoint      = "endpoint"
	OutputCACertificate = "ca_certificate"
	OutputOIDCIssuer    = "oidc_issuer"

	// KubeconfigKey is the key of the kubeconfig Secret.
	KubeconfigKey = "value"

	labelManagedBy = "app.kubernetes.io/managed-by"
)

// SecretRef points at a key of a Secret.
type SecretRef struct {
	Namespace string
	Name      string
	Key       string
}

// RenderConfig is the part of the configuration that decides how resources
// look. Everything provider specific is configuration.
type RenderConfig struct {
	// Namespace of all created resources on the host cluster.
	Namespace string
	// Name is the component name, used for labels and as field manager.
	Name string

	// RunnerServiceAccount is the service account of the Terraform runner pods.
	RunnerServiceAccount string
	// Provider credentials mounted into the runner pods.
	RunnerCredentialsSecret    string
	RunnerCredentialsEnv       string
	RunnerCredentialsMountPath string
	RunnerCredentialsFile      string

	// ClusterToken is the Secret holding the token used in the kubeconfig
	// of the created cluster.
	ClusterToken SecretRef

	// OCIPullSecret is the optional name of a Secret with registry credentials
	// for the module repository.
	OCIPullSecret string
}

// Names of the resources of one cluster.
type names struct {
	provisionSource string // OCIRepository
	terraform       string
	outputs         string // Secret written by tofu-controller
	kubeconfig      string // Secret
	deliverySource  string // HelmRepository
	release         string // HelmRelease
	status          string // ConfigMap
}

func resourceNames(spec *ClusterSpec) names {
	return names{
		provisionSource: "provision-" + spec.Alias,
		terraform:       "cluster-" + spec.Alias,
		outputs:         "tfout-" + spec.Alias,
		kubeconfig:      "kubeconfig-" + spec.Alias,
		deliverySource:  "delivery-" + spec.Alias,
		release:         spec.DeliveryRelease + "-" + spec.Alias,
		status:          "status-" + spec.Alias,
	}
}

// validateNames checks that all derived names and the label value are valid
// and returns readable errors.
func (c RenderConfig) validateNames(spec *ClusterSpec) error {
	if errs := validation.IsValidLabelValue(spec.Alias); len(errs) > 0 || spec.Alias == "" {
		return fmt.Errorf("cluster alias %q is not usable as a label value: %v", spec.Alias, errs)
	}
	n := resourceNames(spec)
	for _, name := range []string{n.provisionSource, n.terraform, n.outputs, n.kubeconfig, n.deliverySource, n.release, n.status} {
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			return fmt.Errorf("cluster %s: derived resource name %q is invalid: %v", spec.Alias, name, errs)
		}
	}
	// Helm release names are limited to 53 characters.
	if len(n.release) > 53 {
		return fmt.Errorf("cluster %s: release name %q is longer than 53 characters", spec.Alias, n.release)
	}
	return nil
}

func (c RenderConfig) labels(spec *ClusterSpec) map[string]interface{} {
	return map[string]interface{}{
		labelManagedBy:      c.Name,
		c.Name + "/cluster": spec.Alias,
	}
}

func (c RenderConfig) object(apiVersion, kind, name string, spec *ClusterSpec) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": c.Namespace,
			"labels":    c.labels(spec),
		},
	}}
}

// ProvisionSource renders the OCIRepository holding the infrastructure
// module.
func (c RenderConfig) ProvisionSource(spec *ClusterSpec) *unstructured.Unstructured {
	o := c.object("source.toolkit.fluxcd.io/v1", "OCIRepository", resourceNames(spec).provisionSource, spec)
	s := map[string]interface{}{
		"interval": sourceInterval,
		"url":      spec.ProvisionModuleRepo,
		"ref":      map[string]interface{}{"tag": spec.ProvisionModuleRef},
	}
	if c.OCIPullSecret != "" {
		s["secretRef"] = map[string]interface{}{"name": c.OCIPullSecret}
	}
	o.Object["spec"] = s
	return o
}

// Terraform renders the Terraform resource that creates the cluster.
func (c RenderConfig) Terraform(spec *ClusterSpec) *unstructured.Unstructured {
	n := resourceNames(spec)
	o := c.object("infra.contrib.fluxcd.io/v1alpha2", "Terraform", n.terraform, spec)

	s := map[string]interface{}{
		"interval": sourceInterval,
		"path":     spec.ProvisionModulePath,
		// Deleting the resource must never destroy the cluster.
		"destroyResourcesOnDeletion": false,
		"sourceRef": map[string]interface{}{
			"kind":      "OCIRepository",
			"name":      n.provisionSource,
			"namespace": c.Namespace,
		},
		"serviceAccountName": c.RunnerServiceAccount,
		"vars":               spec.TerraformVars(),
		"writeOutputsToSecret": map[string]interface{}{
			"name": n.outputs,
			"outputs": []interface{}{
				OutputClusterID, OutputEndpoint, OutputCACertificate, OutputOIDCIssuer,
			},
		},
		"runnerPodTemplate": map[string]interface{}{
			"spec": map[string]interface{}{
				"volumes": []interface{}{
					map[string]interface{}{
						"name":   "provider-credentials",
						"secret": map[string]interface{}{"secretName": c.RunnerCredentialsSecret},
					},
				},
				"env": []interface{}{
					map[string]interface{}{
						"name":  c.RunnerCredentialsEnv,
						"value": c.RunnerCredentialsMountPath + "/" + c.RunnerCredentialsFile,
					},
				},
				"volumeMounts": []interface{}{
					map[string]interface{}{
						"name":      "provider-credentials",
						"mountPath": c.RunnerCredentialsMountPath,
						"readOnly":  true,
					},
				},
			},
		},
	}
	// "auto" approves every plan; for manual approval the field stays unset
	// and the plan is approved on the resource.
	if spec.ProvisionApprove == ApproveAuto {
		s["approvePlan"] = "auto"
	}
	o.Object["spec"] = s
	return o
}

// Status renders the status ConfigMap without data. The data is owned by the
// status reporting, so applying this object never overwrites it.
func (c RenderConfig) Status(spec *ClusterSpec) *unstructured.Unstructured {
	return c.object("v1", "ConfigMap", resourceNames(spec).status, spec)
}

// Kubeconfig renders the Secret holding the kubeconfig of the created cluster.
func (c RenderConfig) Kubeconfig(spec *ClusterSpec, kubeconfig []byte) *unstructured.Unstructured {
	o := c.object("v1", "Secret", resourceNames(spec).kubeconfig, spec)
	o.Object["type"] = "Opaque"
	o.Object["data"] = map[string]interface{}{KubeconfigKey: base64Encode(kubeconfig)}
	return o
}

// DeliverySource renders the HelmRepository of the chart.
func (c RenderConfig) DeliverySource(spec *ClusterSpec) *unstructured.Unstructured {
	o := c.object("source.toolkit.fluxcd.io/v1", "HelmRepository", resourceNames(spec).deliverySource, spec)
	o.Object["spec"] = map[string]interface{}{
		"type":     spec.DeliveryRepoType,
		"interval": sourceInterval,
		"url":      spec.DeliveryRepoURL,
	}
	return o
}

// Release renders the HelmRelease that installs the chart on the created
// cluster.
func (c RenderConfig) Release(spec *ClusterSpec) *unstructured.Unstructured {
	n := resourceNames(spec)
	o := c.object("helm.toolkit.fluxcd.io/v2", "HelmRelease", n.release, spec)
	s := map[string]interface{}{
		"interval":        releaseInterval,
		"releaseName":     spec.DeliveryRelease,
		"targetNamespace": spec.DeliveryTargetNamespace,
		"chart": map[string]interface{}{
			"spec": map[string]interface{}{
				"chart":   spec.DeliveryChart,
				"version": spec.DeliveryChartVersion,
				"sourceRef": map[string]interface{}{
					"kind":      "HelmRepository",
					"name":      n.deliverySource,
					"namespace": c.Namespace,
				},
			},
		},
		"kubeConfig": map[string]interface{}{
			"secretRef": map[string]interface{}{"name": n.kubeconfig},
		},
		"install": map[string]interface{}{"createNamespace": true},
	}
	if len(spec.DeliveryValues) > 0 {
		s["values"] = spec.HelmValues()
	}
	o.Object["spec"] = s
	return o
}
