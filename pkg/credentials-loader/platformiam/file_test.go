package platformiam

import (
	"os"
	"testing"
	"time"
)

const (
	testToken = "HEADER.eyJzdWIiOiJpZCIsImh0dHBzOi8vaWRlbnRpdHkuemFsYW5kby5jb20vcmVhbG0iOiJ1c2VycyIsImh0dHBzOi8vaWRlbnRpdHkuemFsYW5kby5jb20vdG9rZW4iOiJCZWFyZXIiLCJodHRwczovL2lkZW50aXR5LnphbGFuZG8uY29tL21hbmFnZWQtaWQiOiJ1c2VyIiwiYXpwIjoiZ28tenRva2VuIiwiaHR0cHM6Ly9pZGVudGl0eS56YWxhbmRvLmNvbS9icCI6ImJwIiwiYXV0aF90aW1lIjoxNDk4MzE3MjIzLCJpc3MiOiJodHRwczovL2lkZW50aXR5LnphbGFuZG8uY29tIiwiZXhwIjoxNDk5MjM1OTgzLCJpYXQiOjE0OTkyMzIzNzN9.SIGNATURE"
)

var (
	// expiry time in token
	expiryTime = time.Date(2017, 07, 05, 06, 26, 23, 0, time.UTC)
)

func CreateTempTokenFiles() {
	_ = os.WriteFile("foo-token-type", []byte("Bearer"), 0644)
	_ = os.WriteFile("foo-token-secret", []byte(testToken), 0644)
}

func CreateTempClientCredentialsFiles() {
	_ = os.WriteFile("bar-client-secret", []byte("HksjnJHhhjshd"), 0644)
	_ = os.WriteFile("bar-client-id", []byte("foo-bar"), 0644)
}

func DeleteTempTokenFiles() {
	_ = os.Remove("foo-token-type")
	_ = os.Remove("foo-token-secret")
}

func DeleteTempClientCredentialsFiles() {
	_ = os.Remove("bar-client-secret")
	_ = os.Remove("bar-client-id")
}

func TestToken(t *testing.T) {
	CreateTempTokenFiles()
	defer DeleteTempTokenFiles()

	p := NewTokenSource("foo", ".")
	token, err := p.Token()
	if err != nil {
		t.Error(err)
	}
	t.Logf("Token: %s, type: %s, expiry: %s", token.AccessToken, token.TokenType, token.Expiry)
	if token.AccessToken != testToken && token.TokenType != "Bearer" && token.Expiry != expiryTime {
		t.Errorf("Error, expecting token: %s, got: %s and type: %s, got: %s", "token.foo.bar", token.AccessToken, "Bearer", token.TokenType)
	}
}

func TestClientCredentials(t *testing.T) {
	CreateTempClientCredentialsFiles()
	defer DeleteTempClientCredentialsFiles()

	p := NewClientCredentialsSource("bar", ".")
	cred, err := p.ClientCredentials()
	if err != nil {
		t.Error(err)
	}
	t.Logf("Client ID: %s, Client Secret: %s", cred.ID, cred.Secret)
	if cred.ID != "foo-bar" && cred.Secret != "HksjnJHhhjshd" {
		t.Errorf("Error, expecting ID: %s, got: %s and Secret: %s, got: %s", "foo-bar", cred.ID, "HksjnJHhhjshd", cred.Secret)
	}
}
