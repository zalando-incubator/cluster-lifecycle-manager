package provisioner

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	awsUtils "github.com/zalando-incubator/cluster-lifecycle-manager/pkg/aws"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
)

func render(_ *testing.T, templates map[string]string, templateName string, data interface{}, adapter *awsAdapter, instanceTypes *awsUtils.InstanceTypes) (string, error) {
	templateData := make(map[string][]byte, len(templates))

	for name, content := range templates {
		templateData[name] = []byte(content)
	}

	context := newTemplateContext(templateData, &api.Cluster{}, nil, map[string]interface{}{"data": data}, adapter, instanceTypes)
	return renderTemplate(context, templateName)
}

func renderSingle(t *testing.T, template string, data interface{}) (string, error) {
	return render(
		t,
		map[string]string{"foo.yaml": template},
		"foo.yaml",
		data,
		nil,
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
		nil,
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
		nil,
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
		nil,
		nil)

	require.Error(t, err)
}

func TestSha256(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ printf "%.32s" (.Values.data | sha256) }}`,
		"hello")

	require.NoError(t, err)
	require.EqualValues(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e", result)
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

func TestSplit(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		result, err := renderSingle(
			t,
			`{{ range $index, $element := split .Values.data "," }}{{ $index }}={{ $element}}{{end}}`,
			"")

		require.NoError(t, err)
		require.Equal(t, "", result)
	})

	t.Run("single", func(t *testing.T) {
		result, err := renderSingle(
			t,
			`{{ range $index, $element := split .Values.data "," }}{{ $index }}={{ $element}}{{end}}`,
			"foo")

		require.NoError(t, err)
		require.Equal(t, "0=foo", result)
	})

	t.Run("multiple", func(t *testing.T) {
		result, err := renderSingle(
			t,
			`{{ range $index, $element := split .Values.data "," }}{{ $index }}={{ $element}}{{end}}`,
			"foo,bar")

		require.NoError(t, err)
		require.Equal(t, "0=foo1=bar", result)
	})
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

func TestParseSGIngressRanges(t *testing.T) {
	testTemplate := `{{- if index .Values.data.sgIngressRanges -}}
{{- range $index, $element := sgIngressRanges .Values.data.sgIngressRanges -}}
- CidrIp: {{ $element.CIDR }}
  FromPort: {{ $element.FromPort }}
  IpProtocol: {{ $element.Protocol }}
  ToPort: {{ $element.ToPort }}
{{ end -}}
{{- end -}}`

	r, err := renderSingle(t, testTemplate, map[string]string{"sgIngressRanges": "10.0.0.0/8:0-100,0.0.0.0/0:300-400,127.0.0.1/32:500,udp:0.0.0.0/0:53"})
	require.NoError(t, err)
	require.Equal(t, `- CidrIp: 10.0.0.0/8
  FromPort: 0
  IpProtocol: tcp
  ToPort: 100
- CidrIp: 0.0.0.0/0
  FromPort: 300
  IpProtocol: tcp
  ToPort: 400
- CidrIp: 127.0.0.1/32
  FromPort: 500
  IpProtocol: tcp
  ToPort: 500
- CidrIp: 0.0.0.0/0
  FromPort: 53
  IpProtocol: udp
  ToPort: 53
`, r, "rendered template is incorrect")

	r, err = renderSingle(t, testTemplate, map[string]string{"sgIngressRanges": ""})
	require.NoError(t, err)
	require.Equal(t, "", r, "rendered template is not empty")

	_, err = renderSingle(t, `{{ sgIngressRanges "0.0.0.0/0:0-1-2 }}`, "")
	require.Error(t, err)

	_, err = renderSingle(t, `{{ sgIngressRanges "0.0.0.0/0:0-1,-2 }}`, "")
	require.Error(t, err)

	_, err = renderSingle(t, `{{ sgIngressRanges "0.0.0.0/0:30-20" }}`, "")
	require.Error(t, err)

	_, err = renderSingle(t, `{{ sgIngressRanges "0.0.0.0/0:0-200000" }}`, "")
	require.Error(t, err)

	_, err = renderSingle(t, `{{ sgIngressRanges "10.0.0:0-1" }}`, "")
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
				&adapter,
				nil)

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
		podCIDR       int64
		cidr          int64
		reserved      int64
		expected      string
		expectedError bool
	}{
		{
			name:     "basic",
			podCIDR:  16,
			cidr:     24,
			expected: "256",
		},
		{
			name:     "basic+reserved",
			podCIDR:  16,
			cidr:     24,
			reserved: 10,
			expected: "246",
		},
		{
			name:     "large",
			podCIDR:  16,
			cidr:     27,
			expected: "2048",
		},
		{
			name:          "error: too small",
			podCIDR:       16,
			cidr:          19,
			expectedError: true,
		},
		{
			name:          "error: too large",
			podCIDR:       16,
			cidr:          29,
			expectedError: true,
		},
		{
			name:          "error: pod CIDR too small",
			podCIDR:       13,
			cidr:          24,
			expectedError: true,
		},
		{
			name:          "error: pod CIDR too large",
			podCIDR:       17,
			cidr:          24,
			expectedError: true,
		},
		{
			name:     "large podCIDR",
			podCIDR:  15,
			cidr:     26,
			expected: "2048",
		},
		{
			name:     "large podCIDR, small node CIDR",
			podCIDR:  15,
			cidr:     25,
			expected: "1024",
		},
		{
			name:     "very large podCIDR",
			podCIDR:  14,
			cidr:     26,
			expected: "4096",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := renderSingle(t, "{{ nodeCIDRMaxNodesPodCIDR .Values.data.pod_cidr .Values.data.cidr .Values.data.reserved }}", map[string]int64{
				"pod_cidr": tc.podCIDR,
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
	for _, tc := range []struct {
		name          string
		itemTemplate  string
		length        string
		expected      string
		expectedError bool
	}{{
		name:     "empty template",
		length:   "3",
		expected: ",,",
	}, {
		name:         "no placeholder",
		itemTemplate: "foo.bar.baz",
		length:       "3",
		expected:     "foo.bar.baz,foo.bar.baz,foo.bar.baz",
	}, {
		name:         "multiple placeholders",
		itemTemplate: "foo$.bar$.baz$",
		length:       "3",
		expected:     "foo0.bar0.baz0,foo1.bar1.baz1,foo2.bar2.baz2",
	}, {
		name:          "negative length",
		itemTemplate:  "foo$.bar$.baz$",
		length:        "-42",
		expectedError: true,
	}, {
		name:         "zero length",
		itemTemplate: "foo$.bar$.baz$",
		length:       "0",
		expected:     "",
	}, {
		name:          "invalid string",
		itemTemplate:  "foo$.bar$.baz$",
		length:        "qux",
		expectedError: true,
	}, {
		name:         "common case",
		itemTemplate: "foo.bar$.baz",
		length:       "3",
		expected:     "foo.bar0.baz,foo.bar1.baz,foo.bar2.baz",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			const template = "{{ indexedList .Values.data.item (parseInt64 .Values.data.length) }}"
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

func TestZoneDistributedNodePoolGroupsDedicated(t *testing.T) {
	nodePools := []*api.NodePool{
		// Master pools are ignored
		{
			Name:    "default-master",
			Profile: "master-default",
		},

		// Non-dedicated pools are ignored
		{
			Name:    "default",
			Profile: "worker-splitaz",
		},
		{
			Name:        "default",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"taints": "dedicated=invalid:NoSchedule"},
		},

		// Both pools are OK
		{
			Name:        "valid-1",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=valid", "taints": "dedicated=valid:NoSchedule"},
		},
		{
			Name:        "valid-2",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=valid", "taints": "dedicated=valid:NoSchedule"},
		},

		// Both karpenter pools are OK
		{
			Name:        "valid-1",
			Profile:     "worker-karpenter",
			ConfigItems: map[string]string{"labels": "dedicated=valid-karpenter", "taints": "dedicated=valid-karpenter:NoSchedule"},
		},
		{
			Name:        "valid-2",
			Profile:     "worker-karpenter",
			ConfigItems: map[string]string{"labels": "dedicated=valid-karpenter", "taints": "dedicated=valid-karpenter:NoSchedule"},
		},

		// Just one pool
		{
			Name:        "valid-single-1",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=valid-single", "taints": "dedicated=valid-single:NoSchedule"},
		},

		// Pools doesn't have the correct taints
		{
			Name:        "invalid-taint-1",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=invalid-taint", "taints": "dedicated=invalid-taint:NoSchedule"},
		},
		{
			Name:        "invalid-taint-2",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=invalid-taint", "taints": "dedicated=invalid-taint:NoSchedule,another=value:NoSchedule"},
		},

		// Pool doesn't have a taint
		{
			Name:        "missing-taint-1",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=missing-taint", "taints": "dedicated=missing-taint:NoSchedule"},
		},
		{
			Name:        "missing-taint-2",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=missing-taint"},
		},

		// Pool is limited to some AZs
		{
			Name:        "explicit-azs-1",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=explicit-azs", "taints": "dedicated=explicit-azs:NoSchedule"},
		},
		{
			Name:        "explicit-azs-2",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=explicit-azs", "taints": "dedicated=explicit-azs:NoSchedule", "availability_zones": "1a,1b,1c"},
		},

		// Pool has the wrong profile
		{
			Name:        "wrong-profile-1",
			Profile:     "worker-splitaz",
			ConfigItems: map[string]string{"labels": "dedicated=wrong-profile", "taints": "dedicated=wrong-profile:NoSchedule"},
		},
		{
			Name:        "wrong-profile-2",
			Profile:     "worker-default",
			ConfigItems: map[string]string{"labels": "dedicated=wrong-profile", "taints": "dedicated=wrong-profile:NoSchedule"},
		},
	}

	result, err := renderSingle(t, `{{ range $k, $v := zoneDistributedNodePoolGroups .Values.data.pools }}{{ if ne $k "" }}{{ $k }};{{ end }}{{ end }}`, map[string]interface{}{"pools": nodePools})
	require.NoError(t, err)
	require.Equal(t, "valid;valid-karpenter;valid-single;", result)
}

func TestZoneDistributedNodePoolGroupsDefault(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pools    []*api.NodePool
		expected bool
	}{
		{
			name: "all pools match",
			pools: []*api.NodePool{
				{
					Name:    "default",
					Profile: "worker-splitaz",
				},
				{
					Name:    "default-2",
					Profile: "worker-splitaz",
				},
			},
			expected: true,
		},
		{
			name: "pools with taints are ignored",
			pools: []*api.NodePool{
				{
					Name:    "default",
					Profile: "worker-splitaz",
				},
				{
					Name:        "default-2",
					Profile:     "worker-default",
					ConfigItems: map[string]string{"taints": "foo=bar:NoSchedule"},
				},
			},
			expected: true,
		},
		{
			name: "master node pools are ignored",
			pools: []*api.NodePool{
				{
					Name:    "default",
					Profile: "worker-splitaz",
				},
				{
					Name:    "default-master",
					Profile: "master-default",
				},
			},
			expected: true,
		},
		{
			name: "pools with AZ restrictions are not allowed",
			pools: []*api.NodePool{
				{
					Name:    "default",
					Profile: "worker-splitaz",
				},
				{
					Name:        "default-2",
					Profile:     "worker-splitaz",
					ConfigItems: map[string]string{"availability_zones": "1a,1b,1c"},
				},
			},
			expected: false,
		},
		{
			name: "pools with non-splitaz profiles are not allowed",
			pools: []*api.NodePool{
				{
					Name:    "default",
					Profile: "worker-splitaz",
				},
				{
					Name:    "default-2",
					Profile: "worker-default",
				},
			},
			expected: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := renderSingle(t, `{{ index (zoneDistributedNodePoolGroups .Values.data.pools) "" }}`, map[string]interface{}{"pools": tc.pools})
			require.NoError(t, err)
			require.Equal(t, strconv.FormatBool(tc.expected), result)
		})
	}
}

func TestCertificateExpiry(t *testing.T) {
	exampleCert := `-----BEGIN CERTIFICATE-----
MIICoDCCAYgCCQCICOd8jmc77jANBgkqhkiG9w0BAQsFADASMRAwDgYDVQQDDAdl
eGFtcGxlMB4XDTIxMDYxMDEwMDY0M1oXDTIyMDYxMDEwMDY0M1owEjEQMA4GA1UE
AwwHZXhhbXBsZTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAMYr2Lsz
Wm3I1GqxdqnuuXqT/SHWKv/CdGorE5nb1O6OBFibo0TJN8ztoooySqwF81Qh9Uwu
mA5mkScdWJagqYlGsR+d1U3wuGmY9jSXdIn5VX0PUWD38MazT+s2kzZVg8Xu/CC8
waBdQDZGKpJeO/z1LC8zoY9P3f3YuxmgQqzDfpJgzjSEaSqhgIDD7RCA3kngfzYP
J3T+O54NpTNU5fZx437e7L643arZdB636yyV6dGz3ZV3WZw9TeLry6mf671BWvsN
Ngkz0fmG1rNgzD7gwn6jTG29p5O9f3djX2oH2aHUb7ry+n40QmxVUM+JO3OgVg99
ZV2jRxqfVNse61UCAwEAATANBgkqhkiG9w0BAQsFAAOCAQEAiLJJgKyP1aFJK+jL
T3E9EZfiYzWE301DMzkhCDVcAEY8KNsugQPn/dNiAWcZB+JLEWby0LFyQcPVE5eu
TLvNJLT6Iui7ITNC4bbrIqJxdKdeHX2Y/gj4j2mtHupiLHkJoLrahjAG8JrIDpMt
MFsSQS6YJ87TjYgtlNlOLa+/k771rS9qG/uIK8+Ijx8Y3HYboS5zMVyMdELFufof
0rjsnygpvicwVEyZU0d4sCVwyX3I9OtCUTI/CY/3UqqL2LhNEq3hYPNDFDJV9zXE
T6qW9CgZFGg83VqV2Tz44pneTFzvbr7Kcvrhpe0Wr2Ed2zPsz5BSz194DopAdRYv
CWeOoA==
-----END CERTIFICATE-----`
	res, err := renderSingle(t, `{{ certificateExpiry .Values.data.certificate }}`, map[string]interface{}{"certificate": exampleCert})
	require.NoError(t, err)
	require.Equal(t, "2022-06-10T10:06:43Z", res)
}

func TestSumQuantities(t *testing.T) {
	for _, tc := range []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "whole add zero",
			template: `{{ sumQuantities "2" "0" }}`,
			expected: "2",
		},
		{
			name:     "whole addition",
			template: `{{ sumQuantities "2" "3" }}`,
			expected: "5",
		},
		{
			name:     "whole subtraction",
			template: `{{ sumQuantities "5" "-3" }}`,
			expected: "2",
		},
		{
			name:     "fraction add zero",
			template: `{{ sumQuantities "256m" "0" }}`,
			expected: "256m",
		},
		{
			name:     "whole CPU add fraction",
			template: `{{ sumQuantities "2" "256m" }}`,
			expected: "2256m",
		},
		{
			name:     "whole CPU add fraction sub whole",
			template: `{{ sumQuantities "2" "256m" "-1" }}`,
			expected: "1256m",
		},
		{
			name:     "whole CPU add fraction add whole",
			template: `{{ sumQuantities "2" "256m" "1" }}`,
			expected: "3256m",
		},
		{
			name:     "whole CPU sub fraction",
			template: `{{ sumQuantities "2" "-256m" }}`,
			expected: "1744m",
		},
		{
			name:     "whole CPU sub fraction sub whole CPU",
			template: `{{ sumQuantities "2" "-256m" "-1" }}`,
			expected: "744m",
		},
		{
			name:     "Gi sub Mi",
			template: `{{ sumQuantities "2Gi" "-512Mi" }}`,
			expected: "1536Mi",
		},
		{
			name:     "Gi sub Ki",
			template: `{{ sumQuantities "2Gi" "-1024Ki" }}`,
			expected: "2047Mi",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := renderSingle(t, tc.template, nil)
			require.NoError(t, err)
			require.Equal(t, tc.expected, res)
		})
	}
}

func TestAWSValidID(t *testing.T) {
	result, err := renderSingle(t, `{{ .Values.data.id | awsValidID }}`, map[string]interface{}{"id": "aws:12345678910:eu-central-1:kube-1"})
	require.NoError(t, err)
	require.Equal(t, "aws__12345678910__eu-central-1__kube-1", result)
}

func TestNodePoolGroupsProfile(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    []*api.NodePool
		expected map[string]string
	}{
		{
			name: "application-1 dedicated pools share the same profile",
			input: []*api.NodePool{
				{
					Name:    "example-1",
					Profile: "worker-combined",
					ConfigItems: map[string]string{
						"labels": "dedicated=application-1",
					},
				},
				{
					Name:    "example-2",
					Profile: "worker-combined",
					ConfigItems: map[string]string{
						"labels": "dedicated=application-1",
					},
				},
			},
			expected: map[string]string{
				"application-1": "zalando",
			},
		},
		{
			name: "application-1 dedicated pools share the same profile, which is unknown",
			input: []*api.NodePool{
				{
					Name:    "example-1",
					Profile: "worker-unknown",
					ConfigItems: map[string]string{
						"labels": "dedicated=application-1",
					},
				},
				{
					Name:    "example-2",
					Profile: "worker-unknown",
					ConfigItems: map[string]string{
						"labels": "dedicated=application-1",
					},
				},
			},
			expected: map[string]string{
				"application-1": "",
			},
		},
		{
			name: "application-2 dedicated pools do not share the same profile",
			input: []*api.NodePool{
				{
					Name:    "example-1",
					Profile: "profile-2",
					ConfigItems: map[string]string{
						"labels": "dedicated=application-2",
					},
				},
				{
					Name:    "example-2",
					Profile: "profile-1",
					ConfigItems: map[string]string{
						"labels": "dedicated=application-2",
					},
				},
			},
			expected: map[string]string{
				"application-2": "",
			},
		},
		{
			name: "default pools share the same profile",
			input: []*api.NodePool{
				{
					Name:    "example-1",
					Profile: "worker-karpenter",
				},
				{
					Name:    "example-2",
					Profile: "worker-karpenter",
				},
			},
			expected: map[string]string{
				"default": "karpenter",
			},
		},
		{
			name: "default pools do not share the same profile",
			input: []*api.NodePool{
				{
					Name:    "example-1",
					Profile: "worker-karpenter",
				},
				{
					Name:    "example-2",
					Profile: "worker-combined",
				},
			},
			expected: map[string]string{
				"default": "",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := nodeLifeCycleProviderPerNodePoolGroup(tc.input)
			require.Equal(t, tc.expected, output)
		})
	}
}

func TestDict(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ define "a-template" -}}
name: {{ .name }}
version: {{ .version }}
{{ end }}

{{ template "a-template" dict "name" "foo" "version" .Values.data }}
`,
		"1")

	require.NoError(t, err)
	require.EqualValues(t, `

name: foo
version: 1

`, result)
}

func TestDictInvalidArgs(t *testing.T) {
	for i, tc := range []struct {
		args []interface{}
	}{
		{args: []interface{}{}},
		{args: []interface{}{"foo"}},
		{args: []interface{}{1, "foo"}},
		{args: []interface{}{"foo", "bar", "foo", "baz"}},
	} {
		t.Run(fmt.Sprintf("%d: %v", i, tc.args), func(t *testing.T) {
			_, err := dict(tc.args...)
			require.Error(t, err)
		})
	}
}

func TestScaleQuantity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		quantity k8sresource.Quantity
		factor   float32
		expected k8sresource.Quantity
	}{
		{
			name:     "whole CPU scaled by whole",
			quantity: k8sresource.MustParse("1.0"),
			factor:   2.0,
			expected: k8sresource.MustParse("2.0"),
		},
		{
			name:     "whole CPU scaled by fraction",
			quantity: k8sresource.MustParse("10"),
			factor:   0.5,
			expected: k8sresource.MustParse("5"),
		},
		{
			name:     "fraction CPU scaled by whole",
			quantity: k8sresource.MustParse("256m"),
			factor:   2.0,
			expected: k8sresource.MustParse("512m"),
		},
		{
			name:     "fraction CPU scaled by fraction",
			quantity: k8sresource.MustParse("256m"),
			factor:   0.5,
			expected: k8sresource.MustParse("128m"),
		},
		{
			name:     "memory scaled by whole",
			quantity: k8sresource.MustParse("1Gi"),
			factor:   2.0,
			expected: k8sresource.MustParse("2Gi"),
		},
		{
			name:     "memory scaled by fraction",
			quantity: k8sresource.MustParse("1Gi"),
			factor:   0.5,
			expected: k8sresource.MustParse("512Mi"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := scaleQuantity(tc.quantity, tc.factor)
			require.NoError(t, err)
			require.EqualValues(t, tc.expected.String(), result.String())
		})
	}
}

func TestScaleQuantityError(t *testing.T) {
	_, err := scaleQuantity(k8sresource.MustParse("1.0"), -1.0)
	require.Error(t, err)
}
