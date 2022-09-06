// Code generated by go-swagger; DO NOT EDIT.

package node_pools

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"net/http"
	"time"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	cr "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
)

// NewDeleteNodePoolParams creates a new DeleteNodePoolParams object,
// with the default timeout for this client.
//
// Default values are not hydrated, since defaults are normally applied by the API server side.
//
// To enforce default values in parameter, use SetDefaults or WithDefaults.
func NewDeleteNodePoolParams() *DeleteNodePoolParams {
	return &DeleteNodePoolParams{
		timeout: cr.DefaultTimeout,
	}
}

// NewDeleteNodePoolParamsWithTimeout creates a new DeleteNodePoolParams object
// with the ability to set a timeout on a request.
func NewDeleteNodePoolParamsWithTimeout(timeout time.Duration) *DeleteNodePoolParams {
	return &DeleteNodePoolParams{
		timeout: timeout,
	}
}

// NewDeleteNodePoolParamsWithContext creates a new DeleteNodePoolParams object
// with the ability to set a context for a request.
func NewDeleteNodePoolParamsWithContext(ctx context.Context) *DeleteNodePoolParams {
	return &DeleteNodePoolParams{
		Context: ctx,
	}
}

// NewDeleteNodePoolParamsWithHTTPClient creates a new DeleteNodePoolParams object
// with the ability to set a custom HTTPClient for a request.
func NewDeleteNodePoolParamsWithHTTPClient(client *http.Client) *DeleteNodePoolParams {
	return &DeleteNodePoolParams{
		HTTPClient: client,
	}
}

/* DeleteNodePoolParams contains all the parameters to send to the API endpoint
   for the delete node pool operation.

   Typically these are written to a http.Request.
*/
type DeleteNodePoolParams struct {

	/* ClusterID.

	   ID of the cluster.
	*/
	ClusterID string

	/* NodePoolName.

	   Name of the node pool.
	*/
	NodePoolName string

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithDefaults hydrates default values in the delete node pool params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *DeleteNodePoolParams) WithDefaults() *DeleteNodePoolParams {
	o.SetDefaults()
	return o
}

// SetDefaults hydrates default values in the delete node pool params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *DeleteNodePoolParams) SetDefaults() {
	// no default values defined for this parameter
}

// WithTimeout adds the timeout to the delete node pool params
func (o *DeleteNodePoolParams) WithTimeout(timeout time.Duration) *DeleteNodePoolParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the delete node pool params
func (o *DeleteNodePoolParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the delete node pool params
func (o *DeleteNodePoolParams) WithContext(ctx context.Context) *DeleteNodePoolParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the delete node pool params
func (o *DeleteNodePoolParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the delete node pool params
func (o *DeleteNodePoolParams) WithHTTPClient(client *http.Client) *DeleteNodePoolParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the delete node pool params
func (o *DeleteNodePoolParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithClusterID adds the clusterID to the delete node pool params
func (o *DeleteNodePoolParams) WithClusterID(clusterID string) *DeleteNodePoolParams {
	o.SetClusterID(clusterID)
	return o
}

// SetClusterID adds the clusterId to the delete node pool params
func (o *DeleteNodePoolParams) SetClusterID(clusterID string) {
	o.ClusterID = clusterID
}

// WithNodePoolName adds the nodePoolName to the delete node pool params
func (o *DeleteNodePoolParams) WithNodePoolName(nodePoolName string) *DeleteNodePoolParams {
	o.SetNodePoolName(nodePoolName)
	return o
}

// SetNodePoolName adds the nodePoolName to the delete node pool params
func (o *DeleteNodePoolParams) SetNodePoolName(nodePoolName string) {
	o.NodePoolName = nodePoolName
}

// WriteToRequest writes these params to a swagger request
func (o *DeleteNodePoolParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error

	// path param cluster_id
	if err := r.SetPathParam("cluster_id", o.ClusterID); err != nil {
		return err
	}

	// path param node_pool_name
	if err := r.SetPathParam("node_pool_name", o.NodePoolName); err != nil {
		return err
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
