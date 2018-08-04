package provisioner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/kms/kmsiface"
	"github.com/stretchr/testify/require"
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
	encodedData := base64.StdEncoding.EncodeToString([]byte(data))
	return fmt.Sprintf(fileTemplate, path, encodedData, permissions, encrypted)
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
