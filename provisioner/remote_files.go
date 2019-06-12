package provisioner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/kms/kmsiface"
	"gopkg.in/yaml.v2"
)

func makeArchive(input string, kmsKey string, kmsClient kmsiface.KMSAPI) ([]byte, error) {
	var data remoteData
	err := yaml.UnmarshalStrict([]byte(input), &data)
	if err != nil {
		return nil, err
	}

	var filesTar bytes.Buffer
	gzw := gzip.NewWriter(&filesTar)
	defer gzw.Close()
	tarWriter := tar.NewWriter(gzw)
	defer tarWriter.Close()
	for _, remoteFile := range data.Files {
		var compressionInput []byte
		var finalPath string

		decoded, err := base64Decode(remoteFile.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode data for file: %s", remoteFile.Path)
		}
		if remoteFile.Encrypted {
			output, err := kmsClient.Encrypt(&kms.EncryptInput{KeyId: &kmsKey, Plaintext: []byte(decoded)})
			if err != nil {
				return nil, err
			}
			compressionInput = output.CiphertextBlob
			finalPath = fmt.Sprintf("%s.enc", remoteFile.Path)
		} else {
			compressionInput = []byte(decoded)
			finalPath = remoteFile.Path
		}
		tarWriter.WriteHeader(&tar.Header{
			Name: finalPath,
			Size: int64(len(compressionInput)),
			Mode: remoteFile.Permissions,
		})
		tarWriter.Write(compressionInput)
	}
	err = tarWriter.Close()
	if err != nil {
		return nil, err
	}
	err = gzw.Close()
	if err != nil {
		return nil, err
	}
	return filesTar.Bytes(), nil
}
