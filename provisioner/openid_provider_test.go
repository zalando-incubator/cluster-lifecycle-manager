package provisioner

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const newProviderArn = "new-openid"

func TestStringArraySub(t *testing.T) {
	for _, tc := range []struct {
		a      []string
		b      []string
		result []string
		name   string
	}{
		{
			a:      []string{"a", "b"},
			b:      []string{"a"},
			result: []string{"b"},
			name:   "simple",
		},
		{
			a:      []string{"a", "b"},
			b:      []string{},
			result: []string{"a", "b"},
			name:   "empty second",
		},
		{
			a:      []string{},
			b:      []string{"a", "b"},
			result: []string{},
			name:   "empty first",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := stringArraySub(tc.a, tc.b)
			assert.ElementsMatch(t, result, tc.result)
		})
	}
}

func TestStringArrayContains(t *testing.T) {
	for _, tc := range []struct {
		name   string
		first  []string
		second []string
		result bool
	}{
		{
			name:   "simple",
			first:  []string{"a", "b"},
			second: []string{"a"},
			result: true,
		},
		{
			name:   "equal",
			first:  []string{"a", "b"},
			second: []string{"a", "b"},
			result: true,
		},
		{
			name:   "false",
			first:  []string{"a", "b"},
			second: []string{"a", "b", "c"},
			result: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.result, stringArrayContains(tc.first, tc.second))
		})
	}
}

func TestStringSetUnion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		a      []string
		b      []string
		result []string
	}{
		{
			name:   "simple",
			a:      []string{"b"},
			b:      []string{"a"},
			result: []string{"a", "b"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := stringSetUnion(tc.a, tc.b)
			assert.Equal(t, tc.result, result)
		})
	}
}

func TestOpenIDProviderProvisionerProvision(t *testing.T) {
	for _, tc := range []struct {
		name               string
		openidProviderHost string
		existingProvider   []testOpenidProvider
		finalProviders     []testOpenidProvider
	}{
		{
			name:             "provider is missing",
			existingProvider: []testOpenidProvider{},
			finalProviders: []testOpenidProvider{
				{
					arn:         newProviderArn,
					thumbprints: []string{acmRootCAThumbprint},
					clientIDs:   []string{stsDomain},
					url:         "https://test-1.example.org",
				},
			},
			openidProviderHost: "test-1.example.org",
		},
		{
			name: "provider configuration is up-to-date",
			existingProvider: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{acmRootCAThumbprint},
					clientIDs:   []string{stsDomain},
					url:         "test-openid.example.org",
				},
			},
			finalProviders: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{acmRootCAThumbprint},
					clientIDs:   []string{stsDomain},
					url:         "test-openid.example.org",
				},
			},
			openidProviderHost: "test-openid.example.org",
		},
		{
			name: "multiple providers with one correct configuration",
			existingProvider: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{acmRootCAThumbprint},
					clientIDs:   []string{stsDomain},
					url:         "test-openid.example.org",
				},
				{
					arn:         "second",
					thumbprints: []string{"some-thumbprint"},
					clientIDs:   []string{"some-client-id"},
					url:         "broken-openid.example.org",
				},
			},
			finalProviders: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{acmRootCAThumbprint},
					clientIDs:   []string{stsDomain},
					url:         "test-openid.example.org",
				},
				{
					arn:         "second",
					thumbprints: []string{"some-thumbprint"},
					clientIDs:   []string{"some-client-id"},
					url:         "broken-openid.example.org",
				},
			},
			openidProviderHost: "test-openid.example.org",
		},
		{
			name: "provider thumbprint is out of date",
			existingProvider: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{"oldthumbprint"},
					clientIDs:   []string{stsDomain},
					url:         "test-openid.example.org",
				},
			},
			finalProviders: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{acmRootCAThumbprint, "oldthumbprint"},
					clientIDs:   []string{stsDomain},
					url:         "test-openid.example.org",
				},
			},
			openidProviderHost: "test-openid.example.org",
		},
		{
			name: "provider client-id is out of date",
			existingProvider: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{acmRootCAThumbprint},
					clientIDs:   []string{"test.amazonaws.com"},
					url:         "test-openid.example.org",
				},
			},
			finalProviders: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{acmRootCAThumbprint},
					clientIDs:   []string{stsDomain, "test.amazonaws.com"},
					url:         "test-openid.example.org",
				},
			},
			openidProviderHost: "test-openid.example.org",
		},
		{
			name: "both provider client-id and thumbprint are out of date",
			existingProvider: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{"incorrect-thumbprint"},
					clientIDs:   []string{"test.amazonaws.com"},
					url:         "test-openid.example.org",
				},
			},
			finalProviders: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{acmRootCAThumbprint, "incorrect-thumbprint"},
					clientIDs:   []string{stsDomain, "test.amazonaws.com"},
					url:         "test-openid.example.org",
				},
			},
			openidProviderHost: "test-openid.example.org",
		},
		{
			name: "no change even when outdated thumbprint and client-id present",
			existingProvider: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{"incorrect-thumbprint", acmRootCAThumbprint},
					clientIDs:   []string{"test.amazonaws.com", stsDomain},
					url:         "test-openid.example.org",
				},
			},
			finalProviders: []testOpenidProvider{
				{
					arn:         "first",
					thumbprints: []string{"incorrect-thumbprint", acmRootCAThumbprint},
					clientIDs:   []string{"test.amazonaws.com", stsDomain},
					url:         "test-openid.example.org",
				},
			},
			openidProviderHost: "test-openid.example.org",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testClient := testIamClient{
				providers: tc.existingProvider,
			}
			adapter := &awsAdapter{iamClient: &testClient}
			provisioner, err := NewOpenIDProviderProvisioner(adapter, tc.openidProviderHost)
			require.NoError(t, err)
			err = provisioner.Provision()
			require.NoError(t, err)
			require.EqualValues(t, tc.finalProviders, testClient.providers)
		})
	}
}

type testIamClient struct {
	iamiface.IAMAPI
	t         *testing.T
	providers []testOpenidProvider
}
type testOpenidProvider struct {
	arn         string
	thumbprints []string
	clientIDs   []string
	url         string
}

func (testClient *testIamClient) ListOpenIDConnectProviders(input *iam.ListOpenIDConnectProvidersInput) (*iam.ListOpenIDConnectProvidersOutput, error) {
	output := make([]*iam.OpenIDConnectProviderListEntry, len(testClient.providers))
	for i, v := range testClient.providers {
		output[i] = &iam.OpenIDConnectProviderListEntry{
			Arn: aws.String(v.arn),
		}
	}
	return &iam.ListOpenIDConnectProvidersOutput{OpenIDConnectProviderList: output}, nil
}

func (testClient *testIamClient) GetOpenIDConnectProvider(input *iam.GetOpenIDConnectProviderInput) (*iam.GetOpenIDConnectProviderOutput, error) {
	arn := *input.OpenIDConnectProviderArn
	for _, p := range testClient.providers {
		if p.arn == arn {
			output := &iam.GetOpenIDConnectProviderOutput{
				ClientIDList:   aws.StringSlice(p.clientIDs),
				ThumbprintList: aws.StringSlice(p.thumbprints),
				Url:            aws.String(p.url),
			}
			return output, nil
		}
	}
	return nil, fmt.Errorf("openid provider with arn %s not found", arn)
}

func (testClient *testIamClient) CreateOpenIDConnectProvider(input *iam.CreateOpenIDConnectProviderInput) (*iam.CreateOpenIDConnectProviderOutput, error) {
	testClient.providers = append(testClient.providers, testOpenidProvider{
		arn:         newProviderArn,
		thumbprints: aws.StringValueSlice(input.ThumbprintList),
		clientIDs:   aws.StringValueSlice(input.ClientIDList),
		url:         *input.Url,
	})
	return &iam.CreateOpenIDConnectProviderOutput{OpenIDConnectProviderArn: aws.String(newProviderArn)}, nil
}

func (testClient *testIamClient) UpdateOpenIDConnectProviderThumbprint(input *iam.UpdateOpenIDConnectProviderThumbprintInput) (*iam.UpdateOpenIDConnectProviderThumbprintOutput, error) {
	newThumbprints := aws.StringValueSlice(input.ThumbprintList)
	var index int
	var found bool
	for i, p := range testClient.providers {
		if *input.OpenIDConnectProviderArn == p.arn {
			index = i
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("open id provider with arn %s not found", *input.OpenIDConnectProviderArn)
	}
	testClient.providers[index].thumbprints = newThumbprints
	return &iam.UpdateOpenIDConnectProviderThumbprintOutput{}, nil
}

func (testClient *testIamClient) AddClientIDToOpenIDConnectProvider(input *iam.AddClientIDToOpenIDConnectProviderInput) (*iam.AddClientIDToOpenIDConnectProviderOutput, error) {
	newClientId := *input.ClientID
	var found bool
	var index int
	for i, p := range testClient.providers {
		if p.arn == *input.OpenIDConnectProviderArn {
			index = i
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("open id provider with arn %s not found", *input.OpenIDConnectProviderArn)
	}
	existingClientIds := testClient.providers[index].clientIDs
	testClient.providers[index].clientIDs = stringSetUnion(existingClientIds, []string{newClientId})
	return &iam.AddClientIDToOpenIDConnectProviderOutput{}, nil
}
