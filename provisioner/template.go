package provisioner

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	awsUtil "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
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
	highestPossiblePort                       = 65535
	lowestPossiblePort                        = 0
	describeImageFilterNameName               = "name"
	describeImageFilterNameOwner              = "owner-id"
)

type templateContext struct {
	templateData          map[string]string
	fileData              map[string][]byte
	cluster               *api.Cluster
	nodePool              *api.NodePool
	values                map[string]interface{}
	computingManifestHash bool
	awsAdapter            *awsAdapter
}

type templateData struct {
	// From api.Cluster, TODO: drop after we migrate all Kubernetes manifests
	Alias                 string
	APIServerURL          string
	Channel               string
	ConfigItems           map[string]string
	CriticalityLevel      int32
	Environment           string
	ID                    string
	InfrastructureAccount string
	LifecycleStatus       string
	LocalID               string
	NodePools             []*api.NodePool
	Region                string
	Owner                 string

	// Available everywhere
	Cluster *api.Cluster

	// Available everywhere except defaults
	Values map[string]interface{}

	// Available in node pool templates
	NodePool *api.NodePool

	// User data (deprecated, TODO: move to .Values.UserData)
	UserData string

	// Path to the generated files uploaded to S3 (deprecated, TODO: move to .Values.S3GeneratedFilesPath)
	S3GeneratedFilesPath string
}

type podResources struct {
	CPU    string
	Memory string
}

func newTemplateContext(fileData map[string][]byte, cluster *api.Cluster, nodePool *api.NodePool, values map[string]interface{}, adapter *awsAdapter) *templateContext {
	return &templateContext{
		fileData:     fileData,
		templateData: make(map[string]string),
		cluster:      cluster,
		nodePool:     nodePool,
		values:       values,
		awsAdapter:   adapter,
	}
}

func renderSingleTemplate(manifest channel.Manifest, cluster *api.Cluster, nodePool *api.NodePool, values map[string]interface{}, adapter *awsAdapter) (string, error) {
	ctx := newTemplateContext(map[string][]byte{manifest.Path: manifest.Contents}, cluster, nodePool, values, adapter)
	return renderTemplate(ctx, manifest.Path)
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
// TODO use the proper type for the argument
func autoscalingBufferSettings(clusterOrData interface{}) (*podResources, error) {
	var cluster *api.Cluster

	switch v := clusterOrData.(type) {
	case *api.Cluster:
		cluster = v
	case *templateData:
		cluster = v.Cluster
	default:
		return nil, fmt.Errorf("autoscalingBufferSettings: expected *api.Cluster or *templateData, got %s", reflect.TypeOf(clusterOrData))
	}

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

	var currentBestFitVCPU, currentBestFitMemory int64

	for _, pool := range pools {
		for _, instanceType := range pool.InstanceTypes {
			instanceInfo, err := aws.InstanceInfo(instanceType)
			if err != nil {
				return nil, err
			}

			if currentBestFitVCPU == 0 || instanceInfo.VCPU < currentBestFitVCPU {
				currentBestFitVCPU = instanceInfo.VCPU
			}

			if currentBestFitMemory == 0 || instanceInfo.Memory < currentBestFitMemory {
				currentBestFitMemory = instanceInfo.Memory
			}
		}
	}

	result := &podResources{
		CPU:    k8sresource.NewMilliQuantity(effectiveQuantity(currentBestFitVCPU*1000, cpuScale, cpuReserved), k8sresource.DecimalSI).String(),
		Memory: k8sresource.NewQuantity(effectiveQuantity(currentBestFitMemory, memoryScale, memoryReserved), k8sresource.BinarySI).String(),
	}
	return result, nil
}

func effectiveQuantity(instanceResource int64, scale float64, reservedResource int64) int64 {
	scaledResource := int64(float64(instanceResource) * scale)
	withoutReserved := instanceResource - reservedResource
	if scaledResource < withoutReserved {
		return scaledResource
	}
	return withoutReserved
}

// renderTemplate takes a fileName of a template in the context and the model to apply to it.
// returns the transformed template or an error if not successful
func renderTemplate(context *templateContext, file string) (string, error) {
	funcMap := template.FuncMap{
		"getAWSAccountID":           getAWSAccountID,
		"base64":                    base64Encode,
		"base64Decode":              base64Decode,
		"manifestHash":              func(template string) (string, error) { return manifestHash(context, file, template) },
		"autoscalingBufferSettings": autoscalingBufferSettings,
		"asgSize":                   asgSize,
		"azID":                      azID,
		"azCount":                   azCount,
		"split":                     split,
		"mountUnitName":             mountUnitName,
		"accountID":                 accountID,
		"portRanges":                portRanges,
		"splitHostPort":             splitHostPort,
		"extractEndpointHosts":      extractEndpointHosts,
		"publicKey":                 publicKey,
		"stupsNATSubnets":           stupsNATSubnets,
		"amiID": func(imageName, imageOwner string) (string, error) {
			return amiID(context.awsAdapter, imageName, imageOwner)
		},
		"nodeCIDRMaxNodes":              nodeCIDRMaxNodes,
		"nodeCIDRMaxPods":               nodeCIDRMaxPods,
		"parseInt64":                    parseInt64,
		"generateJWKSDocument":          generateJWKSDocument,
		"generateOIDCDiscoveryDocument": generateOIDCDiscoveryDocument,
		"kubernetesSizeToKiloBytes":     kubernetesSizeToKiloBytes,
		"indexedList":                   indexedList,
	}

	content, ok := context.fileData[file]
	if !ok {
		return "", fmt.Errorf("template file not found: %s", file)
	}
	t, err := template.New(file).Option("missingkey=error").Funcs(funcMap).Parse(string(content))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer

	data := &templateData{
		Alias:                 context.cluster.Alias,
		APIServerURL:          context.cluster.APIServerURL,
		Channel:               context.cluster.Channel,
		ConfigItems:           context.cluster.ConfigItems,
		CriticalityLevel:      context.cluster.CriticalityLevel,
		Environment:           context.cluster.Environment,
		ID:                    context.cluster.ID,
		InfrastructureAccount: context.cluster.InfrastructureAccount,
		LifecycleStatus:       context.cluster.LifecycleStatus,
		LocalID:               context.cluster.LocalID,
		NodePools:             context.cluster.NodePools,
		Region:                context.cluster.Region,
		Owner:                 context.cluster.Owner,
		Cluster:               context.cluster,
		Values:                context.values,
		NodePool:              context.nodePool,
	}

	if ud, ok := context.values[userDataValuesKey]; ok {
		data.UserData = ud.(string)
	}

	if s3path, ok := context.values[s3GeneratedFilesPathValuesKey]; ok {
		data.S3GeneratedFilesPath = s3path.(string)
	}

	err = t.Execute(&out, data)
	if err != nil {
		return "", err
	}

	templateData := out.String()
	context.templateData[file] = templateData

	return templateData, nil
}

// manifestHash is a function for the templates that will return a hash of an interpolated sibling template
// file. returns an error if computing manifestHash calls manifestHash again, if interpolation of that template
// returns an error, or if the path is outside of the manifests folder.
func manifestHash(context *templateContext, originalFile, templateFile string) (string, error) {
	if context.computingManifestHash {
		return "", fmt.Errorf("manifestHash is not reentrant")
	}
	context.computingManifestHash = true
	defer func() {
		context.computingManifestHash = false
	}()

	newPath := path.Clean(path.Join(path.Dir(originalFile), templateFile))
	templateData, ok := context.templateData[newPath]
	if !ok {
		applied, err := renderTemplate(context, newPath)
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

// mountUnitName escapes / characters in a mount path to be used a systemd unit name
func mountUnitName(path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("not an absolute path: %s", path)
	}
	return strings.Replace(path[1:], "/", "-", -1), nil
}

// base64Encode base64 encodes a string.
func base64Encode(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

// base64Encode base64 decodes a string.
func base64Decode(value string) (string, error) {
	res, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(res), nil
}

// asgSize computes effective size of an ASG (either min or max) from the corresponding
// node pool size and the amount of ASGs in the pool. Current implementation just divides
// and returns an error if the pool size is not an exact multiple, but maybe it's not the
// best one.
func asgSize(poolSize int64, asgPerPool int) (int, error) {
	if int(poolSize)%asgPerPool != 0 {
		return 0, fmt.Errorf("pool size must be an exact multiple of %d", asgPerPool)
	}
	return int(poolSize) / asgPerPool, nil
}

// azID returns the last part of the availability zone name (1c for eu-central-1c)
func azID(azName string) string {
	slugs := strings.Split(azName, "-")
	return slugs[len(slugs)-1]
}

// azCount returns the count of availability zones in the subnet map
func azCount(subnets map[string]string) int {
	var result int
	for k := range subnets {
		if k != subnetAllAZName {
			result++
		}
	}
	return result
}

// split is a template function that takes a string and a separator and returns the splitted parts.
func split(s string, d string) []string {
	return strings.Split(s, d)
}

// accountID returns just the ID part of an account
func accountID(account string) (string, error) {
	items := strings.Split(account, ":")
	if len(items) != 2 {
		return "", fmt.Errorf("invalid account (expected type:id): %s", account)
	}
	return items[1], nil
}

type HostPort struct {
	Host string
	Port string
}

// splitHostPort exposes net.SplitHostPort
func splitHostPort(hostport string) (HostPort, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return HostPort{}, err
	}
	return HostPort{Host: host, Port: port}, nil
}

type PortRange struct {
	FromPort, ToPort int
}

// portRanges parses a comma separated list of port ranges
func portRanges(ranges string) ([]PortRange, error) {
	rangesL := strings.Split(ranges, ",")
	p := make([]PortRange, len(rangesL))
	for i, r := range rangesL {
		splitR := strings.Split(r, "-")
		if len(splitR) != 2 {
			return nil, fmt.Errorf("invalid input for portRange: %s", ranges)
		}
		fromPort, err := strconv.Atoi(splitR[0])
		if err != nil {
			return nil, fmt.Errorf("invalid start port: %s in input: %s", splitR[0], ranges)
		}
		toPort, err := strconv.Atoi(splitR[1])
		if err != nil {
			return nil, fmt.Errorf("invalid end port: %s in input: %s", splitR[1], ranges)
		}
		if !validPortRange(fromPort, toPort) {
			return nil, fmt.Errorf("port range %d-%d is invalid", fromPort, toPort)
		}
		p[i] = PortRange{FromPort: fromPort, ToPort: toPort}
	}
	return p, nil
}

func validPortRange(fromPort, toPort int) bool {
	if fromPort > toPort {
		return false
	}
	if toPort > highestPossiblePort {
		return false
	}
	if fromPort < lowestPossiblePort {
		return false
	}
	return true
}

// given a PEM-encoded private key, returns a PEM-encoded public key
func publicKey(privateKey string) (string, error) {
	decoded, _ := pem.Decode([]byte(privateKey))
	if decoded == nil {
		return "", errors.New("no PEM data found")
	}

	privKey, err := x509.ParsePKCS1PrivateKey(decoded.Bytes)
	if err != nil {
		return "", err
	}

	der, err := x509.MarshalPKIXPublicKey(privKey.Public())
	if err != nil {
		return "", err
	}

	block := pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}
	return string(pem.EncodeToMemory(&block)), nil
}

// subdivide divides a network into smaller /size subnetworks
func subdivide(network *net.IPNet, size int) ([]*net.IPNet, error) {
	subnetSize, addrSize := network.Mask.Size()
	if addrSize != 32 {
		return nil, fmt.Errorf("only ipv4 subnets are supported, got %s", network)
	}
	if size < subnetSize || subnetSize > addrSize {
		return nil, fmt.Errorf("subnet must be between /%d and /32", subnetSize)
	}
	newMask := net.CIDRMask(size, addrSize)

	var addrCountOriginal uint32 = 1 << (uint(addrSize) - uint(subnetSize)) // addresses in the original network
	var addrCountSubdivided uint32 = 1 << (uint(addrSize) - uint(size))     // addresses in the subnets

	var result []*net.IPNet
	for i := uint32(0); i < addrCountOriginal/addrCountSubdivided; i++ {
		// add i * addrCountSubdivided to the initial IP address
		newIp := make([]byte, 4)
		binary.BigEndian.PutUint32(newIp, binary.BigEndian.Uint32(network.IP)+i*addrCountSubdivided)

		result = append(result, &net.IPNet{
			IP:   newIp,
			Mask: newMask,
		})
	}
	return result, nil
}

// given a VPC CIDR block, return a comma-separated list of <count> NAT subnets from the STUPS setup
func stupsNATSubnets(vpcCidr string) ([]string, error) {
	_, vpcNet, err := net.ParseCIDR(vpcCidr)
	if err != nil {
		return nil, err
	}

	// subdivide the network into /size+2 subnets first, take the second one
	subnetSize, _ := vpcNet.Mask.Size()
	if subnetSize == 0 || subnetSize > 24 {
		return nil, fmt.Errorf("invalid subnet, expecting at least /24: %s", vpcNet)
	}

	addrs, err := subdivide(vpcNet, subnetSize+2)
	if err != nil {
		return nil, err
	}

	natNetworks, err := subdivide(addrs[1], 28)
	if err != nil {
		return nil, err
	}

	var result []string
	for i := 0; i < 3; i++ {
		result = append(result, natNetworks[i].String())
	}
	return result, nil
}

func amiID(adapter *awsAdapter, imageName, imageOwner string) (string, error) {
	if adapter == nil || adapter.ec2Client == nil {
		return "", fmt.Errorf("the ec2 client is not available")
	}

	input := ec2.DescribeImagesInput{Filters: []*ec2.Filter{
		{Name: awsUtil.String(describeImageFilterNameName), Values: awsUtil.StringSlice([]string{imageName})},
		{Name: awsUtil.String(describeImageFilterNameOwner), Values: awsUtil.StringSlice([]string{imageOwner})},
	}}
	output, err := adapter.ec2Client.DescribeImages(&input)
	if err != nil {
		return "", fmt.Errorf("failed to describe image with name %s and owner %s: %v", imageName, imageOwner, err)
	}
	if len(output.Images) != 1 {
		return "", fmt.Errorf("more than one image found with name: %s and owner: %s", imageName, imageOwner)
	}
	return *output.Images[0].ImageId, nil
}

func parseInt64(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}

func checkCIDRMaxSize(maskSize int64) error {
	if maskSize < 24 || maskSize > 28 {
		return fmt.Errorf("invalid value for maskSize: %d", maskSize)
	}
	return nil
}

func nodeCIDRMaxNodes(maskSize int64, reserved int64) (int64, error) {
	err := checkCIDRMaxSize(maskSize)
	if err != nil {
		return 0, err
	}

	return 2<<(maskSize-16-1) - reserved, nil
}

func nodeCIDRMaxPods(maskSize int64, extraCapacity int64) (int64, error) {
	err := checkCIDRMaxSize(maskSize)
	if err != nil {
		return 0, err
	}

	maxPods := 2<<(32-maskSize-2) + extraCapacity
	if maxPods > 110 {
		maxPods = 110
	}
	return maxPods, nil
}

func kubernetesSizeToKiloBytes(quantity string, scale float64) (string, error) {
	resource, err := k8sresource.ParseQuantity(quantity)
	if err != nil {
		return "", err
	}
	val, converted := resource.AsInt64()
	if !converted {
		return "", fmt.Errorf("unexpected size for quantity: %s", quantity)
	}
	kbs := int(math.Ceil(float64(val) / 1024 * scale))
	return fmt.Sprintf("%dKB", kbs), nil
}

func extractEndpointHosts(endpoints string) ([]string, error) {
	hostnames := map[string]bool{}
	for _, endpoint := range strings.Split(endpoints, ",") {
		if !strings.HasPrefix(endpoint, "http") {
			endpoint = "http://" + endpoint
		}

		parsed, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("unable to parse endpoint '%s': %v", endpoint, err)
		}
		hostnames[parsed.Hostname()] = true
	}

	var result []string
	for hostname := range hostnames {
		result = append(result, hostname)
	}
	sort.Strings(result)
	return result, nil
}

func indexedList(itemTemplate string, length int64) (string, error) {
	if length < 0 {
		return "", fmt.Errorf("expecting non-negative integer, got: %d", length)
	}

	result := make([]string, length)
	for i := int64(0); i < length; i++ {
		result[i] = strings.ReplaceAll(itemTemplate, "$", fmt.Sprint(i))
	}

	return strings.Join(result, ","), nil
}
