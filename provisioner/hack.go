package provisioner

import (
	"fmt"
	"net/url"
	"strings"
)

// splitStackName takes a stackName and returns the corresponding Senza stack
// and version values.
func splitStackName(stackName string) (string, string, error) {
	split := strings.LastIndex(stackName, "-")
	if split == -1 {
		return "", "", fmt.Errorf("unknown format for stackName '%s'", stackName)
	}
	return stackName[0:split], stackName[split+1:], nil
}

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
