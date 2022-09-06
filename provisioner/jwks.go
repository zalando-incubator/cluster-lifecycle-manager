package provisioner

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"

	"github.com/pkg/errors"
	"gopkg.in/square/go-jose.v2"
)

const (
	kubernetesProgrammaticAuthorization = "urn:kubernetes:programmatic_authorization"
	idTokenResponse                     = "id_token"
	publicSubject                       = "public"
	jwksUrl                             = "%s/openid-configuration/keys.json"
	subjectClaim                        = "sub"
	issuerClaim                         = "iss"
	jwkUse                              = "sig"
)

// Provider contains the subset of the OpenID Connect provider metadata needed to request
// and verify ID Tokens.
type Provider struct {
	Issuer                 string   `json:"issuer"`
	AuthURL                string   `json:"authorization_endpoint"`
	JWKSURL                string   `json:"jwks_uri"`
	SupportedResponseTypes []string `json:"response_types_supported"`
	SupportedSubjectTypes  []string `json:"subject_types_supported"`
	AlgorithmsSupported    []string `json:"id_token_signing_alg_values_supported"`
	SupportedClaims        []string `json:"claims_supported"`
}

// copied from kubernetes/kubernetes#78502
func keyIDFromPublicKey(publicKey interface{}) (string, error) {
	publicKeyDERBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to serialize public key to DER format: %v", err)
	}

	hasher := crypto.SHA256.New()
	_, err = hasher.Write(publicKeyDERBytes)
	if err != nil {
		return "", fmt.Errorf("failed to write sha256 of public key: %v", err)
	}
	publicKeyDERHash := hasher.Sum(nil)

	keyID := base64.RawURLEncoding.EncodeToString(publicKeyDERHash)

	return keyID, nil
}

type KeyResponse struct {
	Keys []map[string]string `json:"keys"`
}

func generateJWKSDocument(serviceAccountKey string) (string, error) {
	block, _ := pem.Decode([]byte(serviceAccountKey))
	if block == nil {
		return "", errors.Errorf("Error decoding serviceAccountKey")
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse public key: %v", err)
	}

	var alg jose.SignatureAlgorithm
	switch pubKey.(type) {
	case *rsa.PublicKey:
		alg = jose.RS256
	default:
		return "", errors.New("Public key was not RSA")
	}

	kid, err := keyIDFromPublicKey(pubKey)
	if err != nil {
		return "", err
	}
	key := jose.JSONWebKey{
		Key:       pubKey,
		KeyID:     kid,
		Algorithm: string(alg),
		Use:       jwkUse,
	}
	keys, err := getStringMapList(key)
	if err != nil {
		return "", fmt.Errorf("failed to get key list: %v", err)
	}
	keyResponse := KeyResponse{Keys: keys}
	response, err := json.MarshalIndent(keyResponse, "", "    ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal key response: %v", err)
	}
	return string(response), nil
}

// getStringMapList is needed because the default serializer does not preserve an empty "kid" field
func getStringMapList(key jose.JSONWebKey) ([]map[string]string, error) {
	keyJSON, err := json.Marshal(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshall key: %v", err)
	}
	var keyMap map[string]string
	err = json.Unmarshal(keyJSON, &keyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshall key json: %v", err)
	}
	keyMapCopy := make(map[string]string, len(keyMap))
	for k, v := range keyMap {
		keyMapCopy[k] = v
	}
	keyMapCopy["kid"] = ""
	return []map[string]string{keyMap, keyMapCopy}, nil
}

func generateOIDCDiscoveryDocument(apiServerUrl string) (string, error) {
	provider := Provider{
		Issuer:                 apiServerUrl,
		AuthURL:                kubernetesProgrammaticAuthorization,
		JWKSURL:                fmt.Sprintf(jwksUrl, apiServerUrl),
		SupportedResponseTypes: []string{idTokenResponse},
		SupportedSubjectTypes:  []string{publicSubject},
		AlgorithmsSupported:    []string{string(jose.RS256)},
		SupportedClaims:        []string{subjectClaim, issuerClaim},
	}

	document, err := json.MarshalIndent(provider, "", "    ")
	if err != nil {
		return "", fmt.Errorf("failed to serialize oidc discovery document: %v", err)
	}
	return string(document), nil
}
