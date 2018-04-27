package jwt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	token                 = "HEADER.eyJzdWIiOiJpZCIsImh0dHBzOi8vaWRlbnRpdHkuemFsYW5kby5jb20vcmVhbG0iOiJ1c2VycyIsImh0dHBzOi8vaWRlbnRpdHkuemFsYW5kby5jb20vdG9rZW4iOiJCZWFyZXIiLCJodHRwczovL2lkZW50aXR5LnphbGFuZG8uY29tL21hbmFnZWQtaWQiOiJ1c2VyIiwiYXpwIjoiZ28tenRva2VuIiwiaHR0cHM6Ly9pZGVudGl0eS56YWxhbmRvLmNvbS9icCI6ImJwIiwiYXV0aF90aW1lIjoxNDk4MzE3MjIzLCJpc3MiOiJodHRwczovL2lkZW50aXR5LnphbGFuZG8uY29tIiwiZXhwIjoxNDk5MjM1OTgzLCJpYXQiOjE0OTkyMzIzNzN9.SIGNATURE"
	invalidPayload        = "HEADER.eyJzdWIiOiJpZCIsImh0dHBzOi8vaWRlbnRpdHkuemFsYW5kby5jb20vcmVhbG0iOiJ1c2VycyIsImh0dHBzOi8vaWRlbnRpdHkuemFsYW5kby5jb20vdG9rZW4iOiJCZWFyZXIiLCJodHRwczovL2lkZW50aXR5LnphbGFuZG8uY29tL21hbmFnZWQtaWQiOiJ1c2VyIiwiYXpwIjoiZ28tenRva2VuIiwiaHR0cHM6Ly9pZGVudGl0eS56YWxhbmRvLmNvbS9icCI6ImJwIiwiYXV0aF90aW1lIjoxNDk4MzE3MjIzLCJpc3MiOiJodHRwczovL2lkZW50aXR5LnphbGFuZG8uY29tIiwiZXhwIjoxNDk5MjM1OTgzLCJpYXQiOjE0OTkyMzIzNzM=.SIGNATURE"
	invalidPayloadSegment = "HEADER.|eyJzdWIiOiJpZCIsImh0dHBzOi8vaWRlbnRpdHkuemFsYW5kby5jb20vcmVhbG0iOiJ1c2VycyIsImh0dHBzOi8vaWRlbnRpdHkuemFsYW5kby5jb20vdG9rZW4iOiJCZWFyZXIiLCJodHRwczovL2lkZW50aXR5LnphbGFuZG8uY29tL21hbmFnZWQtaWQiOiJ1c2VyIiwiYXpwIjoiZ28tenRva2VuIiwiaHR0cHM6Ly9pZGVudGl0eS56YWxhbmRvLmNvbS9icCI6ImJwIiwiYXV0aF90aW1lIjoxNDk4MzE3MjIzLCJpc3MiOiJodHRwczovL2lkZW50aXR5LnphbGFuZG8uY29tIiwiZXhwIjoxNDk5MjM1OTgzLCJpYXQiOjE0OTkyMzIzNzN9.SIGNATURE"
	invalidSegments       = "HEADER.SIGNATURE"
)

func TestParseClaims(t *testing.T) {
	c, err := ParseClaims(token)
	assert.NoError(t, err)
	assert.Equal(t, c.ManagedID, "user")

	_, err = ParseClaims(invalidPayload)
	assert.Error(t, err)

	_, err = ParseClaims(invalidSegments)
	assert.Error(t, err)

	_, err = ParseClaims(invalidPayloadSegment)
	assert.Error(t, err)
}

func TestClaimsUID(t *testing.T) {
	claims := &Claims{Realm: "users", Sub: "service", ManagedID: "user"}
	s := claims.UID()
	assert.Equal(t, claims.ManagedID, s)

	claims.Realm = "services"
	s = claims.UID()
	assert.Equal(t, claims.Sub, s)
}
