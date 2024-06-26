// Code generated by go-swagger; DO NOT EDIT.

package node_pools

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
)

// ListNodePoolsReader is a Reader for the ListNodePools structure.
type ListNodePoolsReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *ListNodePoolsReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewListNodePoolsOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 401:
		result := NewListNodePoolsUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 403:
		result := NewListNodePoolsForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewListNodePoolsInternalServerError()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		return nil, runtime.NewAPIError("[GET /kubernetes-clusters/{cluster_id}/node-pools] listNodePools", response, response.Code())
	}
}

// NewListNodePoolsOK creates a ListNodePoolsOK with default headers values
func NewListNodePoolsOK() *ListNodePoolsOK {
	return &ListNodePoolsOK{}
}

/*
ListNodePoolsOK describes a response with status code 200, with default header values.

List of node pools
*/
type ListNodePoolsOK struct {
	Payload *ListNodePoolsOKBody
}

// IsSuccess returns true when this list node pools o k response has a 2xx status code
func (o *ListNodePoolsOK) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this list node pools o k response has a 3xx status code
func (o *ListNodePoolsOK) IsRedirect() bool {
	return false
}

// IsClientError returns true when this list node pools o k response has a 4xx status code
func (o *ListNodePoolsOK) IsClientError() bool {
	return false
}

// IsServerError returns true when this list node pools o k response has a 5xx status code
func (o *ListNodePoolsOK) IsServerError() bool {
	return false
}

// IsCode returns true when this list node pools o k response a status code equal to that given
func (o *ListNodePoolsOK) IsCode(code int) bool {
	return code == 200
}

// Code gets the status code for the list node pools o k response
func (o *ListNodePoolsOK) Code() int {
	return 200
}

func (o *ListNodePoolsOK) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}/node-pools][%d] listNodePoolsOK %s", 200, payload)
}

func (o *ListNodePoolsOK) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}/node-pools][%d] listNodePoolsOK %s", 200, payload)
}

func (o *ListNodePoolsOK) GetPayload() *ListNodePoolsOKBody {
	return o.Payload
}

func (o *ListNodePoolsOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(ListNodePoolsOKBody)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewListNodePoolsUnauthorized creates a ListNodePoolsUnauthorized with default headers values
func NewListNodePoolsUnauthorized() *ListNodePoolsUnauthorized {
	return &ListNodePoolsUnauthorized{}
}

/*
ListNodePoolsUnauthorized describes a response with status code 401, with default header values.

Unauthorized
*/
type ListNodePoolsUnauthorized struct {
}

// IsSuccess returns true when this list node pools unauthorized response has a 2xx status code
func (o *ListNodePoolsUnauthorized) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this list node pools unauthorized response has a 3xx status code
func (o *ListNodePoolsUnauthorized) IsRedirect() bool {
	return false
}

// IsClientError returns true when this list node pools unauthorized response has a 4xx status code
func (o *ListNodePoolsUnauthorized) IsClientError() bool {
	return true
}

// IsServerError returns true when this list node pools unauthorized response has a 5xx status code
func (o *ListNodePoolsUnauthorized) IsServerError() bool {
	return false
}

// IsCode returns true when this list node pools unauthorized response a status code equal to that given
func (o *ListNodePoolsUnauthorized) IsCode(code int) bool {
	return code == 401
}

// Code gets the status code for the list node pools unauthorized response
func (o *ListNodePoolsUnauthorized) Code() int {
	return 401
}

func (o *ListNodePoolsUnauthorized) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}/node-pools][%d] listNodePoolsUnauthorized", 401)
}

func (o *ListNodePoolsUnauthorized) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}/node-pools][%d] listNodePoolsUnauthorized", 401)
}

func (o *ListNodePoolsUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewListNodePoolsForbidden creates a ListNodePoolsForbidden with default headers values
func NewListNodePoolsForbidden() *ListNodePoolsForbidden {
	return &ListNodePoolsForbidden{}
}

/*
ListNodePoolsForbidden describes a response with status code 403, with default header values.

Forbidden
*/
type ListNodePoolsForbidden struct {
}

// IsSuccess returns true when this list node pools forbidden response has a 2xx status code
func (o *ListNodePoolsForbidden) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this list node pools forbidden response has a 3xx status code
func (o *ListNodePoolsForbidden) IsRedirect() bool {
	return false
}

// IsClientError returns true when this list node pools forbidden response has a 4xx status code
func (o *ListNodePoolsForbidden) IsClientError() bool {
	return true
}

// IsServerError returns true when this list node pools forbidden response has a 5xx status code
func (o *ListNodePoolsForbidden) IsServerError() bool {
	return false
}

// IsCode returns true when this list node pools forbidden response a status code equal to that given
func (o *ListNodePoolsForbidden) IsCode(code int) bool {
	return code == 403
}

// Code gets the status code for the list node pools forbidden response
func (o *ListNodePoolsForbidden) Code() int {
	return 403
}

func (o *ListNodePoolsForbidden) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}/node-pools][%d] listNodePoolsForbidden", 403)
}

func (o *ListNodePoolsForbidden) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}/node-pools][%d] listNodePoolsForbidden", 403)
}

func (o *ListNodePoolsForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewListNodePoolsInternalServerError creates a ListNodePoolsInternalServerError with default headers values
func NewListNodePoolsInternalServerError() *ListNodePoolsInternalServerError {
	return &ListNodePoolsInternalServerError{}
}

/*
ListNodePoolsInternalServerError describes a response with status code 500, with default header values.

Unexpected error
*/
type ListNodePoolsInternalServerError struct {
	Payload *models.Error
}

// IsSuccess returns true when this list node pools internal server error response has a 2xx status code
func (o *ListNodePoolsInternalServerError) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this list node pools internal server error response has a 3xx status code
func (o *ListNodePoolsInternalServerError) IsRedirect() bool {
	return false
}

// IsClientError returns true when this list node pools internal server error response has a 4xx status code
func (o *ListNodePoolsInternalServerError) IsClientError() bool {
	return false
}

// IsServerError returns true when this list node pools internal server error response has a 5xx status code
func (o *ListNodePoolsInternalServerError) IsServerError() bool {
	return true
}

// IsCode returns true when this list node pools internal server error response a status code equal to that given
func (o *ListNodePoolsInternalServerError) IsCode(code int) bool {
	return code == 500
}

// Code gets the status code for the list node pools internal server error response
func (o *ListNodePoolsInternalServerError) Code() int {
	return 500
}

func (o *ListNodePoolsInternalServerError) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}/node-pools][%d] listNodePoolsInternalServerError %s", 500, payload)
}

func (o *ListNodePoolsInternalServerError) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}/node-pools][%d] listNodePoolsInternalServerError %s", 500, payload)
}

func (o *ListNodePoolsInternalServerError) GetPayload() *models.Error {
	return o.Payload
}

func (o *ListNodePoolsInternalServerError) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

/*
ListNodePoolsOKBody list node pools o k body
swagger:model ListNodePoolsOKBody
*/
type ListNodePoolsOKBody struct {

	// items
	Items []*models.NodePool `json:"items"`
}

// Validate validates this list node pools o k body
func (o *ListNodePoolsOKBody) Validate(formats strfmt.Registry) error {
	var res []error

	if err := o.validateItems(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (o *ListNodePoolsOKBody) validateItems(formats strfmt.Registry) error {
	if swag.IsZero(o.Items) { // not required
		return nil
	}

	for i := 0; i < len(o.Items); i++ {
		if swag.IsZero(o.Items[i]) { // not required
			continue
		}

		if o.Items[i] != nil {
			if err := o.Items[i].Validate(formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("listNodePoolsOK" + "." + "items" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("listNodePoolsOK" + "." + "items" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

// ContextValidate validate this list node pools o k body based on the context it is used
func (o *ListNodePoolsOKBody) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	var res []error

	if err := o.contextValidateItems(ctx, formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (o *ListNodePoolsOKBody) contextValidateItems(ctx context.Context, formats strfmt.Registry) error {

	for i := 0; i < len(o.Items); i++ {

		if o.Items[i] != nil {

			if swag.IsZero(o.Items[i]) { // not required
				return nil
			}

			if err := o.Items[i].ContextValidate(ctx, formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("listNodePoolsOK" + "." + "items" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("listNodePoolsOK" + "." + "items" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

// MarshalBinary interface implementation
func (o *ListNodePoolsOKBody) MarshalBinary() ([]byte, error) {
	if o == nil {
		return nil, nil
	}
	return swag.WriteJSON(o)
}

// UnmarshalBinary interface implementation
func (o *ListNodePoolsOKBody) UnmarshalBinary(b []byte) error {
	var res ListNodePoolsOKBody
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*o = res
	return nil
}
