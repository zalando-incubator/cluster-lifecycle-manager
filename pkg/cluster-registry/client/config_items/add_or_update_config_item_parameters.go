// Code generated by go-swagger; DO NOT EDIT.

package config_items

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

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
)

// NewAddOrUpdateConfigItemParams creates a new AddOrUpdateConfigItemParams object,
// with the default timeout for this client.
//
// Default values are not hydrated, since defaults are normally applied by the API server side.
//
// To enforce default values in parameter, use SetDefaults or WithDefaults.
func NewAddOrUpdateConfigItemParams() *AddOrUpdateConfigItemParams {
	return &AddOrUpdateConfigItemParams{
		timeout: cr.DefaultTimeout,
	}
}

// NewAddOrUpdateConfigItemParamsWithTimeout creates a new AddOrUpdateConfigItemParams object
// with the ability to set a timeout on a request.
func NewAddOrUpdateConfigItemParamsWithTimeout(timeout time.Duration) *AddOrUpdateConfigItemParams {
	return &AddOrUpdateConfigItemParams{
		timeout: timeout,
	}
}

// NewAddOrUpdateConfigItemParamsWithContext creates a new AddOrUpdateConfigItemParams object
// with the ability to set a context for a request.
func NewAddOrUpdateConfigItemParamsWithContext(ctx context.Context) *AddOrUpdateConfigItemParams {
	return &AddOrUpdateConfigItemParams{
		Context: ctx,
	}
}

// NewAddOrUpdateConfigItemParamsWithHTTPClient creates a new AddOrUpdateConfigItemParams object
// with the ability to set a custom HTTPClient for a request.
func NewAddOrUpdateConfigItemParamsWithHTTPClient(client *http.Client) *AddOrUpdateConfigItemParams {
	return &AddOrUpdateConfigItemParams{
		HTTPClient: client,
	}
}

/* AddOrUpdateConfigItemParams contains all the parameters to send to the API endpoint
   for the add or update config item operation.

   Typically these are written to a http.Request.
*/
type AddOrUpdateConfigItemParams struct {

	/* ClusterID.

	   ID of the cluster.
	*/
	ClusterID string

	/* ConfigKey.

	   Key for the config value.
	*/
	ConfigKey string

	/* Value.

	   Config value.
	*/
	Value *models.ConfigValue

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithDefaults hydrates default values in the add or update config item params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *AddOrUpdateConfigItemParams) WithDefaults() *AddOrUpdateConfigItemParams {
	o.SetDefaults()
	return o
}

// SetDefaults hydrates default values in the add or update config item params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *AddOrUpdateConfigItemParams) SetDefaults() {
	// no default values defined for this parameter
}

// WithTimeout adds the timeout to the add or update config item params
func (o *AddOrUpdateConfigItemParams) WithTimeout(timeout time.Duration) *AddOrUpdateConfigItemParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the add or update config item params
func (o *AddOrUpdateConfigItemParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the add or update config item params
func (o *AddOrUpdateConfigItemParams) WithContext(ctx context.Context) *AddOrUpdateConfigItemParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the add or update config item params
func (o *AddOrUpdateConfigItemParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the add or update config item params
func (o *AddOrUpdateConfigItemParams) WithHTTPClient(client *http.Client) *AddOrUpdateConfigItemParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the add or update config item params
func (o *AddOrUpdateConfigItemParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithClusterID adds the clusterID to the add or update config item params
func (o *AddOrUpdateConfigItemParams) WithClusterID(clusterID string) *AddOrUpdateConfigItemParams {
	o.SetClusterID(clusterID)
	return o
}

// SetClusterID adds the clusterId to the add or update config item params
func (o *AddOrUpdateConfigItemParams) SetClusterID(clusterID string) {
	o.ClusterID = clusterID
}

// WithConfigKey adds the configKey to the add or update config item params
func (o *AddOrUpdateConfigItemParams) WithConfigKey(configKey string) *AddOrUpdateConfigItemParams {
	o.SetConfigKey(configKey)
	return o
}

// SetConfigKey adds the configKey to the add or update config item params
func (o *AddOrUpdateConfigItemParams) SetConfigKey(configKey string) {
	o.ConfigKey = configKey
}

// WithValue adds the value to the add or update config item params
func (o *AddOrUpdateConfigItemParams) WithValue(value *models.ConfigValue) *AddOrUpdateConfigItemParams {
	o.SetValue(value)
	return o
}

// SetValue adds the value to the add or update config item params
func (o *AddOrUpdateConfigItemParams) SetValue(value *models.ConfigValue) {
	o.Value = value
}

// WriteToRequest writes these params to a swagger request
func (o *AddOrUpdateConfigItemParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error

	// path param cluster_id
	if err := r.SetPathParam("cluster_id", o.ClusterID); err != nil {
		return err
	}

	// path param config_key
	if err := r.SetPathParam("config_key", o.ConfigKey); err != nil {
		return err
	}
	if o.Value != nil {
		if err := r.SetBodyParam(o.Value); err != nil {
			return err
		}
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
