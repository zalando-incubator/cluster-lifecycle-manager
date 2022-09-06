// Code generated by go-swagger; DO NOT EDIT.

package node_pools

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
)

// New creates a new node pools API client.
func New(transport runtime.ClientTransport, formats strfmt.Registry) ClientService {
	return &Client{transport: transport, formats: formats}
}

/*
Client for node pools API
*/
type Client struct {
	transport runtime.ClientTransport
	formats   strfmt.Registry
}

// ClientOption is the option for Client methods
type ClientOption func(*runtime.ClientOperation)

// ClientService is the interface for Client methods
type ClientService interface {
	CreateOrUpdateNodePool(params *CreateOrUpdateNodePoolParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*CreateOrUpdateNodePoolOK, error)

	DeleteNodePool(params *DeleteNodePoolParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*DeleteNodePoolNoContent, error)

	ListNodePools(params *ListNodePoolsParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListNodePoolsOK, error)

	UpdateNodePool(params *UpdateNodePoolParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*UpdateNodePoolOK, error)

	SetTransport(transport runtime.ClientTransport)
}

/*
  CreateOrUpdateNodePool creates update node pool

  Create/update a node pool.
*/
func (a *Client) CreateOrUpdateNodePool(params *CreateOrUpdateNodePoolParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*CreateOrUpdateNodePoolOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewCreateOrUpdateNodePoolParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "createOrUpdateNodePool",
		Method:             "PUT",
		PathPattern:        "/kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &CreateOrUpdateNodePoolReader{formats: a.formats},
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
	success, ok := result.(*CreateOrUpdateNodePoolOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	// safeguard: normally, absent a default response, unknown success responses return an error above: so this is a codegen issue
	msg := fmt.Sprintf("unexpected success response for createOrUpdateNodePool: API contract not enforced by server. Client expected to get an error, but got: %T", result)
	panic(msg)
}

/*
  DeleteNodePool deletes node pool

  Deletes node pool.
*/
func (a *Client) DeleteNodePool(params *DeleteNodePoolParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*DeleteNodePoolNoContent, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewDeleteNodePoolParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "deleteNodePool",
		Method:             "DELETE",
		PathPattern:        "/kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &DeleteNodePoolReader{formats: a.formats},
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
	success, ok := result.(*DeleteNodePoolNoContent)
	if ok {
		return success, nil
	}
	// unexpected success response
	// safeguard: normally, absent a default response, unknown success responses return an error above: so this is a codegen issue
	msg := fmt.Sprintf("unexpected success response for deleteNodePool: API contract not enforced by server. Client expected to get an error, but got: %T", result)
	panic(msg)
}

/*
  ListNodePools lists node pools

  List all node pools of a cluster.
*/
func (a *Client) ListNodePools(params *ListNodePoolsParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListNodePoolsOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewListNodePoolsParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "listNodePools",
		Method:             "GET",
		PathPattern:        "/kubernetes-clusters/{cluster_id}/node-pools",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &ListNodePoolsReader{formats: a.formats},
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
	success, ok := result.(*ListNodePoolsOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	// safeguard: normally, absent a default response, unknown success responses return an error above: so this is a codegen issue
	msg := fmt.Sprintf("unexpected success response for listNodePools: API contract not enforced by server. Client expected to get an error, but got: %T", result)
	panic(msg)
}

/*
  UpdateNodePool updates node pool

  Update a node pool.
*/
func (a *Client) UpdateNodePool(params *UpdateNodePoolParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*UpdateNodePoolOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewUpdateNodePoolParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "updateNodePool",
		Method:             "PATCH",
		PathPattern:        "/kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &UpdateNodePoolReader{formats: a.formats},
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
	success, ok := result.(*UpdateNodePoolOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	// safeguard: normally, absent a default response, unknown success responses return an error above: so this is a codegen issue
	msg := fmt.Sprintf("unexpected success response for updateNodePool: API contract not enforced by server. Client expected to get an error, but got: %T", result)
	panic(msg)
}

// SetTransport changes the transport on the client
func (a *Client) SetTransport(transport runtime.ClientTransport) {
	a.transport = transport
}
