package util

import (
	"fmt"
	"net/url"
	"strings"
)

// GetHostedZone gets the hosted zone from an APIServerURL.
func GetHostedZone(apiServerURL string) (string, error) {
	url, err := url.Parse(apiServerURL)
	if err != nil {
		return "", err
	}

	split := strings.Split(url.Host, ".")
	if len(split) < 2 {
		return "", fmt.Errorf("can't derive hosted zone from URL %s", apiServerURL)
	}

	return strings.Join(split[1:], "."), nil
}
