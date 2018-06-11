package provisioner

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"math"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
)

const (
	autoscalingBufferExplicitCPUConfigItem    = "autoscaling_buffer_cpu"
	autoscalingBufferExplicitMemoryConfigItem = "autoscaling_buffer_memory"
	autoscalingBufferCPUScaleConfigItem       = "autoscaling_buffer_cpu_scale"
	autoscalingBufferMemoryScaleConfigItem    = "autoscaling_buffer_memory_scale"
	autoscalingBufferCPUReservedConfigItem    = "autoscaling_buffer_cpu_reserved"
	autoscalingBufferMemoryReservedConfigItem = "autoscaling_buffer_memory_reserved"
	autoscalingBufferPoolsConfigItem          = "autoscaling_buffer_pools"
)

type templateContext struct {
	manifestData          map[string]string
	baseDir               string
	computingManifestHash bool
	readTemplate          func(string) ([]byte, error)
}

type podResources struct {
	CPU    string
	Memory string
}

func newTemplateContext(baseDir string) *templateContext {
	return &templateContext{
		baseDir:      baseDir,
		manifestData: make(map[string]string),
	}
}

func requiredConfigItem(cluster *api.Cluster, configItem string) (string, error) {
	result, ok := cluster.ConfigItems[configItem]
	if !ok {
		return "", fmt.Errorf("missing config item: %s", configItem)
	}
	return result, nil
}

func requiredFloatConfigItem(cluster *api.Cluster, configItem string) (float64, error) {
	strValue, err := requiredConfigItem(cluster, configItem)
	if err != nil {
		return math.NaN(), err
	}
	result, err := strconv.ParseFloat(strValue, 64)
	if err != nil {
		return math.NaN(), fmt.Errorf("unable to parse %s: %v", configItem, err)
	}
	return result, nil
}

func requiredResourceConfigItem(cluster *api.Cluster, configItem string, scale int32) (int64, error) {
	strValue, err := requiredConfigItem(cluster, configItem)
	if err != nil {
		return 0, err
	}

	quantity, err := k8sresource.ParseQuantity(strValue)
	if err != nil {
		return 0, fmt.Errorf("unable to parse %s: %v", configItem, err)
	}

	return quantity.ScaledValue(k8sresource.Scale(scale)), nil
}

// matchingPools returns all node pools whose names patch poolNameRegex
func matchingPools(cluster *api.Cluster, poolNameRegex string) ([]*api.NodePool, error) {
	nameRegex, err := regexp.Compile(poolNameRegex)
	if err != nil {
		return nil, err
	}

	var result []*api.NodePool
	for _, pool := range cluster.NodePools {
		if nameRegex.FindStringIndex(pool.Name) != nil {
			result = append(result, pool)
		}
	}
	return result, nil
}

// autoscalingBufferSettings returns the CPU and memory resources for the autoscaling buffer pods based on various
// config items. If autoscaling_buffer_cpu and autoscaling_buffer_memory are set, the values are used directly,
// otherwise it finds the largest instance type from the node pools matching autoscaling_buffer_pools, scales
// it using autoscaling_buffer_cpu_scale and autoscaling_buffer_memory_scale and then takes the minimum of
// the scaled value or the node size minus autoscaling_buffer_{cpu|memory}_reserved
func autoscalingBufferSettings(cluster *api.Cluster) (*podResources, error) {
	explicitCPU, haveExplicitCPU := cluster.ConfigItems[autoscalingBufferExplicitCPUConfigItem]
	explicitMemory, haveExplicitMemory := cluster.ConfigItems[autoscalingBufferExplicitMemoryConfigItem]

	if haveExplicitCPU && haveExplicitMemory {
		return &podResources{CPU: explicitCPU, Memory: explicitMemory}, nil
	} else if haveExplicitCPU || haveExplicitMemory {
		// avoid issues if the user overrides the CPU and then the resulting pod can't fit after node pool change
		return nil, fmt.Errorf("autoscaling_buffer_cpu/autoscaling_buffer_memory must be used together or not at all")
	}

	poolNameRegex, err := requiredConfigItem(cluster, autoscalingBufferPoolsConfigItem)
	if err != nil {
		return nil, err
	}
	cpuScale, err := requiredFloatConfigItem(cluster, autoscalingBufferCPUScaleConfigItem)
	if err != nil {
		return nil, err
	}
	memoryScale, err := requiredFloatConfigItem(cluster, autoscalingBufferMemoryScaleConfigItem)
	if err != nil {
		return nil, err
	}
	cpuReserved, err := requiredResourceConfigItem(cluster, autoscalingBufferCPUReservedConfigItem, -3)
	if err != nil {
		return nil, err
	}
	memoryReserved, err := requiredResourceConfigItem(cluster, autoscalingBufferMemoryReservedConfigItem, 0)
	if err != nil {
		return nil, err
	}

	pools, err := matchingPools(cluster, poolNameRegex)
	if err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return nil, fmt.Errorf("no pools matching %s", poolNameRegex)
	}

	currentLargestInstance := aws.Instance{}

	for _, pool := range pools {
		instanceInfo, err := aws.InstanceInfo(pool.InstanceType)
		if err != nil {
			return nil, err
		}

		if instanceInfo.VCPU > currentLargestInstance.VCPU && instanceInfo.Memory > currentLargestInstance.Memory {
			currentLargestInstance = instanceInfo
		} else if instanceInfo.VCPU <= currentLargestInstance.VCPU && instanceInfo.Memory <= currentLargestInstance.Memory {
			// do nothing
		} else {
			return nil, fmt.Errorf("unable to select autoscaling buffer settings, conflicting instance types %s and %s", currentLargestInstance.InstanceType, instanceInfo.InstanceType)
		}
	}

	result := &podResources{
		CPU:    k8sresource.NewMilliQuantity(effectiveQuantity(currentLargestInstance.VCPU*1000, cpuScale, cpuReserved), k8sresource.DecimalSI).String(),
		Memory: k8sresource.NewQuantity(effectiveQuantity(currentLargestInstance.Memory, memoryScale, memoryReserved), k8sresource.BinarySI).String(),
	}
	return result, nil
}

func effectiveQuantity(instanceResource int64, scale float64, reservedResource int64) int64 {
	scaledResource := int64(float64(instanceResource) * scale)
	withoutReserved := instanceResource - reservedResource
	if scaledResource < withoutReserved {
		return scaledResource
	} else {
		return withoutReserved
	}
}

// renderTemplate takes a fileName of a template and the model to apply to it.
// returns the transformed template or an error if not successful
func renderTemplate(context *templateContext, filePath string, data interface{}) (string, error) {
	funcMap := template.FuncMap{
		"getAWSAccountID":           getAWSAccountID,
		"base64":                    base64Encode,
		"manifestHash":              func(template string) (string, error) { return manifestHash(context, filePath, template, data) },
		"autoscalingBufferSettings": autoscalingBufferSettings,
		"asgSize":                   asgSize,
		"azID":                      azID,
		"azCount":                   azCount,
	}

	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	t, err := template.New(filePath).Option("missingkey=error").Funcs(funcMap).Parse(string(content))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = t.Execute(&out, data)
	if err != nil {
		return "", err
	}

	templateData := out.String()
	context.manifestData[filePath] = templateData

	return templateData, nil
}

// manifestHash is a function for the templates that will return a hash of an interpolated sibling template
// file. returns an error if computing manifestHash calls manifestHash again, if interpolation of that template
// returns an error, or if the path is outside of the manifests folder.
func manifestHash(context *templateContext, file string, template string, data interface{}) (string, error) {
	if context.computingManifestHash {
		return "", fmt.Errorf("manifestHash is not reentrant")
	}
	context.computingManifestHash = true
	defer func() {
		context.computingManifestHash = false
	}()

	templateFile, err := filepath.Abs(path.Clean(path.Join(path.Dir(file), template)))
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(templateFile, context.baseDir) {
		return "", fmt.Errorf("invalid template path: %s", templateFile)
	}

	templateData, ok := context.manifestData[templateFile]
	if !ok {
		applied, err := renderTemplate(context, templateFile, data)
		if err != nil {
			return "", err
		}
		templateData = applied
	}

	return fmt.Sprintf("%x", sha256.Sum256([]byte(templateData))), nil
}

// getAWSAccountID is an utility function for the gotemplate that will remove
// the prefix "aws" from the infrastructure ID.
// TODO: get the real AWS account ID from the `external_id` field of the
// infrastructure account in the cluster registry.
func getAWSAccountID(ia string) string {
	return strings.Split(ia, ":")[1]
}

// base64Encode base64 encodes a string.
func base64Encode(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

// asgSize computes effective size of an ASG (either min or max) from the corresponding
// node pool size and the amount of ASGs in the pool. Current implementation just divides
// and returns an error if the pool size is not an exact multiple, but maybe it's not the
// best one.
func asgSize(poolSize, asgPerPool int64) (int64, error) {
	if poolSize%asgPerPool != 0 {
		return 0, fmt.Errorf("pool size must be an exact multiple of %d", asgPerPool)
	}
	return poolSize / asgPerPool, nil
}

// azID returns the last part of the availability zone name (1c for eu-central-1c)
func azID(azName string) string {
	slugs := strings.Split(azName, "-")
	return slugs[len(slugs)-1]
}

// azCount returns the count of availability zones in the subnet map
func azCount(subnets map[string]string) int64 {
	var result int64
	for k := range subnets {
		if k != subnetAllAZName {
			result++
		}
	}
	return result
}
