// Package reconciler turns cluster registry entries into the desired state of
// an infrastructure and delivery toolchain running on a host cluster.
package reconciler

import (
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

// Config items understood by the reconciler.
const (
	// ConfigItemEnabled marks the registry entries the reconciler handles.
	// Entries without it set to EnabledValue are ignored.
	ConfigItemEnabled = "reconciler"
	EnabledValue      = "enabled"

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

// ClusterSpec is the typed form of the reconciler config items of a cluster.
// Defaults are not applied here.
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
		ConfigItemProvisionModulePath,
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

	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("invalid config items for cluster %s: %s", cluster.Alias, strings.Join(problems, "; "))
	}

	return spec, nil
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
