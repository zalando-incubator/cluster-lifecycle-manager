package provisioner

import (
	"crypto/x509"
	"io/ioutil"
	"math"
	"net"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
)

const (
	caCert = `-----BEGIN CERTIFICATE-----
MIIEljCCAn4CCQDtrx7BkiBrMDANBgkqhkiG9w0BAQsFADANMQswCQYDVQQDDAJD
QTAeFw0xODEwMDkxMjMyMTJaFw0yMTA3MjkxMjMyMTJaMA0xCzAJBgNVBAMMAkNB
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA56zosrTB3ryc38oaFz4Y
gH7+WerGZq4G+LBiah1zcpteO5f8vryFv+x6BBExtrm0ArPNwMA/V4t1ReOquY/F
Hd2JcC7Eb5e7Aan4KiV0WJesEDVKNLGJwS3QKnKipS4i9qSUGR59sSZvZhgVDDPX
fpt5cyYdE+xj/oePDviJb1JbocPAqVJurpcyDKlPrPXDoo8mgyngFlF4pBG+XYgy
PL9O83M4w5cpsa4WcaKcGALm65zI5g9eJmDLCMai4kpQNMO8GL5MEvapyLAybVXZ
xMkdKHuS9SYuiRYtlDwX53AY7TD4ImtChCbe5H/rtZNTZBRwQEtzCmdusVdBmqOg
JBrEriyUGlnP4wnFKJwC6f+JVY7R6hZ+RpUVc5YvGXB3Owt/k9T3cEFsRBc8iJMh
IF7GmL6qOaQTCpn5MbJ7xoFdL7d550b2hZYq1tKrd6KEB1wengi2zIxVKT3skUpQ
PsEkdyRy/BuZTpoTPXZxZ3eAnBYbC5AwKKJg+/NUSN6Mli5ygHxICbNBhMWXGiFH
YXZMi8bDbbm39zdyB+tVRMsJ5uSYDktLT9bVipxItR5oIA0ucj0WMoweOZSM61T/
HDIYQ9uO9yOHj7GSNmrzFAKoiQhlgWF1OEO3c+Upj/oCt/srDa2tjAORxiOvKmfx
csa0UUM/1A5adwDHEKlxwLUCAwEAATANBgkqhkiG9w0BAQsFAAOCAgEAyRSqFrbn
bA7C89phXMvNCiJ506e1Sic/89x8Ke6HIJ0Sm0rpHsBNCZrH50JWtJk5CSrMQLA9
9zt5InG+jKK7AjVwxLUVoxCvu5NcOK60d1Q1W0tW94PzL6orb8rSJ3A2jC+gxm75
JMryFjRNdDprIh+Vaz5Z5kQZc0Uh+r9Vz4bv7YNUl9lRc0PlvzkMR6ZQQ9Tq6EKn
tQFk0yuNp0HRSHHPmxT4egDmY0Sc4lbtpTHP0NhXT0W5xDqH9JqS9pLxQ02sjWIO
C9Ah8d8MymurkSqeGth8GApbCQZVck26xClo7Km/1DJJ8sREkYX58Tb5eeXTCXm3
s9BIIFoXSfSfDVqkhApTF3wWOl/a81Ao/y+v8nVt8ubiBq7noJKG6mqVMflaA21a
lUdHseNtNDqHWuuOg7wNoyXi8zrLPxoobxQIN77lDGqLXWjr/G2kvTwSydGwtGja
Vu+nyZnVLf2DIpFL8OkfS9wegc4HxpDDr37k9Dc5h6Z8FPexzRRa3UI1leRxFCw7
4U6CJomxHKomzd12Nc7t2mkUDXLOY0Qc+IqIqGcsxTkKC+VtGz0jy+tGldi5yU/1
U4IEn273CqZhFeo9NAmT+cZogbbs/E7+R1jJbApD1S/LJi9JBYtzFHlLsG67ByBU
eFa0W6dsgjtCwRsq+/SPEyuvc/NYiPIBjtM=
-----END CERTIFICATE-----`

	caKey = `-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----`
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

func render(t *testing.T, templates map[string]string, templateName string, data interface{}) (string, error) {
	basedir, err := ioutil.TempDir(os.TempDir(), strings.Replace(t.Name(), "/", "_", -1))
	require.NoError(t, err, "unable to create temp dir")

	defer os.RemoveAll(basedir)

	for name, content := range templates {
		fullPath := path.Join(basedir, name)
		parentDir := path.Dir(fullPath)
		err := os.MkdirAll(parentDir, 0755)
		require.NoError(t, err, "error while creating %s", parentDir)
		err = ioutil.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err, "error while writing %s", fullPath)
	}

	context := newTemplateContext(basedir)
	return renderTemplate(context, path.Join(basedir, templateName), data)
}

func renderSingle(t *testing.T, template string, data interface{}) (string, error) {
	return render(
		t,
		map[string]string{"dir/foo.yaml": template},
		"dir/foo.yaml",
		data)
}

func TestTemplating(t *testing.T) {
	result, err := renderSingle(
		t,
		"foo {{ . }}",
		"1")

	require.NoError(t, err)
	require.EqualValues(t, "foo 1", result)
}

func TestBase64(t *testing.T) {
	result, err := renderSingle(
		t,
		"{{ . | base64 }}",
		"abc123")

	require.NoError(t, err)
	require.EqualValues(t, "YWJjMTIz", result)
}

func TestManifestHash(t *testing.T) {
	result, err := render(
		t,
		map[string]string{
			"dir/config.yaml": "foo {{ . }}",
			"dir/foo.yaml":    `{{ manifestHash "config.yaml" }}`,
		},
		"dir/foo.yaml",
		"abc123")

	require.NoError(t, err)
	require.EqualValues(t, "82b883f3662dfed3357ba6c497a77684b1d84468c6aa49bf89c4f209889ddc77", result)
}

func TestManifestHashMissingFile(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"dir/foo.yaml": `{{ manifestHash "missing.yaml" }}`,
		},
		"dir/foo.yaml",
		"")

	require.Error(t, err)
}

func TestManifestHashRecursiveInclude(t *testing.T) {
	_, err := render(
		t,
		map[string]string{
			"dir/config.yaml": `{{ manifestHash "foo.yaml" }}`,
			"dir/foo.yaml":    `{{ manifestHash "config.yaml" }}`,
		},
		"dir/foo.yaml",
		"")

	require.Error(t, err)
}

func renderAutoscaling(t *testing.T, cluster *api.Cluster) (string, error) {
	return renderSingle(
		t,
		`{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
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
		`{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
		exampleCluster([]*api.NodePool{
			{
				InstanceType: "m4.xlarge",
				Name:         "master-default",
			},
			{
				InstanceType: "t2.nano",
				Name:         "worker-small",
			},
			{
				// 2 vcpu / 8gb
				InstanceType: "m4.large",
				Name:         "worker-default",
			},
		}))

	require.NoError(t, err)
	require.EqualValues(t, "800m 4692Mi", result)
}

func TestAutoscalingBufferPoolBasedReserved(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
		exampleCluster([]*api.NodePool{
			{
				// 8 vcpu / 32gb
				InstanceType: "m4.2xlarge",
				Name:         "worker-default",
			},
		}))

	require.NoError(t, err)
	require.EqualValues(t, "6 24Gi", result)
}

func TestAutoscalingBufferPoolBasedNoPools(t *testing.T) {
	_, err := renderSingle(
		t,
		`{{ with autoscalingBufferSettings . }}{{.CPU}} {{.Memory}}{{end}}`,
		exampleCluster([]*api.NodePool{
			{
				InstanceType: "m4.xlarge",
				Name:         "master-default",
			},
			{
				InstanceType: "m4.large",
				Name:         "testing-default",
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
				InstanceType: "r4.large",
				Name:         "worker-one",
			},
			{
				InstanceType: "c4.xlarge",
				Name:         "worker-two",
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
				InstanceType: "m4.large",
				Name:         "worker",
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
		`{{ azCount . }}`,
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
		`{{ azCount . }}`,
		map[string]string{
			"*": "",
		})

	require.NoError(t, err)
	require.EqualValues(t, "0", result)
}

func TestAZCountNoSubnets(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ azCount . }}`,
		map[string]string{})

	require.NoError(t, err)
	require.EqualValues(t, "0", result)
}

func TestGenerateCertificateKey(t *testing.T) {
	result, err := renderSingle(
		t,
		`{{ $x := generateCertificate .key .crt "client" 1 "CN" "dns.example" "foo.com" "127.0.0.1" "10.1.1.1" }}{{$x.KeyPEM}}`,
		map[string]string{"crt": caCert, "key": caKey})

	require.NoError(t, err)
	_, err = parsePEMKey(result)
	require.NoError(t, err)
}

func TestGenerateCertificateCertificate(t *testing.T) {
	for _, tc := range []struct {
		name          string
		template      string
		cn            string
		validDuration time.Duration
		dnsNames      []string
		netIPs        []net.IP
		extKeyUsage   []x509.ExtKeyUsage
	}{
		{
			name:          "basic client",
			cn:            "test.example",
			validDuration: time.Hour * 24,
			dnsNames:      []string{"dns.example", "foo.com"},
			netIPs:        []net.IP{{127, 0, 0, 1}, {10, 1, 1, 1}},
			extKeyUsage:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			template:      `{{ $x := generateCertificate .key .crt "client" 1 "test.example" "dns.example" "foo.com" "127.0.0.1" "10.1.1.1" }}{{$x.CertificatePEM}}`,
		},
		{
			name:          "basic server",
			cn:            "server.example",
			validDuration: time.Hour * 24 * 10,
			dnsNames:      nil,
			netIPs:        nil,
			extKeyUsage:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			template:      `{{ $x := generateCertificate .key .crt "server" 10 "server.example" }}{{$x.CertificatePEM}}`,
		},
		{
			name:          "basic mixed",
			cn:            "server-and-client.example",
			validDuration: time.Hour * 24 * 365,
			dnsNames:      nil,
			netIPs:        nil,
			extKeyUsage:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
			template:      `{{ $x := generateCertificate .key .crt "client-and-server" 365 "server-and-client.example" }}{{$x.CertificatePEM}}`,
		},
	} {
		now := time.Now()

		t.Run(tc.name, func(t *testing.T) {
			result, err := renderSingle(
				t,
				tc.template,
				map[string]string{"crt": caCert, "key": caKey})

			require.NoError(t, err)

			cert, err := parsePEMCertificate(result)
			require.NoError(t, err)

			require.EqualValues(t, tc.cn, cert.Subject.CommonName)
			require.EqualValues(t, tc.dnsNames, cert.DNSNames)
			require.EqualValues(t, tc.netIPs, cert.IPAddresses)
			require.EqualValues(t, tc.extKeyUsage, cert.ExtKeyUsage)

			approximateNotAfter := now.Add(tc.validDuration)
			require.True(t, math.Abs(float64(approximateNotAfter.Unix()-cert.NotAfter.Unix())) < 5, "invalid NotAfter: expected ~%s, found %s", approximateNotAfter, cert.NotAfter)
		})
	}
}
