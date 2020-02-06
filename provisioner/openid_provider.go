package provisioner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	log "github.com/sirupsen/logrus"
)

const (
	stsDomain = "sts.amazonaws.com"
	// acmRootCAThumbprint is the SHA-1 sum of the root certificate in the trust chain for the certificate use to serve
	// the open id discovery document.
	acmRootCAThumbprint = "9e99a48a9960b14926bb7f3b02e22da2b0ab7280"
)

type OpenIDProviderProvisioner struct {
	awsAdapter  *awsAdapter
	providerUrl string
	clientIDs   []string
	thumbprints []string
}

func NewOpenIDProviderProvisioner(adapter *awsAdapter, providerHostname string) (*OpenIDProviderProvisioner, error) {
	return &OpenIDProviderProvisioner{
		awsAdapter:  adapter,
		providerUrl: providerHostname,
		clientIDs:   []string{stsDomain},
		thumbprints: []string{acmRootCAThumbprint},
	}, nil
}

func (p *OpenIDProviderProvisioner) Provision() error {
	providers, err := p.awsAdapter.iamClient.ListOpenIDConnectProviders(&iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return fmt.Errorf("failed to list openid providers: %v", err)
	}
	var existingProvider *iam.GetOpenIDConnectProviderOutput
	var existingProviderArn string
	for _, prv := range providers.OpenIDConnectProviderList {
		provider, err := p.awsAdapter.iamClient.GetOpenIDConnectProvider(&iam.GetOpenIDConnectProviderInput{OpenIDConnectProviderArn: prv.Arn})
		if err != nil {
			return fmt.Errorf("failed to get openid provider: %v", err)
		}
		if *provider.Url == p.providerUrl {
			existingProvider = provider
			existingProviderArn = *prv.Arn
		}
	}

	if existingProvider != nil {
		if !stringArrayContains(aws.StringValueSlice(existingProvider.ThumbprintList), p.thumbprints) {
			_, err := p.awsAdapter.iamClient.UpdateOpenIDConnectProviderThumbprint(&iam.UpdateOpenIDConnectProviderThumbprintInput{
				OpenIDConnectProviderArn: aws.String(existingProviderArn),
				ThumbprintList:           aws.StringSlice(stringSetUnion(aws.StringValueSlice(existingProvider.ThumbprintList), p.thumbprints)),
			})
			if err != nil {
				return fmt.Errorf("failed to update the thumprints for openid provider %s: %v", p.providerUrl, err)
			}
		}
		if !stringArrayContains(aws.StringValueSlice(existingProvider.ClientIDList), p.clientIDs) {
			for _, clientID := range stringArraySub(p.clientIDs, aws.StringValueSlice(existingProvider.ClientIDList)) {
				_, err := p.awsAdapter.iamClient.AddClientIDToOpenIDConnectProvider(&iam.AddClientIDToOpenIDConnectProviderInput{
					ClientID:                 &clientID,
					OpenIDConnectProviderArn: aws.String(existingProviderArn),
				})
				if err != nil {
					return fmt.Errorf("failed to add client id to provider: %s: %v", p.providerUrl, err)
				}
			}
		}
		return nil
	}

	input := iam.CreateOpenIDConnectProviderInput{
		ClientIDList:   aws.StringSlice(p.clientIDs),
		ThumbprintList: aws.StringSlice(p.thumbprints),
		Url:            aws.String(fmt.Sprintf("https://%s", p.providerUrl)),
	}
	_, err = p.awsAdapter.iamClient.CreateOpenIDConnectProvider(&input)
	if err != nil {
		return fmt.Errorf("failed to create open id provider: %v", err)
	}
	return nil
}

func makeStringMap(input []string) map[string]interface{} {
	inputMap := make(map[string]interface{}, len(input))
	for _, v := range input {
		inputMap[v] = nil
	}
	return inputMap
}

func stringSetUnion(first []string, second []string) []string {
	merged := makeStringMap(first)
	for _, s := range second {
		merged[s] = nil
	}
	result := make([]string, len(merged))
	i := 0
	for k := range merged {
		result[i] = k
		i++
	}
	sort.Strings(result)
	return result
}

func stringArraySub(first []string, second []string) []string {
	secondMap := makeStringMap(second)
	var missing []string
	for _, v := range first {
		if _, ok := secondMap[v]; !ok {
			missing = append(missing, v)
		}
	}
	return missing
}

func stringArrayContains(first []string, second []string) bool {
	sub := stringArraySub(second, first)
	if len(sub) == 0 {
		return true
	}
	return false
}

func (p *OpenIDProviderProvisioner) Delete() error {
	providers, err := p.awsAdapter.iamClient.ListOpenIDConnectProviders(&iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return fmt.Errorf("failed to list openid providers: %v", err)
	}

	var deletionCandidates []string

	for _, prv := range providers.OpenIDConnectProviderList {
		provider, err := p.awsAdapter.iamClient.GetOpenIDConnectProvider(&iam.GetOpenIDConnectProviderInput{OpenIDConnectProviderArn: prv.Arn})
		if err != nil {
			return fmt.Errorf("failed to get openid connect provider with arn %s: %v", *prv.Arn, err)
		}
		log.Debugf("found provider: %s", *provider.Url)
		if *provider.Url == p.providerUrl {
			deletionCandidates = append(deletionCandidates, *prv.Arn)
		}
	}
	log.Infof("following openid providers will be deleted: %s", strings.Join(deletionCandidates, ","))
	for _, dc := range deletionCandidates {
		_, err := p.awsAdapter.iamClient.DeleteOpenIDConnectProvider(&iam.DeleteOpenIDConnectProviderInput{OpenIDConnectProviderArn: &dc})
		if err != nil {
			return fmt.Errorf("failed to delete openid connect provider with arn %s: %v", dc, err)
		}
	}
	return nil
}
