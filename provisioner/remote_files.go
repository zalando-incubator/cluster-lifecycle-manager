package provisioner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"path"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/kms/kmsiface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	"github.com/zalando-incubator/cluster-lifecycle-manager/channel"
	"gopkg.in/yaml.v2"
)

type FilesRenderer struct {
	awsAdapter *awsAdapter
	cluster    *api.Cluster
	config     channel.Config
	directory  string
	nodePool   *api.NodePool
}

func (f *FilesRenderer) RenderAndUploadFiles(
	values map[string]interface{},
	bucketName string,
	kmsKey string,
) (string, error) {

	var (
		manifest channel.Manifest
		err      error
	)
	if f.nodePool == nil {
		// assume it's etcd
		manifest, err = f.config.EtcdManifest(filesTemplateName)
	} else {
		manifest, err = f.config.NodePoolManifest(
			f.nodePool.Profile,
			filesTemplateName,
		)
	}
	if err != nil {
		return "", err
	}

	rendered, err := renderSingleTemplate(
		manifest,
		f.cluster,
		f.nodePool,
		values,
		f.awsAdapter,
	)
	if err != nil {
		return "", err
	}

	archive, err := makeArchive(rendered, kmsKey, f.awsAdapter.kmsClient)
	if err != nil {
		return "", err
	}

	userDataHash, err := generateDataHash([]byte(rendered))
	if err != nil {
		return "", fmt.Errorf("failed to generate hash of userdata: %v", err)
	}

	filename := path.Join(f.cluster.LocalID, f.directory, userDataHash)

	_, err = f.awsAdapter.s3Uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(filename),
		Body:   bytes.NewReader(archive),
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("s3://%s/%s", bucketName, filename), nil
}

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
		err = tarWriter.WriteHeader(&tar.Header{
			Name: finalPath,
			Size: int64(len(compressionInput)),
			Mode: remoteFile.Permissions,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to write header: %v", err)
		}
		_, err = tarWriter.Write(compressionInput)
		if err != nil {
			return nil, fmt.Errorf("failed to write to tar archive: %v", err)
		}
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
