package decrypter

import (
	"fmt"
	"testing"
)

type mockDecrypter struct{}

func (d mockDecrypter) Decrypt(_ string) (string, error) {
	return "", nil
}

type mockErrDecrypter struct{}

func (d mockErrDecrypter) Decrypt(_ string) (string, error) {
	return "", fmt.Errorf("failed")
}

func TestSecretDecrypterDecrypt(t *testing.T) {
	for _, ti := range []struct {
		msg       string
		decrypter SecretDecrypter
		secret    string
		success   bool
	}{
		{
			msg: "test successful decryption",
			decrypter: SecretDecrypter(map[string]Decrypter{
				AWSKMSSecretPrefix: mockDecrypter{},
			}),
			secret:  AWSKMSSecretPrefix + "my-secret",
			success: true,
		},
		{
			msg: "test returning the secret without decrypting",
			decrypter: SecretDecrypter(map[string]Decrypter{
				"custom:": mockDecrypter{},
			}),
			secret:  "custom:my-secret",
			success: true,
		},
		{
			msg: "test when decrypt failes",
			decrypter: SecretDecrypter(map[string]Decrypter{
				AWSKMSSecretPrefix: mockErrDecrypter{},
			}),
			secret:  AWSKMSSecretPrefix + "my-secret",
			success: false,
		},
	} {
		t.Run(ti.msg, func(t *testing.T) {
			_, err := ti.decrypter.Decrypt(ti.secret)
			if err != nil && ti.success {
				t.Errorf("should not fail: %s", err)
			}

			if err == nil && !ti.success {
				t.Errorf("expected error")
			}
		})
	}
}
