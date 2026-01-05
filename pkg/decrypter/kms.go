package decrypter

import (
	"context"
	"encoding/base64"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

const (
	// AWSKMSSecretPrefix is the secret prefix for secrets which can be
	// descrypted by the awsKMS decrypter.
	AWSKMSSecretPrefix = "aws:kms:"
)

// awsKMS is a decrypter which can decrypt kms encrypted secrets.
type awsKMS struct {
	kmsSvc *kms.Client
}

// NewAWSKMSDescrypter initializes a new awsKMS based Decrypter.
func NewAWSKMSDescrypter(cfg aws.Config) Decrypter {
	return &awsKMS{
		kmsSvc: kms.NewFromConfig(cfg),
	}
}

// Decrypt decrypts a kms encrypted secret.
func (a *awsKMS) Decrypt(ctx context.Context, secret string) (string, error) {
	decodedSecret, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return "", err
	}

	params := &kms.DecryptInput{
		CiphertextBlob: decodedSecret,
	}

	resp, err := a.kmsSvc.Decrypt(ctx, params)
	if err != nil {
		return "", err
	}

	return string(resp.Plaintext), nil
}
