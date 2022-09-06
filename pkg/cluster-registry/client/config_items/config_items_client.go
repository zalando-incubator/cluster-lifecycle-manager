// Code generated by go-swagger; DO NOT EDIT.

package config_items

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
)

// New creates a new config items API client.
func New(transport runtime.ClientTransport, formats strfmt.Registry) ClientService {
	return &Client{transport: transport, formats: formats}
}

/*
Client for config items API
*/
type Client struct {
	transport runtime.ClientTransport
	formats   strfmt.Registry
}

// ClientOption is the option for Client methods
type ClientOption func(*runtime.ClientOperation)

// ClientService is the interface for Client methods
type ClientService interface {
	AddOrUpdateConfigItem(params *AddOrUpdateConfigItemParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*AddOrUpdateConfigItemOK, error)

	DeleteConfigItem(params *DeleteConfigItemParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*DeleteConfigItemNoContent, error)

	SetTransport(transport runtime.ClientTransport)
}

/*
  AddOrUpdateConfigItem adds update config item

  Add/update a configuration item unique to the cluster.
*/
func (a *Client) AddOrUpdateConfigItem(params *AddOrUpdateConfigItemParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*AddOrUpdateConfigItemOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewAddOrUpdateConfigItemParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "addOrUpdateConfigItem",
		Method:             "PUT",
		PathPattern:        "/kubernetes-clusters/{cluster_id}/config-items/{config_key}",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &AddOrUpdateConfigItemReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	}
	for _, opt := range opts {
		opt(op)
	}

	result, err := a.transport.Submit(op)
	if err != nil {
		return nil, err
	}
	success, ok := result.(*AddOrUpdateConfigItemOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	// safeguard: normally, absent a default response, unknown success responses return an error above: so this is a codegen issue
	msg := fmt.Sprintf("unexpected success response for addOrUpdateConfigItem: API contract not enforced by server. Client expected to get an error, but got: %T", result)
	panic(msg)
}

/*
  DeleteConfigItem deletes config item

  Deletes config item.
*/
func (a *Client) DeleteConfigItem(params *DeleteConfigItemParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*DeleteConfigItemNoContent, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewDeleteConfigItemParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "deleteConfigItem",
		Method:             "DELETE",
		PathPattern:        "/kubernetes-clusters/{cluster_id}/config-items/{config_key}",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &DeleteConfigItemReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	}
	for _, opt := range opts {
		opt(op)
	}

	result, err := a.transport.Submit(op)
	if err != nil {
		return nil, err
	}
	success, ok := result.(*DeleteConfigItemNoContent)
	if ok {
		return success, nil
	}
	// unexpected success response
	// safeguard: normally, absent a default response, unknown success responses return an error above: so this is a codegen issue
	msg := fmt.Sprintf("unexpected success response for deleteConfigItem: API contract not enforced by server. Client expected to get an error, but got: %T", result)
	panic(msg)
}

// SetTransport changes the transport on the client
func (a *Client) SetTransport(transport runtime.ClientTransport) {
	a.transport = transport
}
