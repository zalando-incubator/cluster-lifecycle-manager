package eks

import (
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
	"golang.org/x/oauth2"
	awsiamtoken "sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

func NewTokenSource(sess *session.Session, clusterName string) *TokenSource {
	return &TokenSource{
		session:     sess,
		clusterName: clusterName,
	}
}

type TokenSource struct {
	session     *session.Session
	clusterName string
}

func (ts *TokenSource) Token() (*oauth2.Token, error) {
	// TODO: cached?
	gen, err := awsiamtoken.NewGenerator(true, false)
	if err != nil {
		return nil, err
	}

	stsAPI := sts.New(ts.session)

	awsToken, err := gen.GetWithSTS(ts.clusterName, stsAPI)
	if err != nil {
		return nil, err
	}
	return &oauth2.Token{
		AccessToken: awsToken.Token,
		Expiry:      awsToken.Expiration,
	}, nil
}
