package eks

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"golang.org/x/oauth2"
	awsiamtoken "sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

func NewTokenSource(cfg aws.Config, clusterName string) *TokenSource {
	return &TokenSource{
		config:      cfg,
		clusterName: clusterName,
	}
}

type TokenSource struct {
	config      aws.Config
	clusterName string
}

func (ts *TokenSource) Token() (*oauth2.Token, error) {
	// TODO: cached?
	gen, err := awsiamtoken.NewGenerator(true, false)
	if err != nil {
		return nil, err
	}

	stsAPI := sts.NewFromConfig(ts.config)

	awsToken, err := gen.GetWithSTS(ts.clusterName, stsAPI)
	if err != nil {
		return nil, err
	}
	return &oauth2.Token{
		AccessToken: awsToken.Token,
		Expiry:      awsToken.Expiration,
	}, nil
}
