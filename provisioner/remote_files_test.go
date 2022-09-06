package provisioner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/kms/kmsiface"
	"github.com/stretchr/testify/require"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
)

const (
	testBucket = "cluster-lifecycle-manager-0000000000-eu-central-1"

	etcdFiles = `files:
  - path: /etc/etcd/ssl/ca.cert
    data: "{{ .Cluster.ConfigItems.etcd_client_ca_cert }}"
    permissions: 0400
    encrypted: false
  - path: /etc/etcd/ssl/client.cert
    data: "{{ .Cluster.ConfigItems.etcd_client_server_cert }}"
    permissions: 0400
    encrypted: false
  - path: /etc/etcd/ssl/client.key.enc
    data: "{{ .Cluster.ConfigItems.etcd_client_server_key }}"
    permissions: 0400
    encrypted: true
`

	workerPoolFiles = `files:
  - path: /etc/kubernetes/.local-id
    data: "{{ .Cluster.LocalID | base64 }}"
    permissions: 0400
  - path: /etc/kubernetes/ssl/worker.pem
    data: {{ .Cluster.ConfigItems.worker_cert }}
    permissions: 0400
    encrypted: false
  - path: /etc/kubernetes/ssl/worker-key.pem
    data: {{ .Cluster.ConfigItems.worker_key }}
    permissions: 0400
    encrypted: true
  - path: /etc/kubernetes/ssl/ca.pem
    data: {{ .Cluster.ConfigItems.ca_cert_decompressed }}
    permissions: 0400
    encrypted: false
`
)

var fileTemplate = `
files:
  - path: %s
    data: %s
    permissions: %d
    encrypted: %t
`

type testKMSClient struct {
	kmsiface.KMSAPI
}

func (testKMSClient) Encrypt(input *kms.EncryptInput) (*kms.EncryptOutput, error) {
	inputS := string(input.Plaintext)
	outputS := fmt.Sprintf("xx%sxx", inputS)
	return &kms.EncryptOutput{CiphertextBlob: []byte(outputS)}, nil
}

func makeTestInput(path, data string, permissions int64, encrypted bool) string {
	encodedData := base64Encode(data)
	return fmt.Sprintf(fileTemplate, path, encodedData, permissions, encrypted)
}

func TestRenderAndUploadFiles(t *testing.T) {
	tempDir := channel.CreateTempDir(t)
	defer os.RemoveAll(tempDir)

	channel.SetupConfig(
		t,
		tempDir,
		map[string]string{
			"cluster/etcd/files.yaml":                      etcdFiles,
			"cluster/node-pools/worker-default/files.yaml": workerPoolFiles,
		},
	)

	testConfig, err := channel.NewSimpleConfig("main", tempDir, false)
	if err != nil {
		t.Fatalf("error creating config: %v", err)
	}

	testCluster := api.SampleCluster()
	testCluster.ConfigItems["etcd_client_ca_cert"] = "dGVzdF9jYV9jZXJ0"
	testCluster.ConfigItems["etcd_client_server_cert"] = "Y2xpZW50X2NlcnQ="
	testCluster.ConfigItems["etcd_client_server_key"] = "dGVzdF9zZXJ2ZXJfa2V5"
	testCluster.ConfigItems["worker_cert"] = "d29ya2VyX2NlcnQ="
	testCluster.ConfigItems["worker_key"] = "d29ya2VyX2tleQ=="
	testCluster.ConfigItems["ca_cert_decompressed"] = "Y2FfY2VydA=="

	testAWSAdapter := newAWSAdapterWithStubs("", "test")
	testAWSAdapter.kmsClient = &testKMSClient{}

	tcs := []struct {
		Directory string
		NodePool  *api.NodePool
	}{
		{
			Directory: "etcd",
			NodePool:  nil,
		},
		{
			Directory: "worker",
			NodePool:  testCluster.NodePools[1],
		},
	}

	for _, tc := range tcs {
		renderer := &FilesRenderer{
			awsAdapter: testAWSAdapter,
			cluster:    testCluster,
			config:     testConfig,
			directory:  tc.Directory,
			nodePool:   tc.NodePool,
		}

		s3Location, err := renderer.RenderAndUploadFiles(
			map[string]interface{}{},
			testBucket,
			"test-key",
		)

		if err != nil {
			t.Errorf("error rendering template(%s): %v", tc.Directory, err)
			continue
		}

		for _, s := range []string{testBucket, "s3://", tc.Directory} {
			if !strings.Contains(s3Location, s) {
				t.Errorf("%q not found in bucket name: %s", s, s3Location)
				break
			}
		}
	}
}

func TestMakeArchive(t *testing.T) {
	for _, tc := range []struct {
		Message     string
		Path        string
		Data        string
		Permissions int64
		Encrypted   bool
	}{
		{
			Message:     "basic",
			Path:        "/abc",
			Data:        "abc",
			Permissions: 0777,
			Encrypted:   false,
		},
		{
			Message:     "encrypted",
			Path:        "/abc",
			Data:        "abc",
			Permissions: 0777,
			Encrypted:   true,
		},
	} {
		t.Run(tc.Message, func(tt *testing.T) {
			testKMSClient := &testKMSClient{}
			archive, err := makeArchive(makeTestInput(tc.Path, tc.Data, tc.Permissions, tc.Encrypted), "test-key", testKMSClient)
			require.NoError(t, err)
			buffer := bytes.NewBuffer(archive)
			gzr, err := gzip.NewReader(buffer)
			require.NoError(t, err)
			tarReader := tar.NewReader(gzr)
			for {
				th, err := tarReader.Next()
				if err == io.EOF {
					break
				}
				if tc.Encrypted {
					require.Equal(t, th.Name, tc.Path+".enc")
				} else {
					require.Equal(t, th.Name, tc.Path)
				}

				buffer := bytes.Buffer{}
				_, err = buffer.ReadFrom(tarReader)
				require.NoError(t, err)
				contents := buffer.String()
				if tc.Encrypted {
					decrypted := contents[2 : len(contents)-2]
					require.Equal(t, decrypted, tc.Data)
				} else {
					require.Equal(t, contents, tc.Data)
				}
				require.Equal(t, th.Mode, tc.Permissions)
			}
		})
	}
}
