package decrypter

import (
	"encoding/base64"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
)

const (
	// AWSKMSSecretPrefix is the secret prefix for secrets which can be
	// descrypted by the awsKMS decrypter.
	AWSKMSSecretPrefix = "aws:kms:"
)

// awsKMS is a decrypter which can decrypt kms encrypted secrets.
type awsKMS struct {
	kmsSvc *kms.KMS
}

// NewAWSKMSDescrypter initializes a new awsKMS based Decrypter.
func NewAWSKMSDescrypter(sess *session.Session) Decrypter {
	return &awsKMS{
		kmsSvc: kms.New(sess),
	}
}

// Decrypt decrypts a kms encrypted secret.
func (a *awsKMS) Decrypt(secret string) (string, error) {
	decodedSecret, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return "", err
	}

	params := &kms.DecryptInput{
		CiphertextBlob: decodedSecret,
	}

	resp, err := a.kmsSvc.Decrypt(params)
	if err != nil {
		return "", err
	}

	return string(resp.Plaintext), nil
}
