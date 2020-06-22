package spotio

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

const (
	spotIOBaseURL  = "https://api.spotinst.io"
	launchSpecPath = "/ocean/aws/k8s/launchSpec"
)

// OceanTag is a tag defined for an ocean cluster.
type OceanTag struct {
	Key   string `json:"tagKey"`
	Value string `json:"tagValue"`
}

// OceanLaunchSpec is the launcSpec defined for an ocean cluster.
type OceanLaunchSpec struct {
	ID       string     `json:"id"`
	OceanID  string     `json:"oceanId"`
	ImageID  string     `json:"imageId"`
	UserData string     `json:"userData"`
	Tags     []OceanTag `json:"tags"`
}

// API is an interface describing the Spot.io HTTP API.
type API interface {
	ListLaunchSpecs(oceanID string) ([]OceanLaunchSpec, error)
}

// Client is the implementation of a spot.io HTTP client.
type Client struct {
	baseURL   string
	accountID string
	client    *http.Client
}

// NewClient initializes a new Spot.io HTTP Client.
func NewClient(accountID string, tokenSource oauth2.TokenSource) *Client {
	return &Client{
		accountID: accountID,
		client:    newOauth2HTTPClient(tokenSource),
		baseURL:   spotIOBaseURL,
	}
}

// ListLaunchSpecs lists the launch specs for the specified oceanID.
func (c *Client) ListLaunchSpecs(oceanID string) ([]OceanLaunchSpec, error) {
	u, err := url.Parse(c.baseURL + launchSpecPath)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("accountId", c.accountID)
	q.Set("oceanId", oceanID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	d, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		// TODO: handle error
		return nil, fmt.Errorf("spot.io client error: %#v", string(d))
	}

	var response struct {
		Response struct {
			Items []OceanLaunchSpec `json:"items"`
		} `json:"response"`
	}

	err = json.Unmarshal(d, &response)
	if err != nil {
		return nil, err
	}

	return response.Response.Items, nil
}

// newOauth2HTTPClient creates an HTTP client with automatic oauth2 token
// injection. Additionally it will spawn a go-routine for closing idle
// connections every 3 second on the http.Transport. This solves the problem of
// re-resolving DNS when the endpoint backend changes.
// https://github.com/golang/go/issues/23427
func newOauth2HTTPClient(tokenSource oauth2.TokenSource) *http.Client {
	transport := &http.Transport{}
	go func(transport *http.Transport) {
		for {
			time.Sleep(3 * time.Second)
			transport.CloseIdleConnections()
		}
	}(transport)

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	ctx := context.Background()
	// add HTTP client to context (this is how the oauth2 lib gets it).
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)

	// instantiate an http.Client containg the token source.
	return oauth2.NewClient(ctx, tokenSource)
}
