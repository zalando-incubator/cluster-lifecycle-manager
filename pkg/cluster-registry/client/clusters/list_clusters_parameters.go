// Code generated by go-swagger; DO NOT EDIT.

package clusters

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
	"github.com/go-openapi/swag"
)

// NewListClustersParams creates a new ListClustersParams object,
// with the default timeout for this client.
//
// Default values are not hydrated, since defaults are normally applied by the API server side.
//
// To enforce default values in parameter, use SetDefaults or WithDefaults.
func NewListClustersParams() *ListClustersParams {
	return &ListClustersParams{
		timeout: cr.DefaultTimeout,
	}
}

// NewListClustersParamsWithTimeout creates a new ListClustersParams object
// with the ability to set a timeout on a request.
func NewListClustersParamsWithTimeout(timeout time.Duration) *ListClustersParams {
	return &ListClustersParams{
		timeout: timeout,
	}
}

// NewListClustersParamsWithContext creates a new ListClustersParams object
// with the ability to set a context for a request.
func NewListClustersParamsWithContext(ctx context.Context) *ListClustersParams {
	return &ListClustersParams{
		Context: ctx,
	}
}

// NewListClustersParamsWithHTTPClient creates a new ListClustersParams object
// with the ability to set a custom HTTPClient for a request.
func NewListClustersParamsWithHTTPClient(client *http.Client) *ListClustersParams {
	return &ListClustersParams{
		HTTPClient: client,
	}
}

/*
ListClustersParams contains all the parameters to send to the API endpoint

	for the list clusters operation.

	Typically these are written to a http.Request.
*/
type ListClustersParams struct {

	/* Alias.

	   Filter on cluster alias.
	*/
	Alias *string

	/* APIServerURL.

	   Filter on API server URL.
	*/
	APIServerURL *string

	/* Channel.

	   Filter on channel.
	*/
	Channel *string

	/* CostCenter.

	   Filter on cost center number.
	*/
	CostCenter *string

	/* CriticalityLevel.

	   Filter on criticality level.

	   Format: int32
	*/
	CriticalityLevel *int32

	/* Environment.

	   Filter on environment.
	*/
	Environment *string

	/* InfrastructureAccount.

	   Filter on infrastructure account.
	*/
	InfrastructureAccount *string

	/* LifecycleStatus.

	   Filter on cluster lifecycle status.
	*/
	LifecycleStatus *string

	/* LocalID.

	   Filter on local id.
	*/
	LocalID *string

	/* Provider.

	   Filter on provider.
	*/
	Provider *string

	/* Region.

	   Filter on region.
	*/
	Region *string

	/* Verbose.

	   Include technical data (config items, node pools) in the response, true by default

	   Default: true
	*/
	Verbose *bool

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithDefaults hydrates default values in the list clusters params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *ListClustersParams) WithDefaults() *ListClustersParams {
	o.SetDefaults()
	return o
}

// SetDefaults hydrates default values in the list clusters params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *ListClustersParams) SetDefaults() {
	var (
		verboseDefault = bool(true)
	)

	val := ListClustersParams{
		Verbose: &verboseDefault,
	}

	val.timeout = o.timeout
	val.Context = o.Context
	val.HTTPClient = o.HTTPClient
	*o = val
}

// WithTimeout adds the timeout to the list clusters params
func (o *ListClustersParams) WithTimeout(timeout time.Duration) *ListClustersParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the list clusters params
func (o *ListClustersParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the list clusters params
func (o *ListClustersParams) WithContext(ctx context.Context) *ListClustersParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the list clusters params
func (o *ListClustersParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the list clusters params
func (o *ListClustersParams) WithHTTPClient(client *http.Client) *ListClustersParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the list clusters params
func (o *ListClustersParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithAlias adds the alias to the list clusters params
func (o *ListClustersParams) WithAlias(alias *string) *ListClustersParams {
	o.SetAlias(alias)
	return o
}

// SetAlias adds the alias to the list clusters params
func (o *ListClustersParams) SetAlias(alias *string) {
	o.Alias = alias
}

// WithAPIServerURL adds the aPIServerURL to the list clusters params
func (o *ListClustersParams) WithAPIServerURL(aPIServerURL *string) *ListClustersParams {
	o.SetAPIServerURL(aPIServerURL)
	return o
}

// SetAPIServerURL adds the apiServerUrl to the list clusters params
func (o *ListClustersParams) SetAPIServerURL(aPIServerURL *string) {
	o.APIServerURL = aPIServerURL
}

// WithChannel adds the channel to the list clusters params
func (o *ListClustersParams) WithChannel(channel *string) *ListClustersParams {
	o.SetChannel(channel)
	return o
}

// SetChannel adds the channel to the list clusters params
func (o *ListClustersParams) SetChannel(channel *string) {
	o.Channel = channel
}

// WithCostCenter adds the costCenter to the list clusters params
func (o *ListClustersParams) WithCostCenter(costCenter *string) *ListClustersParams {
	o.SetCostCenter(costCenter)
	return o
}

// SetCostCenter adds the costCenter to the list clusters params
func (o *ListClustersParams) SetCostCenter(costCenter *string) {
	o.CostCenter = costCenter
}

// WithCriticalityLevel adds the criticalityLevel to the list clusters params
func (o *ListClustersParams) WithCriticalityLevel(criticalityLevel *int32) *ListClustersParams {
	o.SetCriticalityLevel(criticalityLevel)
	return o
}

// SetCriticalityLevel adds the criticalityLevel to the list clusters params
func (o *ListClustersParams) SetCriticalityLevel(criticalityLevel *int32) {
	o.CriticalityLevel = criticalityLevel
}

// WithEnvironment adds the environment to the list clusters params
func (o *ListClustersParams) WithEnvironment(environment *string) *ListClustersParams {
	o.SetEnvironment(environment)
	return o
}

// SetEnvironment adds the environment to the list clusters params
func (o *ListClustersParams) SetEnvironment(environment *string) {
	o.Environment = environment
}

// WithInfrastructureAccount adds the infrastructureAccount to the list clusters params
func (o *ListClustersParams) WithInfrastructureAccount(infrastructureAccount *string) *ListClustersParams {
	o.SetInfrastructureAccount(infrastructureAccount)
	return o
}

// SetInfrastructureAccount adds the infrastructureAccount to the list clusters params
func (o *ListClustersParams) SetInfrastructureAccount(infrastructureAccount *string) {
	o.InfrastructureAccount = infrastructureAccount
}

// WithLifecycleStatus adds the lifecycleStatus to the list clusters params
func (o *ListClustersParams) WithLifecycleStatus(lifecycleStatus *string) *ListClustersParams {
	o.SetLifecycleStatus(lifecycleStatus)
	return o
}

// SetLifecycleStatus adds the lifecycleStatus to the list clusters params
func (o *ListClustersParams) SetLifecycleStatus(lifecycleStatus *string) {
	o.LifecycleStatus = lifecycleStatus
}

// WithLocalID adds the localID to the list clusters params
func (o *ListClustersParams) WithLocalID(localID *string) *ListClustersParams {
	o.SetLocalID(localID)
	return o
}

// SetLocalID adds the localId to the list clusters params
func (o *ListClustersParams) SetLocalID(localID *string) {
	o.LocalID = localID
}

// WithProvider adds the provider to the list clusters params
func (o *ListClustersParams) WithProvider(provider *string) *ListClustersParams {
	o.SetProvider(provider)
	return o
}

// SetProvider adds the provider to the list clusters params
func (o *ListClustersParams) SetProvider(provider *string) {
	o.Provider = provider
}

// WithRegion adds the region to the list clusters params
func (o *ListClustersParams) WithRegion(region *string) *ListClustersParams {
	o.SetRegion(region)
	return o
}

// SetRegion adds the region to the list clusters params
func (o *ListClustersParams) SetRegion(region *string) {
	o.Region = region
}

// WithVerbose adds the verbose to the list clusters params
func (o *ListClustersParams) WithVerbose(verbose *bool) *ListClustersParams {
	o.SetVerbose(verbose)
	return o
}

// SetVerbose adds the verbose to the list clusters params
func (o *ListClustersParams) SetVerbose(verbose *bool) {
	o.Verbose = verbose
}

// WriteToRequest writes these params to a swagger request
func (o *ListClustersParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error

	if o.Alias != nil {

		// query param alias
		var qrAlias string

		if o.Alias != nil {
			qrAlias = *o.Alias
		}
		qAlias := qrAlias
		if qAlias != "" {

			if err := r.SetQueryParam("alias", qAlias); err != nil {
				return err
			}
		}
	}

	if o.APIServerURL != nil {

		// query param api_server_url
		var qrAPIServerURL string

		if o.APIServerURL != nil {
			qrAPIServerURL = *o.APIServerURL
		}
		qAPIServerURL := qrAPIServerURL
		if qAPIServerURL != "" {

			if err := r.SetQueryParam("api_server_url", qAPIServerURL); err != nil {
				return err
			}
		}
	}

	if o.Channel != nil {

		// query param channel
		var qrChannel string

		if o.Channel != nil {
			qrChannel = *o.Channel
		}
		qChannel := qrChannel
		if qChannel != "" {

			if err := r.SetQueryParam("channel", qChannel); err != nil {
				return err
			}
		}
	}

	if o.CostCenter != nil {

		// query param cost_center
		var qrCostCenter string

		if o.CostCenter != nil {
			qrCostCenter = *o.CostCenter
		}
		qCostCenter := qrCostCenter
		if qCostCenter != "" {

			if err := r.SetQueryParam("cost_center", qCostCenter); err != nil {
				return err
			}
		}
	}

	if o.CriticalityLevel != nil {

		// query param criticality_level
		var qrCriticalityLevel int32

		if o.CriticalityLevel != nil {
			qrCriticalityLevel = *o.CriticalityLevel
		}
		qCriticalityLevel := swag.FormatInt32(qrCriticalityLevel)
		if qCriticalityLevel != "" {

			if err := r.SetQueryParam("criticality_level", qCriticalityLevel); err != nil {
				return err
			}
		}
	}

	if o.Environment != nil {

		// query param environment
		var qrEnvironment string

		if o.Environment != nil {
			qrEnvironment = *o.Environment
		}
		qEnvironment := qrEnvironment
		if qEnvironment != "" {

			if err := r.SetQueryParam("environment", qEnvironment); err != nil {
				return err
			}
		}
	}

	if o.InfrastructureAccount != nil {

		// query param infrastructure_account
		var qrInfrastructureAccount string

		if o.InfrastructureAccount != nil {
			qrInfrastructureAccount = *o.InfrastructureAccount
		}
		qInfrastructureAccount := qrInfrastructureAccount
		if qInfrastructureAccount != "" {

			if err := r.SetQueryParam("infrastructure_account", qInfrastructureAccount); err != nil {
				return err
			}
		}
	}

	if o.LifecycleStatus != nil {

		// query param lifecycle_status
		var qrLifecycleStatus string

		if o.LifecycleStatus != nil {
			qrLifecycleStatus = *o.LifecycleStatus
		}
		qLifecycleStatus := qrLifecycleStatus
		if qLifecycleStatus != "" {

			if err := r.SetQueryParam("lifecycle_status", qLifecycleStatus); err != nil {
				return err
			}
		}
	}

	if o.LocalID != nil {

		// query param local_id
		var qrLocalID string

		if o.LocalID != nil {
			qrLocalID = *o.LocalID
		}
		qLocalID := qrLocalID
		if qLocalID != "" {

			if err := r.SetQueryParam("local_id", qLocalID); err != nil {
				return err
			}
		}
	}

	if o.Provider != nil {

		// query param provider
		var qrProvider string

		if o.Provider != nil {
			qrProvider = *o.Provider
		}
		qProvider := qrProvider
		if qProvider != "" {

			if err := r.SetQueryParam("provider", qProvider); err != nil {
				return err
			}
		}
	}

	if o.Region != nil {

		// query param region
		var qrRegion string

		if o.Region != nil {
			qrRegion = *o.Region
		}
		qRegion := qrRegion
		if qRegion != "" {

			if err := r.SetQueryParam("region", qRegion); err != nil {
				return err
			}
		}
	}

	if o.Verbose != nil {

		// query param verbose
		var qrVerbose bool

		if o.Verbose != nil {
			qrVerbose = *o.Verbose
		}
		qVerbose := swag.FormatBool(qrVerbose)
		if qVerbose != "" {

			if err := r.SetQueryParam("verbose", qVerbose); err != nil {
				return err
			}
		}
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
