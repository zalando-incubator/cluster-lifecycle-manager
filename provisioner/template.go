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
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	awsUtils "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
)

const (
	highestPossiblePort          = 65535
	lowestPossiblePort           = 0
	describeImageFilterNameName  = "name"
	describeImageFilterNameOwner = "owner-id"

	labelsConfigItem = "labels"
	taintsConfigItem = "taints"
	dedicatedLabel   = "dedicated"
)

type templateContext struct {
	templateData          map[string]string
	fileData              map[string][]byte
	cluster               *api.Cluster
	nodePool              *api.NodePool
	values                map[string]interface{}
	computingManifestHash bool
	awsAdapter            *awsAdapter
	instanceTypes         *awsUtils.InstanceTypes
}

type templateData struct {
	// Available everywhere
	Cluster *api.Cluster

	// Available everywhere except defaults
	Values map[string]interface{}

	// Available in node pool templates
	NodePool *api.NodePool
}

func newTemplateContext(fileData map[string][]byte, cluster *api.Cluster, nodePool *api.NodePool, values map[string]interface{}, adapter *awsAdapter, instanceTypes *awsUtils.InstanceTypes) *templateContext {
	return &templateContext{
		fileData:      fileData,
		templateData:  make(map[string]string),
		cluster:       cluster,
		nodePool:      nodePool,
		values:        values,
		awsAdapter:    adapter,
		instanceTypes: instanceTypes,
	}
}

func renderSingleTemplate(manifest channel.Manifest, cluster *api.Cluster, nodePool *api.NodePool, values map[string]interface{}, adapter *awsAdapter, instanceTypes *awsUtils.InstanceTypes) (string, error) {
	ctx := newTemplateContext(map[string][]byte{manifest.Path: manifest.Contents}, cluster, nodePool, values, adapter, instanceTypes)
	return renderTemplate(ctx, manifest.Path)
}

// renderTemplate takes a fileName of a template in the context and the model to apply to it.
// returns the transformed template or an error if not successful
func renderTemplate(context *templateContext, file string) (string, error) {
	funcMap := template.FuncMap{
		"getAWSAccountID": getAWSAccountID,
		"base64":          base64Encode,
		"base64Decode":    base64Decode,
		"manifestHash": func(template string) (string, error) {
			return manifestHash(context, file, template)
		},
		"sha256": func(value string) (string, error) {
			return fmt.Sprintf("%x", sha256.Sum256([]byte(value))), nil
		},
		"asgSize":              asgSize,
		"azID":                 azID,
		"azCount":              azCount,
		"split":                split,
		"join":                 sprig.GenericFuncMap()["join"],
		"eksID":                eksID,
		"mountUnitName":        mountUnitName,
		"accountID":            accountID,
		"portRanges":           portRanges,
		"sgIngressRanges":      sgIngressRanges,
		"splitHostPort":        splitHostPort,
		"extractEndpointHosts": extractEndpointHosts,
		"publicKey":            publicKey,
		"stupsNATSubnets":      stupsNATSubnets,
		"amiID": func(imageName, imageOwner string) (string, error) {
			return amiID(context.awsAdapter, imageName, imageOwner)
		},
		// TODO: this function is kept for backward compatibility while
		// the the use of `nodeCIDRMaxNodesPodCIDR` is being rolled out
		// to all channels of kubernetes-on-aws. After a full rollout
		// this can be dropped.
		"nodeCIDRMaxNodes": func(maskSize int64, reserved int64) (int64, error) {
			return nodeCIDRMaxNodes(16, maskSize, reserved)
		},
		"nodeCIDRMaxNodesPodCIDR":               nodeCIDRMaxNodes,
		"nodeCIDRMaxPods":                       nodeCIDRMaxPods,
		"nthAddressFromCIDR":                    nthAddressFromCIDR,
		"parseInt64":                            parseInt64,
		"generateJWKSDocument":                  generateJWKSDocument,
		"generateOIDCDiscoveryDocument":         generateOIDCDiscoveryDocument,
		"kubernetesSizeToKiloBytes":             kubernetesSizeToKiloBytes,
		"indexedList":                           indexedList,
		"zoneDistributedNodePoolGroups":         zoneDistributedNodePoolGroups,
		"nodeLifeCycleProviderPerNodePoolGroup": nodeLifeCycleProviderPerNodePoolGroup,
		"certificateExpiry":                     certificateExpiry,
		"sumQuantities":                         sumQuantities,
		"awsValidID":                            awsValidID,
		"indent":                                sprig.GenericFuncMap()["indent"],
		"dict":                                  dict,
		"list":                                  list,
		"append":                                strAppend,
		"scaleQuantity":                         scaleQuantity,
		"instanceTypeCPUQuantity": func(instanceType string) (string, error) {
			return instanceTypeCPUQuantity(context, instanceType)
		},
		"instanceTypeMemoryQuantity": func(instanceType string) (string, error) {
			return instanceTypeMemoryQuantity(context, instanceType)
		},
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
		Cluster:  context.cluster,
		Values:   context.values,
		NodePool: context.nodePool,
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
	if s == "" {
		return nil
	}
	return strings.Split(s, d)
}

// dict is a template function that constructs a map out of its arguments.
// Argument list is treated as a sequence of key-value pairs and must have even length.
// Key arguments must be unique and have string type.
func dict(args ...interface{}) (map[string]interface{}, error) {
	if len(args) == 0 || len(args)%2 != 0 {
		return nil, fmt.Errorf("dict: invalid number of arguments: %d", len(args))
	}
	dict := make(map[string]interface{}, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key argument %d must be string", i)
		}
		if _, ok := dict[key]; ok {
			return nil, fmt.Errorf("dict: duplicate key %s", key)
		}
		dict[key] = args[i+1]
	}
	return dict, nil
}

func list(args ...interface{}) []interface{} {
	return args
}

func strAppend(list []string, item string, moreItems ...string) []string {
	items := append(list, item)
	return append(items, moreItems...)
}

func eksID(id string) string {
	parts := strings.Split(id, ":")
	return parts[len(parts)-1]
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

type SGIngressRange struct {
	CIDR             string
	FromPort, ToPort int
	Protocol         string
}

// sgIngressRanges parses a list of Security Group Ingress ranges.
//
// The format is [<protocol>:]<cidr>:<port>[-<port>]
//
// Example:
//
// "10.0.0.0/8:4180-4181,0.0.0.0/0:4190,udp:0.0.0.0/0:53" would result in the ingress ranges:
// [
//
//	{
//	  CIDR: "10.0.0.0/8",
//	  FromPort: 4180,
//	  ToPort: 4181,
//	  Protocol: "tcp",
//	},
//	{
//	  CIDR: "0.0.0.0/0",
//	  FromPort: 4190,
//	  ToPort: 4190,
//	  Protocol: "tcp",
//	},
//	{
//	  CIDR: "0.0.0.0/0",
//	  FromPort: 53,
//	  ToPort: 53,
//	  Protocol: "udp",
//	},
//
// ]
func sgIngressRanges(ranges string) ([]SGIngressRange, error) {
	rangesL := strings.Split(ranges, ",")
	p := make([]SGIngressRange, len(rangesL))
	for i, r := range rangesL {
		splitFields := strings.Split(r, ":")
		var protocol, cidr, portRange string
		if len(splitFields) == 2 {
			protocol = "tcp"
			cidr = splitFields[0]
			portRange = splitFields[1]
		} else if len(splitFields) == 3 {
			protocol = splitFields[0]
			cidr = splitFields[1]
			portRange = splitFields[2]
			switch protocol {
			case "tcp", "udp":
			default:
				return nil, fmt.Errorf("invalid protocol '%s' only 'tcp' or 'udp' supported: %s", protocol, ranges)
			}
		} else {
			return nil, fmt.Errorf("invalid input for sgIngressRanges: %s", ranges)
		}

		_, cidrNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		splitR := strings.Split(portRange, "-")
		var fromPortStr, toPortStr string
		if len(splitR) == 1 {
			fromPortStr = splitR[0]
			toPortStr = fromPortStr
		} else if len(splitR) == 2 {
			fromPortStr = splitR[0]
			toPortStr = splitR[1]
		} else {
			return nil, fmt.Errorf("invalid input for sgIngressRanges: %s", ranges)
		}
		fromPort, err := strconv.Atoi(fromPortStr)
		if err != nil {
			return nil, fmt.Errorf("invalid start port: %s in input: %s", splitR[0], ranges)
		}
		toPort, err := strconv.Atoi(toPortStr)
		if err != nil {
			return nil, fmt.Errorf("invalid end port: %s in input: %s", splitR[1], ranges)
		}
		if !validPortRange(fromPort, toPort) {
			return nil, fmt.Errorf("port range %d-%d is invalid", fromPort, toPort)
		}
		p[i] = SGIngressRange{CIDR: cidrNet.String(), FromPort: fromPort, ToPort: toPort, Protocol: protocol}
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
		newIP := make([]byte, 4)
		binary.BigEndian.PutUint32(newIP, binary.BigEndian.Uint32(network.IP)+i*addrCountSubdivided)

		result = append(result, &net.IPNet{
			IP:   newIP,
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
		{Name: aws.String(describeImageFilterNameName), Values: aws.StringSlice([]string{imageName})},
		{Name: aws.String(describeImageFilterNameOwner), Values: aws.StringSlice([]string{imageOwner})},
	}}
	output, err := adapter.ec2Client.DescribeImages(&input)
	if err != nil {
		return "", fmt.Errorf("failed to describe image with name %s and owner %s: %v", imageName, imageOwner, err)
	}
	if len(output.Images) > 1 {
		return "", fmt.Errorf("more than one image found with name: %s and owner: %s", imageName, imageOwner)
	}
	if len(output.Images) == 0 {
		return "", fmt.Errorf("no image found with name: %s and owner: %s", imageName, imageOwner)
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

func nodeCIDRMaxNodes(podCIDRMaskSize, maskSize int64, reserved int64) (int64, error) {
	if podCIDRMaskSize < 14 || podCIDRMaskSize > 16 {
		return 0, fmt.Errorf("invalid for podCIDRMaskSize: %d", podCIDRMaskSize)
	}

	err := checkCIDRMaxSize(maskSize)
	if err != nil {
		return 0, err
	}

	return 2<<(maskSize-podCIDRMaskSize-1) - reserved, nil
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

// nthAddressFromCIDR takes an IPv4 or IPv6 CIDR and returns the Nth address in
// the subnet.
func nthAddressFromCIDR(cidr string, n int) (string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", err
	}

	ip := ipNet.IP

	nthByte := 0
	if ip.To4() != nil {
		// ipv4 address
		nthByte = 3
		ip = ip.To4()
	} else {
		// ipv6 address
		ip = ip.To16()
		if ip == nil {
			return "", fmt.Errorf("invalid IP address: %s", ipNet.IP)
		}
		nthByte = 15
	}

	ip = ip.Mask(ipNet.Mask)
	for i := 0; i < n; i++ {
		ip[nthByte]++
	}
	return ip.String(), nil
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

func parseLabels(nodePool *api.NodePool) map[string]string {
	result := make(map[string]string)
	for _, s := range strings.Split(nodePool.ConfigItems[labelsConfigItem], ",") {
		if items := strings.SplitN(s, "=", 2); len(items) == 2 {
			result[items[0]] = items[1]
		}
	}
	return result
}

func poolsDistributed(dedicated string, pools []*api.NodePool) bool {
	if len(pools) == 0 {
		return false
	}

	for _, pool := range pools {
		// For dedicated pools, check that the taints are configured correctly
		if dedicated != "" && pool.ConfigItems[taintsConfigItem] != fmt.Sprintf("dedicated=%s:NoSchedule", dedicated) {
			return false
		}

		// Check if the pool is configured to run in specific AZs
		if _, ok := pool.ConfigItems["availability_zones"]; ok {
			return false
		}

		// Check if the pool is using the pool profile that's properly spread between AZs
		switch pool.Profile {
		case "worker-splitaz", "worker-karpenter":
		default:
			return false
		}
	}
	return true
}

// azDistributedNodePoolGroups returns a list of node pool groups (using the dedicated label) that are safe to use
// with even pod spreading. Currently this is the case iff all node pools with this dedicated label
//   - are correctly configured with regards to the labels and taints
//   - don't have AZ restrictions
//   - use the worker-splitaz profile or the worker-karpenter profile.
//
// The default pool is represented with an empty string as the key.
func zoneDistributedNodePoolGroups(nodePools []*api.NodePool) map[string]bool {
	poolGroups := groupNodePoolsByPurpose(nodePools)
	result := make(map[string]bool)
	for group, pools := range poolGroups {
		if poolsDistributed(group, pools) {
			result[group] = true
		}
	}
	return result
}

func groupNodePoolsByPurpose(nodePools []*api.NodePool) map[string][]*api.NodePool {
	poolGroups := make(map[string][]*api.NodePool)

	for _, pool := range nodePools {
		if strings.HasPrefix(pool.Profile, "master") {
			continue
		}

		labels := parseLabels(pool)
		if group, ok := labels[dedicatedLabel]; ok && group != "" {
			poolGroups[group] = append(poolGroups[group], pool)
		} else if _, ok := pool.ConfigItems[taintsConfigItem]; !ok {
			poolGroups[""] = append(poolGroups[group], pool)
		}
	}

	return poolGroups
}

// nodeLifeCycleProviderPerNodePoolGroup groups node-pools by the dedicated label, each node-pool-group is mapped to
// the provider suitable for its node-pool profile
// in case all pools of a group do not share the same profile, the group is mapped to empty string
func nodeLifeCycleProviderPerNodePoolGroup(nodePools []*api.NodePool) map[string]string {
	providers := map[string]string{
		"worker-karpenter": "karpenter",
		"worker-combined":  "zalando",
		"worker-splitaz":   "zalando",
	}
	poolGroups := groupNodePoolsByPurpose(nodePools)
	provider := make(map[string]string)
	for group, pools := range poolGroups {
		if group == "" {
			group = "default"
		}
		groupProfile := pools[0].Profile
		for _, pool := range pools[1:] {
			if pool.Profile != groupProfile {
				groupProfile = ""
				break
			}
		}
		provider[group] = providers[groupProfile]
	}
	return provider
}

// certificateExpiry returns the notAfter timestamp of a PEM-encoded certificate in the RFC3339 format
func certificateExpiry(certificate string) (string, error) {
	expiry, err := certificateExpiryTime(certificate)
	if err != nil {
		return "", err
	}
	return expiry.UTC().Format(time.RFC3339), nil
}

func sumQuantities(quantities ...string) (string, error) {
	var result k8sresource.Quantity
	for _, quantity := range quantities {
		q, err := k8sresource.ParseQuantity(quantity)
		if err != nil {
			return "", err
		}
		result.Add(q)
	}

	return result.String(), nil
}

func awsValidID(id string) string {
	return strings.Replace(id, ":", "__", -1)
}

// instanceTypeCPUQuantity returns the vCPUs of an instance type provided as k8sresource.Quantity represented as string
func instanceTypeCPUQuantity(context *templateContext, instanceType string) (string, error) {
	// get the instance type info
	instanceTypeInfo, err := context.instanceTypes.InstanceInfo(instanceType)

	if err != nil {
		return "", err
	}

	cpu := fmt.Sprintf("%d", instanceTypeInfo.VCPU)

	return cpu, nil
}

// instanceTypeMemoryQuantity returns the memory of an instance type provided as k8sresource.Quantity represented as string
func instanceTypeMemoryQuantity(context *templateContext, instanceType string) (string, error) {
	// get the instance type info
	instanceTypeInfo, err := context.instanceTypes.InstanceInfo(instanceType)

	if err != nil {
		return "", err
	}

	memory := k8sresource.NewQuantity(instanceTypeInfo.Memory, k8sresource.BinarySI)

	return memory.String(), nil
}

// scaleQuantity scales a k8sresource.Quantity by a factor, represented as string
// returns the k8sresource.Quantity and an error if the scaling factor is less than or equal to 0.0
func scaleQuantity(quantityStr string, factor float32) (string, error) {
	// validate scaling factor
	if factor <= 0.0 {
		return quantityStr, fmt.Errorf("scaling factor must be greater than 0.0")
	}

	// parse the quantity as k8sresource.Quantity
	quantity, err := k8sresource.ParseQuantity(quantityStr)
	if err != nil {
		return quantityStr, fmt.Errorf("failed to parse %v as k8sresource.Quantity: %v", quantityStr, err)
	}
	// CPU quantities can be represented as milli-cores, so we need to scale them differently
	if quantity.Value() < 1024 && quantity.Format == k8sresource.DecimalSI {
		quantity.SetMilli(int64(float32(quantity.MilliValue()) * factor))
	} else {
		// memory quantities can be represented as binary SI, so we need to scale them differently
		scaledValue := int64(float32(quantity.Value()) * factor)
		quantity.Set(scaledValue)
	}
	return quantity.String(), nil
}
