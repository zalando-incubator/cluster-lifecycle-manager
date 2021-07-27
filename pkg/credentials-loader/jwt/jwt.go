package jwt

import (
	"encoding/json"
	"fmt"
	"strings"

	jwt "github.com/golang-jwt/jwt"
)

// Claims defines the claims of a jwt token.
type Claims struct {
	Sub             string `json:"sub"`
	Realm           string `json:"https://identity.zalando.com/realm"`
	Type            string `json:"https://identity.zalando.com/token"`
	ManagedID       string `json:"https://identity.zalando.com/managed-id"`
	Azp             string `json:"azp"`
	Businesspartnet string `json:"https://identity.zalando.com/bp"`
	AuthTime        int    `json:"auth_time"`
	ISS             string `json:"iss"`
	Exp             int    `json:"exp"`
	IAt             int    `json:"iat"`
}

// UID returns the claim mapping to the uid depending on type.
func (c *Claims) UID() string {
	switch c.Realm {
	case "users":
		return c.ManagedID
	default:
		return c.Sub
	}
}

// ParseClaims parses the claims of a jwt token.
func ParseClaims(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("token contains an invalid number of segments")
	}

	d, err := jwt.DecodeSegment(parts[1])
	if err != nil {
		return nil, err
	}

	var claims Claims
	err = json.Unmarshal(d, &claims)
	if err != nil {
		return nil, err
	}

	return &claims, nil
}
