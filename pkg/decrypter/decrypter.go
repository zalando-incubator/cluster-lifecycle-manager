package decrypter

import "strings"

// SecretDecrypter is a map of decrypters.
type SecretDecrypter map[string]Decrypter

// Decrypt tries to find the right decrypter for the secret based on the secret
// prefix e.g. 'aws:kms:'. If the decrypter is found it will attempt to decrypt
// the secret and return it in plaintext.
func (s SecretDecrypter) Decrypt(secret string) (string, error) {
	if strings.HasPrefix(secret, AWSKMSSecretPrefix) {
		if decrypter, ok := s[AWSKMSSecretPrefix]; ok {
			return decrypter.Decrypt(strings.TrimPrefix(secret, AWSKMSSecretPrefix))
		}
	}

	// in case no decrypter is found we just return the secret as
	// 'plaintext'
	return secret, nil
}

// Decrypter is an interface describing any type of secret decrypter which
// given an ecrypted secret can return the decrypted plaintext value.
type Decrypter interface {
	Decrypt(secret string) (string, error)
}
