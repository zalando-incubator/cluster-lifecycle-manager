package provisioner

import (
	"fmt"
	"net/url"
	"strings"
)

// parseWebhookID parses the webhookID from a clusterID.
// This is a hack for the special case of clusterID with localID
// 'kube-aws-test'.
func parseWebhookID(clusterID string) (string, error) {
	account, region, localID, err := parseClusterID(clusterID)
	if err != nil {
		return "", err
	}

	name, _, err := splitStackName(localID)
	if err != nil {
		return "", err
	}

	if name == "kube-aws-test" {
		return fmt.Sprintf("%s:%s:%s", account, region, name), nil
	}

	return clusterID, nil
}

// parseClusterID parses a clusterID into account, region and localID.
func parseClusterID(clusterID string) (string, string, string, error) {
	split := strings.Split(clusterID, ":")
	if len(split) != 4 {
		return "", "", "", fmt.Errorf("invalid clusterID %s", clusterID)
	}
	return split[0] + ":" + split[1], split[2], split[3], nil
}

// splitStackName takes a stackName and returns the corresponding Senza stack
// and version values.
func splitStackName(stackName string) (string, string, error) {
	split := strings.LastIndex(stackName, "-")
	if split == -1 {
		return "", "", fmt.Errorf("unknown format for stackName '%s'", stackName)
	}
	return stackName[0:split], stackName[split+1 : len(stackName)], nil
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
