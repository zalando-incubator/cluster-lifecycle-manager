package provisioner

import (
	"fmt"
	"net/url"
	"strings"
)

// getHostedZone gets derrive hosted zone from an APIServerURL.
func getHostedZone(APIServerURL string) (string, error) {
	url, err := url.Parse(APIServerURL)
	if err != nil {
		return "", err
	}

	split := strings.Split(url.Host, ".")
	if len(split) < 2 {
		return "", fmt.Errorf("can't derive hosted zone from URL %s", APIServerURL)
	}

	return strings.Join(split[1:], "."), nil
}
