// Code generated by go-swagger; DO NOT EDIT.

package config_items

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
)

// AddOrUpdateConfigItemReader is a Reader for the AddOrUpdateConfigItem structure.
type AddOrUpdateConfigItemReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *AddOrUpdateConfigItemReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewAddOrUpdateConfigItemOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 400:
		result := NewAddOrUpdateConfigItemBadRequest()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 401:
		result := NewAddOrUpdateConfigItemUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 403:
		result := NewAddOrUpdateConfigItemForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewAddOrUpdateConfigItemInternalServerError()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		return nil, runtime.NewAPIError("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}] addOrUpdateConfigItem", response, response.Code())
	}
}

// NewAddOrUpdateConfigItemOK creates a AddOrUpdateConfigItemOK with default headers values
func NewAddOrUpdateConfigItemOK() *AddOrUpdateConfigItemOK {
	return &AddOrUpdateConfigItemOK{}
}

/*
AddOrUpdateConfigItemOK describes a response with status code 200, with default header values.

The config items add/update request is accepted.
*/
type AddOrUpdateConfigItemOK struct {
	Payload *models.ConfigValue
}

// IsSuccess returns true when this add or update config item o k response has a 2xx status code
func (o *AddOrUpdateConfigItemOK) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this add or update config item o k response has a 3xx status code
func (o *AddOrUpdateConfigItemOK) IsRedirect() bool {
	return false
}

// IsClientError returns true when this add or update config item o k response has a 4xx status code
func (o *AddOrUpdateConfigItemOK) IsClientError() bool {
	return false
}

// IsServerError returns true when this add or update config item o k response has a 5xx status code
func (o *AddOrUpdateConfigItemOK) IsServerError() bool {
	return false
}

// IsCode returns true when this add or update config item o k response a status code equal to that given
func (o *AddOrUpdateConfigItemOK) IsCode(code int) bool {
	return code == 200
}

// Code gets the status code for the add or update config item o k response
func (o *AddOrUpdateConfigItemOK) Code() int {
	return 200
}

func (o *AddOrUpdateConfigItemOK) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemOK %s", 200, payload)
}

func (o *AddOrUpdateConfigItemOK) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemOK %s", 200, payload)
}

func (o *AddOrUpdateConfigItemOK) GetPayload() *models.ConfigValue {
	return o.Payload
}

func (o *AddOrUpdateConfigItemOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.ConfigValue)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewAddOrUpdateConfigItemBadRequest creates a AddOrUpdateConfigItemBadRequest with default headers values
func NewAddOrUpdateConfigItemBadRequest() *AddOrUpdateConfigItemBadRequest {
	return &AddOrUpdateConfigItemBadRequest{}
}

/*
AddOrUpdateConfigItemBadRequest describes a response with status code 400, with default header values.

Invalid request
*/
type AddOrUpdateConfigItemBadRequest struct {
	Payload *models.Error
}

// IsSuccess returns true when this add or update config item bad request response has a 2xx status code
func (o *AddOrUpdateConfigItemBadRequest) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this add or update config item bad request response has a 3xx status code
func (o *AddOrUpdateConfigItemBadRequest) IsRedirect() bool {
	return false
}

// IsClientError returns true when this add or update config item bad request response has a 4xx status code
func (o *AddOrUpdateConfigItemBadRequest) IsClientError() bool {
	return true
}

// IsServerError returns true when this add or update config item bad request response has a 5xx status code
func (o *AddOrUpdateConfigItemBadRequest) IsServerError() bool {
	return false
}

// IsCode returns true when this add or update config item bad request response a status code equal to that given
func (o *AddOrUpdateConfigItemBadRequest) IsCode(code int) bool {
	return code == 400
}

// Code gets the status code for the add or update config item bad request response
func (o *AddOrUpdateConfigItemBadRequest) Code() int {
	return 400
}

func (o *AddOrUpdateConfigItemBadRequest) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemBadRequest %s", 400, payload)
}

func (o *AddOrUpdateConfigItemBadRequest) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemBadRequest %s", 400, payload)
}

func (o *AddOrUpdateConfigItemBadRequest) GetPayload() *models.Error {
	return o.Payload
}

func (o *AddOrUpdateConfigItemBadRequest) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewAddOrUpdateConfigItemUnauthorized creates a AddOrUpdateConfigItemUnauthorized with default headers values
func NewAddOrUpdateConfigItemUnauthorized() *AddOrUpdateConfigItemUnauthorized {
	return &AddOrUpdateConfigItemUnauthorized{}
}

/*
AddOrUpdateConfigItemUnauthorized describes a response with status code 401, with default header values.

Unauthorized
*/
type AddOrUpdateConfigItemUnauthorized struct {
}

// IsSuccess returns true when this add or update config item unauthorized response has a 2xx status code
func (o *AddOrUpdateConfigItemUnauthorized) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this add or update config item unauthorized response has a 3xx status code
func (o *AddOrUpdateConfigItemUnauthorized) IsRedirect() bool {
	return false
}

// IsClientError returns true when this add or update config item unauthorized response has a 4xx status code
func (o *AddOrUpdateConfigItemUnauthorized) IsClientError() bool {
	return true
}

// IsServerError returns true when this add or update config item unauthorized response has a 5xx status code
func (o *AddOrUpdateConfigItemUnauthorized) IsServerError() bool {
	return false
}

// IsCode returns true when this add or update config item unauthorized response a status code equal to that given
func (o *AddOrUpdateConfigItemUnauthorized) IsCode(code int) bool {
	return code == 401
}

// Code gets the status code for the add or update config item unauthorized response
func (o *AddOrUpdateConfigItemUnauthorized) Code() int {
	return 401
}

func (o *AddOrUpdateConfigItemUnauthorized) Error() string {
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemUnauthorized", 401)
}

func (o *AddOrUpdateConfigItemUnauthorized) String() string {
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemUnauthorized", 401)
}

func (o *AddOrUpdateConfigItemUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewAddOrUpdateConfigItemForbidden creates a AddOrUpdateConfigItemForbidden with default headers values
func NewAddOrUpdateConfigItemForbidden() *AddOrUpdateConfigItemForbidden {
	return &AddOrUpdateConfigItemForbidden{}
}

/*
AddOrUpdateConfigItemForbidden describes a response with status code 403, with default header values.

Forbidden
*/
type AddOrUpdateConfigItemForbidden struct {
}

// IsSuccess returns true when this add or update config item forbidden response has a 2xx status code
func (o *AddOrUpdateConfigItemForbidden) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this add or update config item forbidden response has a 3xx status code
func (o *AddOrUpdateConfigItemForbidden) IsRedirect() bool {
	return false
}

// IsClientError returns true when this add or update config item forbidden response has a 4xx status code
func (o *AddOrUpdateConfigItemForbidden) IsClientError() bool {
	return true
}

// IsServerError returns true when this add or update config item forbidden response has a 5xx status code
func (o *AddOrUpdateConfigItemForbidden) IsServerError() bool {
	return false
}

// IsCode returns true when this add or update config item forbidden response a status code equal to that given
func (o *AddOrUpdateConfigItemForbidden) IsCode(code int) bool {
	return code == 403
}

// Code gets the status code for the add or update config item forbidden response
func (o *AddOrUpdateConfigItemForbidden) Code() int {
	return 403
}

func (o *AddOrUpdateConfigItemForbidden) Error() string {
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemForbidden", 403)
}

func (o *AddOrUpdateConfigItemForbidden) String() string {
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemForbidden", 403)
}

func (o *AddOrUpdateConfigItemForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewAddOrUpdateConfigItemInternalServerError creates a AddOrUpdateConfigItemInternalServerError with default headers values
func NewAddOrUpdateConfigItemInternalServerError() *AddOrUpdateConfigItemInternalServerError {
	return &AddOrUpdateConfigItemInternalServerError{}
}

/*
AddOrUpdateConfigItemInternalServerError describes a response with status code 500, with default header values.

Unexpected error
*/
type AddOrUpdateConfigItemInternalServerError struct {
	Payload *models.Error
}

// IsSuccess returns true when this add or update config item internal server error response has a 2xx status code
func (o *AddOrUpdateConfigItemInternalServerError) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this add or update config item internal server error response has a 3xx status code
func (o *AddOrUpdateConfigItemInternalServerError) IsRedirect() bool {
	return false
}

// IsClientError returns true when this add or update config item internal server error response has a 4xx status code
func (o *AddOrUpdateConfigItemInternalServerError) IsClientError() bool {
	return false
}

// IsServerError returns true when this add or update config item internal server error response has a 5xx status code
func (o *AddOrUpdateConfigItemInternalServerError) IsServerError() bool {
	return true
}

// IsCode returns true when this add or update config item internal server error response a status code equal to that given
func (o *AddOrUpdateConfigItemInternalServerError) IsCode(code int) bool {
	return code == 500
}

// Code gets the status code for the add or update config item internal server error response
func (o *AddOrUpdateConfigItemInternalServerError) Code() int {
	return 500
}

func (o *AddOrUpdateConfigItemInternalServerError) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemInternalServerError %s", 500, payload)
}

func (o *AddOrUpdateConfigItemInternalServerError) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PUT /kubernetes-clusters/{cluster_id}/config-items/{config_key}][%d] addOrUpdateConfigItemInternalServerError %s", 500, payload)
}

func (o *AddOrUpdateConfigItemInternalServerError) GetPayload() *models.Error {
	return o.Payload
}

func (o *AddOrUpdateConfigItemInternalServerError) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
