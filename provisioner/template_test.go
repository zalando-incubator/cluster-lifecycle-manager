package provisioner

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

func exampleCluster(pools []*api.NodePool) *api.Cluster {
	return &api.Cluster{
		ConfigItems: map[string]string{
			"autoscaling_buffer_pools":           "worker",
			"autoscaling_buffer_cpu_scale":       "0.75",
			"autoscaling_buffer_memory_scale":    "0.75",
			"autoscaling_buffer_cpu_reserved":    "1200m",
			"autoscaling_buffer_memory_reserved": "3500Mi",
		},
		NodePools: pools,
	}
}

func render(t *testing.T, templates map[string]string, templateName string, data interface{}, adapter *awsAdapter) (string, error) {
	templateData := make(map[string][]byte, len(templates))

	for name, content := range templates {
		templateData[name] = []byte(content)
	}

	context := newTemplateContext(templateData, &api.Cluster{}, nil, map[string]interface{}{"data": data}, adapter)
	return renderTemplate(context, templateName)
}

func renderSingle(t *testing.T, template string, data interface{}) (string, error) {
	return render(
		t,
		map[string]string{"foo.yaml": template},
		"foo.yaml",
		data,
		nil)
}

func TestTemplating(t *testing.T) {
	result, err := renderSingle(
		t,
		"foo {{ .Values.data }}",
		"1")

	require.NoError(t, err)
	require.EqualValues(t, "foo 1", result)
}

func TestBase64Encode(t *testing.T) {
	result, err := renderSingle(
		t,
		"{{ .Values.data | base64 }}",
		"abc123")

	require.NoError(t, err)
	require.EqualValues(t, "YWJjMTIz", result)
}

func TestBase64Decode(t *testing.T) {
	result, err := renderSingle(
		t,
		"{{ .Values.data | base64Decode }}",
		"YWJjMTIz")

	require.NoError(t, err)
	require.EqualValues(t, "abc123", result)
}

func TestManifestHash(t *testing.T) {
	result, err := render(
		t,
		map[string]string{
			"dir/config.yaml": "foo {{ .Values.data }}",
			"dir/foo.yaml":    `{{ manifestHash "config.yaml" }}`,
		},
		"dir/foo.yaml",
		"abc123",
		nil)

	require.NoError(t, err)
	require.EqualValues(t, "82b883f3662dfed3357ba6c497a77684b1d84468c6aa49bf89c4f209889ddc77", result)
}

func TestManifestHashMissingFile(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"foo.yaml": `{{ manifestHash "missing.yaml" }}`,
		},
		"foo.yaml",
		"",
		nil)

	require.Error(t, err)
}

func TestManifestHashRecursiveInclude(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"config.yaml": `{{ manifestHash "foo.yaml" }}`,
			"foo.yaml":    `{{ manifestHash "config.yaml" }}`,
		},
		"foo.yaml",
		"",
		nil)

	require.Error(t, err)
}

func renderAutoscaling(t *testing.T, cluster *api.Cluster) (string, error) {
	return renderSingle(
		t,
		`{{ with autoscalingBufferSettings .Values.data }}{{.CPU}} {{.Memory}}{{end}}`,
		cluster)
}

func TestAutoscalingBufferExplicit(t *testing.T) {
	cluster := exampleCluster([]*api.NodePool{})
	cluster.ConfigItems["autoscaling_buffer_cpu"] = "111m"
	cluster.ConfigItems["autoscaling_buffer_memory"] = "1500Mi"

	result, err := renderAutoscaling(t, cluster)

	require.NoError(t, err)
	require.EqualValues(t, "111m 1500Mi", result)
}

func TestAutoscalingBufferExplicitOnlyOne(t *testing.T) {
	cluster := exampleCluster([]*api.NodePool{})
	cluster.ConfigItems["autoscaling_buffer_cpu"] = "111m"

	_, err := renderAutoscaling(t, cluster)
	require.Error(t, err)

	delete(cluster.ConfigItems, "autoscaling_buffer_cpu")
	cluster.ConfigItems["autoscaling_buffer_memory"] = "1500Mi"

	_, err = renderAutoscaling(t, cluster)
	require.Error(t, err)
}

func TestAutoscalingBufferPoolBasedScale(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ with autoscalingBufferSettings .Values.data }}{{.CPU}} {{.Memory}}{{end}}`,
		exampleCluster([]*api.NodePool{
			{
				InstanceTypes: []string{"m4.xlarge"},
				Name:          "master-default",
			},
			{
				// 2 vcpu / 8gb
				InstanceTypes: []string{"m4.large"},
				Name:          "worker-default",
			},
			{
				// 4 vcpu / 7.5gb
				InstanceTypes: []string{"c4.xlarge"},
				Name:          "worker-cpu",
			},
		}))

	require.NoError(t, err)
	require.EqualValues(t, "800m 4180Mi", result)
}

func TestAutoscalingBufferPoolBasedReserved(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ with autoscalingBufferSettings .Values.data }}{{.CPU}} {{.Memory}}{{end}}`,
		exampleCluster([]*api.NodePool{
			{
				// 8 vcpu / 32gb
				InstanceTypes: []string{"m4.2xlarge"},
				Name:          "worker-default",
			},
		}))

	require.NoError(t, err)
	require.EqualValues(t, "6 24Gi", result)
}

func TestAutoscalingBufferPoolBasedNoPools(t *testing.T) {
	_, err := renderSingle(
		t,
		`{{ with autoscalingBufferSettings .Values.data }}{{.CPU}} {{.Memory}}{{end}}`,
		exampleCluster([]*api.NodePool{
			{
				InstanceTypes: []string{"m4.xlarge"},
				Name:          "master-default",
			},
			{
				InstanceTypes: []string{"m4.large"},
				Name:          "testing-default",
			},
		}))

	require.Error(t, err)
}

func TestAutoscalingBufferPoolBasedMismatchingType(t *testing.T) {
	_, err := renderSingle(
		t,
		`{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
		exampleCluster([]*api.NodePool{
			{
				InstanceTypes: []string{"r4.large"},
				Name:          "worker-one",
			},
			{
				InstanceTypes: []string{"c4.xlarge"},
				Name:          "worker-two",
			},
		}))

	require.Error(t, err)
}

func TestAutoscalingBufferPoolBasedInvalidSettings(t *testing.T) {
	configSets := []map[string]string{
		// missing
		{"autoscaling_buffer_cpu_scale": "0.8", "autoscaling_buffer_memory_scale": "0.8"},
		{"autoscaling_buffer_pools": "worker", "autoscaling_buffer_memory_scale": "0.8"},
		{"autoscaling_buffer_pools": "worker", "autoscaling_buffer_cpu_scale": "0.8"},
		// invalid
		{"autoscaling_buffer_pools": "[(", "autoscaling_buffer_cpu_scale": "0.8", "autoscaling_buffer_memory_scale": "0.8"},
		{"autoscaling_buffer_pools": "worker", "autoscaling_buffer_cpu_scale": "sdfsdfsdf", "autoscaling_buffer_memory_scale": "0.8"},
		{"autoscaling_buffer_pools": "worker", "autoscaling_buffer_cpu_scale": "0.8", "autoscaling_buffer_memory_scale": "fgdfgdfg"},
	}

	for _, configItems := range configSets {
		cluster := exampleCluster([]*api.NodePool{
			{
				InstanceTypes: []string{"m4.large"},
				Name:          "worker",
			},
		})
		cluster.ConfigItems = configItems

		_, err := renderSingle(
			t,
			`{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
			cluster)

		assert.Error(t, err, "configItems: %s", configItems)
	}
}

func TestASGSize(t *testing.T) {
	result, err := renderSingle(
		t,
		"{{ asgSize 9 3 }}",
		"")

	require.NoError(t, err)
	require.EqualValues(t, "3", result)
}

func TestASGSizeError(t *testing.T) {
	_, err := renderSingle(
		t,
		"{{ asgSize 8 3 }}",
		"")

	require.Error(t, err)
}

func TestAZID(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ azID "eu-central-1a" }}`,
		"")

	require.NoError(t, err)
	require.EqualValues(t, "1a", result)
}

func TestAZCountSimple(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ azCount .Values.data }}`,
		map[string]string{
			"*":             "subnet-foo,subnet-bar,subnet-baz",
			"eu-central-1a": "subnet-foo",
			"eu-central-1b": "subnet-bar",
			"eu-central-1c": "subnet-baz",
		})

	require.NoError(t, err)
	require.EqualValues(t, "3", result)
}

func TestAZCountStarOnly(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ azCount .Values.data }}`,
		map[string]string{
			"*": "",
		})

	require.NoError(t, err)
	require.EqualValues(t, "0", result)
}

func TestAZCountNoSubnets(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ azCount .Values.data }}`,
		map[string]string{})

	require.NoError(t, err)
	require.EqualValues(t, "0", result)
}

func TestMountUnitName(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ mountUnitName "/foo/bar" }}`,
		"")

	require.NoError(t, err)
	require.EqualValues(t, "foo-bar", result)
}

func TestMountUnitNameRelativePath(t *testing.T) {
	_, err := renderSingle(
		t,
		`{{ mountUnitName "foo/bar" }}`,
		"")

	require.Error(t, err)
}

func TestAccountID(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ accountID "aws:12345" }}`,
		"")
	require.NoError(t, err)
	require.EqualValues(t, "12345", result)
}

func TestAccountIDFailsOnInvalid(t *testing.T) {
	_, err := renderSingle(
		t,
		`{{ accountID "aws12345" }}`,
		"")
	require.Error(t, err)
}

func TestParsePortRanges(t *testing.T) {
	testTemplate := `{{- if index .Values.data.portRanges -}}
{{- range $index, $element := portRanges .Values.data.portRanges -}}
- CidrIp: 0.0.0.0/0
  FromPort: {{ $element.FromPort }}
  IpProtocol: tcp
  ToPort: {{ $element.ToPort }}
{{ end -}}
{{- end -}}`

	r, err := renderSingle(t, testTemplate, map[string]string{"portRanges": "0-100,300-400"})
	require.NoError(t, err)
	require.Equal(t, `- CidrIp: 0.0.0.0/0
  FromPort: 0
  IpProtocol: tcp
  ToPort: 100
- CidrIp: 0.0.0.0/0
  FromPort: 300
  IpProtocol: tcp
  ToPort: 400
`, r, "rendered template is incorrect")

	r, err = renderSingle(t, testTemplate, map[string]string{"portRanges": ""})
	require.NoError(t, err)
	require.Equal(t, "", r, "rendered template is not empty")

	_, err = renderSingle(t, `{{ portRanges "0-1-2 }}`, "")
	require.Error(t, err)

	_, err = renderSingle(t, `{{ portRanges "0-1,-2 }}`, "")
	require.Error(t, err)

	_, err = renderSingle(t, `{{ portRanges "30-20" }}`, "")
	require.Error(t, err)

	_, err = renderSingle(t, `{{ portRanges "0-200000" }}`, "")
	require.Error(t, err)
}

func TestSplitHostPort(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ with splitHostPort "example.org:80" }}{{.Host}} - {{.Port}}{{end}}`,
		nil)

	require.NoError(t, err)
	require.EqualValues(t, "example.org - 80", result)
}

func TestSplitHostPortError(t *testing.T) {
	_, err := renderSingle(
		t,
		`{{ with splitHostPort "a:b:c" }}{{.Host}} - {{.Port}}{{end}}`,
		nil)

	require.Error(t, err)
}

func TestPublicKey(t *testing.T) {
	privkey := `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAw6bd0oGYHTvzz+hbSBeym87rt/jHMEshthOuY2szifqroBzI
gpARyzjjpaH2/QnYzgqOWpj2fbtuVYhSrclWqi8QdCKz83VneXe26IW/MGNmO/sT
CssB1NPuoBWalBZMisbKeqvOqR7BxwCpKjerseEPE/oriPn3QEXOYqaY0eOCK/6Q
6eSFxMd9n6p+o64htrt4q+rTxX1fUodPRuEf4QnSrEEbZq44hUkzmr4VCZi/LQrc
EjfcpxFrM8PMvALaSqf21xHvFqF46Gj777h+7rv6fEfK5T+Twhb76Pc1Cz15dHDl
qfbaVYjaEO0qSRHGJsT2A45zm/8zCwBpJKAtDwIDAQABAoIBACBkJeFN90MPw+Ot
0j7zPWyyKzBADaofJiugwoRPIS88wuE1IrUK6Qc+GeI4GE34LV6fPMYfAN/8Ad5D
PXzsEl8Gf7DadfRegY0Ils2UJvz519kiThrBVUJI+/6g1QCjWHS5SJhajVJOd0Jd
B6SnptNCMV7bUg3RZG/NnseSUUaeG3JG5bsISiJPlPcZHU+rqUnQDnrB+/0ykGoi
grhbtSt896zP+4gdjg3dLUi5UwGT93/h9vc2azK5HbVYuJzO/CClsAp+QfiI4Ia5
5Tz4lnQ/yDYxT9mmrxV0hzr5hULnO/oPsdc2x7bp1gvjejQwkY61b8PTg8GrdhEu
JJLVpXECgYEA9CxEoOowsH1yM8pE7XPzCvh9IwBYGt+6EEoEEQ6fzhNP54dJdFiF
l6J/bTHLDLWuYWq3AZ+8wYOnYev2G8LIKauvQZNyOZlSUU4RL2htMZ31bFeELWA/
cuixeZycTJBTuxAqtwWvyjJDmKYZAzdQReSNygaVcFHju6hpu0VxXq0CgYEAzSDw
cMdz6WoGE0RiHNjhViFbENl6HoO9Cq/qzXcBoCf+1Yl85/AS3s+982IjyPqcbo/Z
GEi7sq6pTnLUZKIImbIidDvIkEkxTmbNfS7151ILTHULMy4c8YPyTfo6h+vrO6XT
zGuKtcc3K6txDbyb2abMg35t0Ljg0RN+togRHisCgYBPzSQE32VYWTd427OZU5rs
S/hB9zvUVKhv6HDZzkjGRiOITPvhzYij3VT+MBbnqX07k3AKVNWQ/WE4LLE7s3ZN
wDHAIdtkHcr8jaIqN1vwqmpqpVOqrNkvygMu9tNSZp0m9wqu1Gn2kGTtP+PO3EYd
AayhiXNPyUO/sjQUI4cA5QKBgHURKWeTzMkXYyQ30K6Z7/AR1UEGfLVRhd/FigF8
u4bFjKAdeRV9Y6eZc9Sk27tlm0VV/xXm3IgbOjC1RBWyi6n7icJAJDSEMQmHjhq1
ZE2B+0TFP4ET/hyvqudpuWG8+GDwQLHXZjBb41ae30RxsZhDo1AgJVgLSvLHZ3eQ
rARFAoGBAL1QriPbz2ZOujrSmPKbOl/H0y74QwmWwatf90cgzMA+/gNgmu8EYmC7
zXJIFt24pCciexrRWOeR9LZdenWUXOvl2I+cKlIGLM42MxUQpN74BlHne0oQ+Mg3
SDpur492ci/fjCMLtPFEmmQvdAjzC8cSHu8MqpLAEvxFBFKz+Q0T
-----END RSA PRIVATE KEY-----`
	pubkey := `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAw6bd0oGYHTvzz+hbSBey
m87rt/jHMEshthOuY2szifqroBzIgpARyzjjpaH2/QnYzgqOWpj2fbtuVYhSrclW
qi8QdCKz83VneXe26IW/MGNmO/sTCssB1NPuoBWalBZMisbKeqvOqR7BxwCpKjer
seEPE/oriPn3QEXOYqaY0eOCK/6Q6eSFxMd9n6p+o64htrt4q+rTxX1fUodPRuEf
4QnSrEEbZq44hUkzmr4VCZi/LQrcEjfcpxFrM8PMvALaSqf21xHvFqF46Gj777h+
7rv6fEfK5T+Twhb76Pc1Cz15dHDlqfbaVYjaEO0qSRHGJsT2A45zm/8zCwBpJKAt
DwIDAQAB
-----END PUBLIC KEY-----
`

	result, err := renderSingle(
		t,
		`{{ publicKey .Values.data }}`,
		privkey)
	require.NoError(t, err)
	require.EqualValues(t, pubkey, result)
}

func TestStupsNATSubnets(t *testing.T) {
	for _, tc := range []struct {
		vpc     string
		subnets string
	}{
		{
			vpc:     "172.31.0.0/16",
			subnets: "172.31.64.0/28 172.31.64.16/28 172.31.64.32/28 ",
		},
		{
			vpc:     "10.153.192.0/19",
			subnets: "10.153.200.0/28 10.153.200.16/28 10.153.200.32/28 ",
		},

		{
			vpc:     "10.149.64.0/19",
			subnets: "10.149.72.0/28 10.149.72.16/28 10.149.72.32/28 ",
		},
	} {
		result, err := renderSingle(
			t,
			`{{ range $elem := stupsNATSubnets .Values.data }}{{$elem}} {{ end }}`,
			tc.vpc)

		require.NoError(t, err)
		require.EqualValues(t, tc.subnets, result)
	}
}

func TestStupsNATSubnetsErrors(t *testing.T) {
	for _, vpc := range []string{
		"172.31.0.0/25",
		"2001::/19",
		"example",
	} {
		_, err := renderSingle(
			t,
			`{{ stupsNATSubnets .Values.data }}`,
			vpc)
		require.Error(t, err)
	}
}

type mockEC2Client struct {
	ec2iface.EC2API
	t               *testing.T
	kubernetesImage string
	ownerID         string
	output          []*ec2.Image
}

func (c mockEC2Client) DescribeImages(input *ec2.DescribeImagesInput) (*ec2.DescribeImagesOutput, error) {
	require.Len(c.t, input.Filters, 2)
	require.Equal(c.t, describeImageFilterNameName, *input.Filters[0].Name)
	require.Len(c.t, input.Filters[0].Values, 1)
	require.Equal(c.t, c.kubernetesImage, *input.Filters[0].Values[0])
	require.Equal(c.t, describeImageFilterNameOwner, *input.Filters[1].Name)
	require.Len(c.t, input.Filters[1].Values, 1)
	require.Equal(c.t, c.ownerID, *input.Filters[1].Values[0])
	return &ec2.DescribeImagesOutput{Images: c.output}, nil
}

func TestAmiID(t *testing.T) {
	for _, tc := range []struct {
		name      string
		imageName string
		ownerID   string
		imageID   string
		output    []*ec2.Image
		expectErr bool
	}{
		{
			name:      "basic",
			imageName: "kubernetes-image-ami",
			ownerID:   "8085",
			imageID:   "ami-0001dsf",
			output:    []*ec2.Image{{ImageId: aws.String("ami-0001dsf")}},
			expectErr: false,
		},
		{
			name:      "multiple images",
			imageName: "kubernetes-image-ami",
			ownerID:   "8085",
			output:    []*ec2.Image{{ImageId: aws.String("ami-00232ccd")}, {ImageId: aws.String("ami-0001dsf")}},
			expectErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := awsAdapter{ec2Client: mockEC2Client{t: t, kubernetesImage: tc.imageName, ownerID: tc.ownerID, output: tc.output}}
			result, err := render(
				t,
				map[string]string{
					"foo.yaml": fmt.Sprintf(`{{ amiID "%s" "%s" }}`, tc.imageName, tc.ownerID),
				},
				"foo.yaml",
				"abc123",
				&adapter)

			if !tc.expectErr {
				require.NoError(t, err)
				require.EqualValues(t, tc.imageID, result)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestNodeCIDRMaxNodes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cidr          int64
		reserved      int64
		expected      string
		expectedError bool
	}{
		{
			name:     "basic",
			cidr:     24,
			expected: "256",
		},
		{
			name:     "basic+reserved",
			cidr:     24,
			reserved: 10,
			expected: "246",
		},
		{
			name:     "large",
			cidr:     27,
			expected: "2048",
		},
		{
			name:          "error: too small",
			cidr:          19,
			expectedError: true,
		},
		{
			name:          "error: too large",
			cidr:          29,
			expectedError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := renderSingle(t, "{{ nodeCIDRMaxNodes .Values.data.cidr .Values.data.reserved }}", map[string]int64{
				"cidr":     tc.cidr,
				"reserved": tc.reserved,
			})
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, tc.expected, result)
			}
		})
	}
}

func TestNodeCIDRMaxPods(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cidr          int64
		extraCapacity int64
		expected      string
		expectedError bool
	}{
		{
			name:     "basic",
			cidr:     24,
			expected: "110",
		},
		{
			name:          "basic+extra",
			cidr:          24,
			extraCapacity: 10,
			expected:      "110",
		},
		{
			name:     "larger",
			cidr:     25,
			expected: "64",
		},
		{
			name:          "large",
			cidr:          27,
			extraCapacity: 5,
			expected:      "21",
		},
		{
			name:          "error: too small",
			cidr:          19,
			expectedError: true,
		},
		{
			name:          "error: too large",
			cidr:          29,
			expectedError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := renderSingle(t, "{{ nodeCIDRMaxPods .Values.data.cidr .Values.data.extra_capacity }}", map[string]int64{
				"cidr":           tc.cidr,
				"extra_capacity": tc.extraCapacity,
			})
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, tc.expected, result)
			}
		})
	}
}

func TestParseInt64(t *testing.T) {
	result, err := renderSingle(t, `{{ parseInt64 "1234" }}`, nil)
	require.NoError(t, err)
	require.EqualValues(t, "1234", result)
}

func TestParseInt64Error(t *testing.T) {
	_, err := renderSingle(t, `{{ parseInt64 "foobar" }}`, nil)
	require.Error(t, err)
}

func TestKubernetesSizeToBytes(t *testing.T) {
	for _, tc := range []struct {
		input  string
		output string
		scale  float64
	}{
		{
			input:  "1Ki",
			output: "1KB",
			scale:  1,
		},
		{
			input:  "1Gi",
			output: "524288KB",
			scale:  0.5,
		},
		{
			input:  "4Gi",
			output: "3355444KB",
			scale:  0.8,
		},
	} {
		t.Run(tc.input, func(t *testing.T) {
			bytes, err := kubernetesSizeToKiloBytes(tc.input, tc.scale)
			require.NoError(t, err)
			require.Equal(t, tc.output, bytes)
		})
	}
}

func TestExtractEndpointHosts(t *testing.T) {
	for _, tc := range []struct {
		endpoints string
		expected  string
	}{
		{
			endpoints: "etcd-server.etcd.example.org:2379",
			expected:  "etcd-server.etcd.example.org, ",
		},
		{
			endpoints: "http://etcd-server.etcd.example.org:2379",
			expected:  "etcd-server.etcd.example.org, ",
		},
		{
			endpoints: "http://etcd-server.etcd.example.org:2379,zalan.do:2479",
			expected:  "etcd-server.etcd.example.org, zalan.do, ",
		},
		{
			endpoints: "http://etcd-server.etcd.example.org:2379,https://etcd-server.etcd.example.org:2479",
			expected:  "etcd-server.etcd.example.org, ",
		},
	} {
		t.Run(tc.endpoints, func(t *testing.T) {
			result, err := renderSingle(t, `{{ range $elem := extractEndpointHosts .Values.data.endpoints }}{{ $elem }}, {{ end }}`, map[string]interface{}{
				"endpoints": tc.endpoints,
			})
			require.NoError(t, err)
			require.EqualValues(t, tc.expected, result)
		})
	}
}

func TestIndexedList(t *testing.T) {
	// length not positive
	// length 3

	for _, tc := range []struct {
		name          string
		itemTemplate  string
		length        int
		expected      string
		expectedError bool
	}{{
		name:     "empty template",
		length:   3,
		expected: ",,",
	}, {
		name:         "no placeholder",
		itemTemplate: "foo.bar.baz",
		length:       3,
		expected:     "foo.bar.baz,foo.bar.baz,foo.bar.baz",
	}, {
		name:         "multiple placeholders",
		itemTemplate: "foo$.bar$.baz$",
		length:       3,
		expected:     "foo0.bar0.baz0,foo1.bar1.baz1,foo2.bar2.baz2",
	}, {
		name:          "negative length",
		itemTemplate:  "foo$.bar$.baz$",
		length:        -42,
		expectedError: true,
	}, {
		name:         "zero length",
		itemTemplate: "foo$.bar$.baz$",
		expected:     "",
	}, {
		name:         "common case",
		itemTemplate: "foo.bar$.baz",
		length:       3,
		expected:     "foo.bar0.baz,foo.bar1.baz,foo.bar2.baz",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			const template = "{{ indexedList .Values.data.item .Values.data.length }}"
			args := map[string]interface{}{"item": tc.itemTemplate, "length": tc.length}
			result, err := renderSingle(t, template, args)
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, tc.expected, result)
			}
		})
	}
}
