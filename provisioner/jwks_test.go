package provisioner

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type testDiscoveryDoc map[string][]map[string]string

func TestGenerateJWKSDocument(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pubKey, err := x509.MarshalPKIXPublicKey(key.Public())
	require.NoError(t, err)
	pemKey := pem.EncodeToMemory(&pem.Block{
		Type:    "RSA PUBLIC KEY",
		Headers: nil,
		Bytes:   pubKey,
	})
	document, err := generateJWKSDocument(string(pemKey))
	require.NoError(t, err)
	var testDoc testDiscoveryDoc
	err = json.Unmarshal([]byte(document), &testDoc)
	require.NoError(t, err)
	require.Contains(t, testDoc, "keys")
	keys := testDoc["keys"]
	require.Len(t, keys, 2)
	firstKey := keys[0]
	secondKey := keys[1]
	pubKeyModulus, err := base64UrlEncode(key.N.Bytes())
	require.NoError(t, err)
	require.Equal(t, firstKey["n"], string(pubKeyModulus))
	require.Equal(t, secondKey["n"], string(pubKeyModulus))
	exponent, err := intToByteArray(uint64(key.E))
	require.NoError(t, err)
	pubKeyExp, err := base64UrlEncode(exponent)
	require.NoError(t, err)
	require.Equal(t, firstKey["e"], string(pubKeyExp))
	require.Equal(t, secondKey["e"], string(pubKeyExp))
	require.Greater(t, len(firstKey["kid"]), 0)
	require.Equal(t, len(secondKey["kid"]), 0)
}

func intToByteArray(input uint64) ([]byte, error) {
	bigEndian := make([]byte, 8)
	binary.BigEndian.PutUint64(bigEndian, input)
	output := bytes.TrimLeft(bigEndian, "\x00")
	return output, nil
}

// base64UrlEncode is special url encoding scheme as per RFC 7518 https://tools.ietf.org/html/rfc7518#page-30
func base64UrlEncode(input []byte) ([]byte, error) {
	buffer := bytes.NewBuffer(nil)
	encoder := base64.NewEncoder(base64.URLEncoding, buffer)
	_, err := encoder.Write(input)
	if err != nil {
		return nil, err
	}
	err = encoder.Close()
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimRight(buffer.String(), "=")
	return []byte(trimmed), nil
}
