// Package reconciler turns cluster registry entries into the desired state of
// an infrastructure and delivery toolchain running on a host cluster.
package reconciler

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/decrypter"
)

// Config items understood by the reconciler.
const (
	ConfigItemProvisionModuleRepo = "provision_module_repo"
	ConfigItemProvisionModuleRef  = "provision_module_ref"
	ConfigItemProvisionModulePath = "provision_module_path"
	ConfigItemProvisionApprove    = "provision_approve"
	ConfigItemProvisionVars       = "provision_vars"

	ConfigItemDeliveryRepoURL         = "delivery_repo_url"
	ConfigItemDeliveryRepoType        = "delivery_repo_type"
	ConfigItemDeliveryChart           = "delivery_chart"
	ConfigItemDeliveryChartVersion    = "delivery_chart_version"
	ConfigItemDeliveryRelease         = "delivery_release"
	ConfigItemDeliveryTargetNamespace = "delivery_target_namespace"
	ConfigItemDeliveryValues          = "delivery_values"
)

// Defaults for optional config items.
const (
	DefaultProvisionApprove    = ApproveAuto
	DefaultProvisionModulePath = "." // the root of the module artifact
	DefaultDeliveryRepoType    = RepoTypeOCI

	ApproveAuto   = "auto"
	ApproveManual = "manual"

	RepoTypeOCI = "oci"
)

// ClusterSpec is the typed form of the reconciler config items of a cluster.
// Optional items have their defaults applied; ProvisionVars and DeliveryValues
// are never nil.
type ClusterSpec struct {
	Alias string
	ID    string

	ProvisionModuleRepo string
	ProvisionModuleRef  string
	ProvisionModulePath string
	ProvisionApprove    string
	ProvisionVars       map[string]interface{}

	DeliveryRepoURL         string
	DeliveryRepoType        string
	DeliveryChart           string
	DeliveryChartVersion    string
	DeliveryRelease         string
	DeliveryTargetNamespace string
	DeliveryValues          map[string]interface{}
}

// ParseSpec parses the config items of a cluster. All problems (missing
// required items and invalid YAML) are reported in a single error.
func ParseSpec(cluster *api.Cluster) (*ClusterSpec, error) {
	items := cluster.ConfigItems

	spec := &ClusterSpec{
		Alias:                   cluster.Alias,
		ID:                      cluster.ID,
		ProvisionModuleRepo:     items[ConfigItemProvisionModuleRepo],
		ProvisionModuleRef:      items[ConfigItemProvisionModuleRef],
		ProvisionModulePath:     items[ConfigItemProvisionModulePath],
		ProvisionApprove:        items[ConfigItemProvisionApprove],
		DeliveryRepoURL:         items[ConfigItemDeliveryRepoURL],
		DeliveryRepoType:        items[ConfigItemDeliveryRepoType],
		DeliveryChart:           items[ConfigItemDeliveryChart],
		DeliveryChartVersion:    items[ConfigItemDeliveryChartVersion],
		DeliveryRelease:         items[ConfigItemDeliveryRelease],
		DeliveryTargetNamespace: items[ConfigItemDeliveryTargetNamespace],
	}

	var problems []string

	required := []string{
		ConfigItemProvisionModuleRepo,
		ConfigItemProvisionModuleRef,
		ConfigItemDeliveryRepoURL,
		ConfigItemDeliveryChart,
		ConfigItemDeliveryChartVersion,
		ConfigItemDeliveryTargetNamespace,
	}
	for _, key := range required {
		if strings.TrimSpace(items[key]) == "" {
			problems = append(problems, fmt.Sprintf("missing required config item %q", key))
		}
	}

	var err error
	if spec.ProvisionVars, err = parseYAMLMap(items[ConfigItemProvisionVars]); err != nil {
		problems = append(problems, fmt.Sprintf("config item %q: %v", ConfigItemProvisionVars, err))
	}
	if spec.DeliveryValues, err = parseYAMLMap(items[ConfigItemDeliveryValues]); err != nil {
		problems = append(problems, fmt.Sprintf("config item %q: %v", ConfigItemDeliveryValues, err))
	}

	// Secrets are not supported yet: refuse encrypted values instead of
	// passing the ciphertext on as if it were the real value.
	for key, value := range items {
		if strings.HasPrefix(strings.TrimSpace(value), decrypter.AWSKMSSecretPrefix) {
			problems = append(problems, fmt.Sprintf("config item %q is encrypted (%s), secrets are not supported", key, decrypter.AWSKMSSecretPrefix))
		}
	}

	spec.applyDefaults()
	problems = append(problems, spec.validate()...)

	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("invalid config items for cluster %s: %s", cluster.Alias, strings.Join(problems, "; "))
	}

	return spec, nil
}

func (spec *ClusterSpec) applyDefaults() {
	if spec.ProvisionApprove == "" {
		spec.ProvisionApprove = DefaultProvisionApprove
	}
	if strings.TrimSpace(spec.ProvisionModulePath) == "" {
		spec.ProvisionModulePath = DefaultProvisionModulePath
	}
	if spec.DeliveryRepoType == "" {
		spec.DeliveryRepoType = DefaultDeliveryRepoType
	}
	if spec.DeliveryRelease == "" {
		spec.DeliveryRelease = spec.DeliveryChart
	}
}

// terraformVarName is the identifier syntax of Terraform/OpenTofu variables.
var terraformVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// validate returns one message per problem with the values of a spec.
func (spec *ClusterSpec) validate() []string {
	var problems []string

	switch spec.ProvisionApprove {
	case ApproveAuto, ApproveManual:
	default:
		problems = append(problems, fmt.Sprintf("config item %q must be %q or %q, got %q",
			ConfigItemProvisionApprove, ApproveAuto, ApproveManual, spec.ProvisionApprove))
	}

	if spec.DeliveryRepoType != RepoTypeOCI {
		problems = append(problems, fmt.Sprintf("config item %q must be %q, got %q",
			ConfigItemDeliveryRepoType, RepoTypeOCI, spec.DeliveryRepoType))
	}

	for name := range spec.ProvisionVars {
		if !terraformVarName.MatchString(name) {
			problems = append(problems, fmt.Sprintf("config item %q: %q is not a valid variable name",
				ConfigItemProvisionVars, name))
		}
	}

	return problems
}

// TerraformVars returns the variables for the "vars" field of the Terraform
// resource, sorted by name. Values keep the type they have in the YAML:
// strings, numbers, booleans, lists and maps are passed on unchanged.
func (spec *ClusterSpec) TerraformVars() []interface{} {
	names := make([]string, 0, len(spec.ProvisionVars))
	for name := range spec.ProvisionVars {
		names = append(names, name)
	}
	sort.Strings(names)

	vars := make([]interface{}, 0, len(names))
	for _, name := range names {
		vars = append(vars, map[string]interface{}{"name": name, "value": spec.ProvisionVars[name]})
	}
	return vars
}

// HelmValues returns the values for the "values" field of the HelmRelease,
// unchanged from the config item.
func (spec *ClusterSpec) HelmValues() map[string]interface{} {
	return spec.DeliveryValues
}

// parseYAMLMap parses a YAML map. An empty value is an empty map.
func parseYAMLMap(value string) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	if strings.TrimSpace(value) == "" {
		return result, nil
	}
	if err := yaml.UnmarshalStrict([]byte(value), &result); err != nil {
		return nil, fmt.Errorf("not a valid YAML map: %w", err)
	}
	if result == nil {
		result = map[string]interface{}{}
	}
	return result, nil
}
