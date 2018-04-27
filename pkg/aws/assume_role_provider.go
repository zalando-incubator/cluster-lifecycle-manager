package aws

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
)

// AssumeRoleProvider is an AWS SDK credentials provider which retrieve
// credentials by assuming the defined role.
type AssumeRoleProvider struct {
	role        string
	sessionName string
	creds       *sts.Credentials
	sts         *sts.STS
}

// NewAssumeRoleProvider initializes a new AssumeRoleProvider.
func NewAssumeRoleProvider(role, sessionName string, sess *session.Session) *AssumeRoleProvider {
	return &AssumeRoleProvider{
		role:        role,
		sessionName: sessionName,
		sts:         sts.New(sess),
	}
}

// Retrieve retrieves new credentials by assuming the defined role.
func (a *AssumeRoleProvider) Retrieve() (credentials.Value, error) {
	params := &sts.AssumeRoleInput{
		RoleArn:         aws.String(a.role),
		RoleSessionName: aws.String(a.sessionName),
	}

	resp, err := a.sts.AssumeRole(params)
	if err != nil {
		return credentials.Value{}, err
	}

	a.creds = resp.Credentials

	return credentials.Value{
		AccessKeyID:     *resp.Credentials.AccessKeyId,
		SecretAccessKey: *resp.Credentials.SecretAccessKey,
		SessionToken:    *resp.Credentials.SessionToken,
		ProviderName:    "assumeRoleProvider",
	}, nil
}

// IsExpired is true when the credentials has expired and must be retrieved
// again.
func (a *AssumeRoleProvider) IsExpired() bool {
	if a.creds != nil && a.creds.Expiration != nil {
		return time.Now().UTC().After(*a.creds.Expiration)
	}
	return true
}
