package provisioner

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/zalando-incubator/cluster-lifecycle-manager/api"
	yaml "gopkg.in/yaml.v2"
)

// populateEncryptedEtcdValues is a horrible hack that is needed to avoid doing an update of the Taupage-based CloudFormation stack every time
// we run. Unfortunately it's pretty much unavoidable because we rely on the KMS-encrypted config items inside the launch template, and if we
// didn't do this, every run would create new ciphertext for the same plaintext value and then trigger a rolling update. We can drop it and switch
// to a saner scheme (same as what we do with Kubernetes) once we migrate away from Taupage, and then this whole mess can be dropped.
func populateEncryptedEtcdValues(adapter *awsAdapter, cluster *api.Cluster, etcdKMSKeyARN string, values map[string]interface{}) error {
	stack, err := adapter.getStackByName(etcdStackName)
	if err != nil && !isDoesNotExistsErr(err) {
		return err
	}

	// Try to reload the values from the existing launch template
	if stack != nil {
		for _, output := range stack.Outputs {
			if aws.StringValue(output.OutputKey) == "LaunchTemplateId" {
				launchTemplateID := aws.StringValue(output.OutputValue)

				versions, err := adapter.ec2Client.DescribeLaunchTemplateVersions(&ec2.DescribeLaunchTemplateVersionsInput{
					LaunchTemplateId: aws.String(launchTemplateID),
					Versions:         []*string{aws.String("$Latest")},
				})
				if err != nil {
					return err
				}

				if len(versions.LaunchTemplateVersions) != 1 {
					return fmt.Errorf("expected 1 version in launch template %s, found %d", launchTemplateID, len(versions.LaunchTemplateVersions))
				}

				userData, err := base64.StdEncoding.DecodeString(aws.StringValue(versions.LaunchTemplateVersions[0].LaunchTemplateData.UserData))
				if err != nil {
					return err
				}

				if bytes.HasPrefix(userData, []byte("#taupage-ami-config")) {
					var result struct {
						Environment struct {
							ClientCert string `yaml:"CLIENT_CERT"`
							ClientKey  string `yaml:"CLIENT_KEY"`
							ClientCA   string `yaml:"CLIENT_TRUSTED_CA"`
						} `yaml:"environment"`
						ScalyrKey string `yaml:"scalyr_account_key"`
					}
					err = yaml.Unmarshal(userData, &result)
					if err != nil {
						return err
					}

					for k, v := range map[string]string{
						"etcd_client_server_cert": result.Environment.ClientCert,
						"etcd_client_server_key":  result.Environment.ClientKey,
						"etcd_client_ca_cert":     result.Environment.ClientCA,
						"etcd_scalyr_key":         result.ScalyrKey,
					} {
						decrypted, err := decryptTaupageKMSValue(adapter, v)
						if err != nil {
							return err
						}

						if cluster.ConfigItems[k] == decrypted {
							// Keep the existing value
							values[k] = v
						}
					}
				}
			}
		}
	}

	// Fill in the missing config items
	for _, ci := range []string{"etcd_client_server_cert", "etcd_client_server_key", "etcd_client_ca_cert", "etcd_scalyr_key"} {
		if _, ok := values[ci]; ok {
			continue
		}

		encrypted, err := adapter.kmsEncryptForTaupage(etcdKMSKeyARN, cluster.ConfigItems[ci])
		if err != nil {
			return err
		}
		values[ci] = encrypted
	}

	return nil
}

func decryptTaupageKMSValue(adapter *awsAdapter, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "aws:kms:"))
	if err != nil {
		return "", err
	}
	decrypted, err := adapter.kmsClient.Decrypt(&kms.DecryptInput{
		CiphertextBlob: decoded,
	})
	if err != nil {
		return "", err
	}
	return string(decrypted.Plaintext), nil
}
