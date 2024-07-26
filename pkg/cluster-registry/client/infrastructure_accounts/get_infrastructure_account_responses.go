// Code generated by go-swagger; DO NOT EDIT.

package infrastructure_accounts

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

// GetInfrastructureAccountReader is a Reader for the GetInfrastructureAccount structure.
type GetInfrastructureAccountReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *GetInfrastructureAccountReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewGetInfrastructureAccountOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 401:
		result := NewGetInfrastructureAccountUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 403:
		result := NewGetInfrastructureAccountForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 404:
		result := NewGetInfrastructureAccountNotFound()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewGetInfrastructureAccountInternalServerError()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		return nil, runtime.NewAPIError("[GET /infrastructure-accounts/{account_id}] getInfrastructureAccount", response, response.Code())
	}
}

// NewGetInfrastructureAccountOK creates a GetInfrastructureAccountOK with default headers values
func NewGetInfrastructureAccountOK() *GetInfrastructureAccountOK {
	return &GetInfrastructureAccountOK{}
}

/*
GetInfrastructureAccountOK describes a response with status code 200, with default header values.

Infrastructure account information.
*/
type GetInfrastructureAccountOK struct {
	Payload *models.InfrastructureAccount
}

// IsSuccess returns true when this get infrastructure account o k response has a 2xx status code
func (o *GetInfrastructureAccountOK) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this get infrastructure account o k response has a 3xx status code
func (o *GetInfrastructureAccountOK) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get infrastructure account o k response has a 4xx status code
func (o *GetInfrastructureAccountOK) IsClientError() bool {
	return false
}

// IsServerError returns true when this get infrastructure account o k response has a 5xx status code
func (o *GetInfrastructureAccountOK) IsServerError() bool {
	return false
}

// IsCode returns true when this get infrastructure account o k response a status code equal to that given
func (o *GetInfrastructureAccountOK) IsCode(code int) bool {
	return code == 200
}

// Code gets the status code for the get infrastructure account o k response
func (o *GetInfrastructureAccountOK) Code() int {
	return 200
}

func (o *GetInfrastructureAccountOK) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountOK %s", 200, payload)
}

func (o *GetInfrastructureAccountOK) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountOK %s", 200, payload)
}

func (o *GetInfrastructureAccountOK) GetPayload() *models.InfrastructureAccount {
	return o.Payload
}

func (o *GetInfrastructureAccountOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.InfrastructureAccount)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewGetInfrastructureAccountUnauthorized creates a GetInfrastructureAccountUnauthorized with default headers values
func NewGetInfrastructureAccountUnauthorized() *GetInfrastructureAccountUnauthorized {
	return &GetInfrastructureAccountUnauthorized{}
}

/*
GetInfrastructureAccountUnauthorized describes a response with status code 401, with default header values.

Unauthorized
*/
type GetInfrastructureAccountUnauthorized struct {
}

// IsSuccess returns true when this get infrastructure account unauthorized response has a 2xx status code
func (o *GetInfrastructureAccountUnauthorized) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this get infrastructure account unauthorized response has a 3xx status code
func (o *GetInfrastructureAccountUnauthorized) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get infrastructure account unauthorized response has a 4xx status code
func (o *GetInfrastructureAccountUnauthorized) IsClientError() bool {
	return true
}

// IsServerError returns true when this get infrastructure account unauthorized response has a 5xx status code
func (o *GetInfrastructureAccountUnauthorized) IsServerError() bool {
	return false
}

// IsCode returns true when this get infrastructure account unauthorized response a status code equal to that given
func (o *GetInfrastructureAccountUnauthorized) IsCode(code int) bool {
	return code == 401
}

// Code gets the status code for the get infrastructure account unauthorized response
func (o *GetInfrastructureAccountUnauthorized) Code() int {
	return 401
}

func (o *GetInfrastructureAccountUnauthorized) Error() string {
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountUnauthorized", 401)
}

func (o *GetInfrastructureAccountUnauthorized) String() string {
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountUnauthorized", 401)
}

func (o *GetInfrastructureAccountUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewGetInfrastructureAccountForbidden creates a GetInfrastructureAccountForbidden with default headers values
func NewGetInfrastructureAccountForbidden() *GetInfrastructureAccountForbidden {
	return &GetInfrastructureAccountForbidden{}
}

/*
GetInfrastructureAccountForbidden describes a response with status code 403, with default header values.

Forbidden
*/
type GetInfrastructureAccountForbidden struct {
}

// IsSuccess returns true when this get infrastructure account forbidden response has a 2xx status code
func (o *GetInfrastructureAccountForbidden) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this get infrastructure account forbidden response has a 3xx status code
func (o *GetInfrastructureAccountForbidden) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get infrastructure account forbidden response has a 4xx status code
func (o *GetInfrastructureAccountForbidden) IsClientError() bool {
	return true
}

// IsServerError returns true when this get infrastructure account forbidden response has a 5xx status code
func (o *GetInfrastructureAccountForbidden) IsServerError() bool {
	return false
}

// IsCode returns true when this get infrastructure account forbidden response a status code equal to that given
func (o *GetInfrastructureAccountForbidden) IsCode(code int) bool {
	return code == 403
}

// Code gets the status code for the get infrastructure account forbidden response
func (o *GetInfrastructureAccountForbidden) Code() int {
	return 403
}

func (o *GetInfrastructureAccountForbidden) Error() string {
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountForbidden", 403)
}

func (o *GetInfrastructureAccountForbidden) String() string {
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountForbidden", 403)
}

func (o *GetInfrastructureAccountForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewGetInfrastructureAccountNotFound creates a GetInfrastructureAccountNotFound with default headers values
func NewGetInfrastructureAccountNotFound() *GetInfrastructureAccountNotFound {
	return &GetInfrastructureAccountNotFound{}
}

/*
GetInfrastructureAccountNotFound describes a response with status code 404, with default header values.

InfrastructureAccount not found
*/
type GetInfrastructureAccountNotFound struct {
}

// IsSuccess returns true when this get infrastructure account not found response has a 2xx status code
func (o *GetInfrastructureAccountNotFound) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this get infrastructure account not found response has a 3xx status code
func (o *GetInfrastructureAccountNotFound) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get infrastructure account not found response has a 4xx status code
func (o *GetInfrastructureAccountNotFound) IsClientError() bool {
	return true
}

// IsServerError returns true when this get infrastructure account not found response has a 5xx status code
func (o *GetInfrastructureAccountNotFound) IsServerError() bool {
	return false
}

// IsCode returns true when this get infrastructure account not found response a status code equal to that given
func (o *GetInfrastructureAccountNotFound) IsCode(code int) bool {
	return code == 404
}

// Code gets the status code for the get infrastructure account not found response
func (o *GetInfrastructureAccountNotFound) Code() int {
	return 404
}

func (o *GetInfrastructureAccountNotFound) Error() string {
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountNotFound", 404)
}

func (o *GetInfrastructureAccountNotFound) String() string {
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountNotFound", 404)
}

func (o *GetInfrastructureAccountNotFound) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewGetInfrastructureAccountInternalServerError creates a GetInfrastructureAccountInternalServerError with default headers values
func NewGetInfrastructureAccountInternalServerError() *GetInfrastructureAccountInternalServerError {
	return &GetInfrastructureAccountInternalServerError{}
}

/*
GetInfrastructureAccountInternalServerError describes a response with status code 500, with default header values.

Unexpected error
*/
type GetInfrastructureAccountInternalServerError struct {
	Payload *models.Error
}

// IsSuccess returns true when this get infrastructure account internal server error response has a 2xx status code
func (o *GetInfrastructureAccountInternalServerError) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this get infrastructure account internal server error response has a 3xx status code
func (o *GetInfrastructureAccountInternalServerError) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get infrastructure account internal server error response has a 4xx status code
func (o *GetInfrastructureAccountInternalServerError) IsClientError() bool {
	return false
}

// IsServerError returns true when this get infrastructure account internal server error response has a 5xx status code
func (o *GetInfrastructureAccountInternalServerError) IsServerError() bool {
	return true
}

// IsCode returns true when this get infrastructure account internal server error response a status code equal to that given
func (o *GetInfrastructureAccountInternalServerError) IsCode(code int) bool {
	return code == 500
}

// Code gets the status code for the get infrastructure account internal server error response
func (o *GetInfrastructureAccountInternalServerError) Code() int {
	return 500
}

func (o *GetInfrastructureAccountInternalServerError) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountInternalServerError %s", 500, payload)
}

func (o *GetInfrastructureAccountInternalServerError) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[GET /infrastructure-accounts/{account_id}][%d] getInfrastructureAccountInternalServerError %s", 500, payload)
}

func (o *GetInfrastructureAccountInternalServerError) GetPayload() *models.Error {
	return o.Payload
}

func (o *GetInfrastructureAccountInternalServerError) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
