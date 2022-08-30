package platformiam

import (
	"fmt"
	"os"
	"path"
	"time"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/credentials-loader/jwt"
	"golang.org/x/oauth2"
)

// NewTokenSource returns a platformIAMTokenSource
func NewTokenSource(name string, path string) oauth2.TokenSource {
	return &tokenSource{
		name: name,
		path: path,
	}
}

// NewClientCredentialsSource returns a NewPlatformIAMClientCredentialsSource
func NewClientCredentialsSource(name string, path string) ClientCredentialsSource {
	return &clientCredentialsSource{
		name: name,
		path: path,
	}
}

// ClientCredentialsSource is an interface that allows access to client credentials
type ClientCredentialsSource interface {
	ClientCredentials() (*ClientCredentials, error)
}

type source struct {
	name string
	path string
}

// tokenSource defines a source that contains a token
type tokenSource source

// clientCredentialsSource defines a source that contains client credentials
type clientCredentialsSource source

func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Token returns a token or an error.
// Token must be safe for concurrent use by multiple goroutines.
// The returned Token must not be modified.
func (p *tokenSource) Token() (*oauth2.Token, error) {
	token, err := readFileContent(path.Join(p.path, fmt.Sprintf("%s-token-secret", p.name)))
	if err != nil {
		return nil, err
	}

	tokenType, err := readFileContent(path.Join(p.path, fmt.Sprintf("%s-token-type", p.name)))
	if err != nil {
		return nil, err
	}

	// parse the token claims to get expiry time
	claims, err := jwt.ParseClaims(token)
	if err != nil {
		return nil, err
	}

	return &oauth2.Token{
		AccessToken: token,
		TokenType:   tokenType,
		Expiry:      time.Unix(int64(claims.Exp), 0),
	}, nil
}

// ClientCredentials contains ID and Secret to use to authenticate
type ClientCredentials struct {
	ID     string
	Secret string
}

// ClientCredentials returns a pointer to a ClientCredentials object or an error
func (p *clientCredentialsSource) ClientCredentials() (*ClientCredentials, error) {
	id, err := readFileContent(path.Join(p.path, fmt.Sprintf("%s-client-id", p.name)))
	if err != nil {
		return nil, err
	}

	secret, err := readFileContent(path.Join(p.path, fmt.Sprintf("%s-client-secret", p.name)))
	if err != nil {
		return nil, err
	}

	return &ClientCredentials{
		ID:     id,
		Secret: secret,
	}, nil
}
