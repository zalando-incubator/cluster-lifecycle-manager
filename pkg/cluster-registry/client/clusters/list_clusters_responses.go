// Code generated by go-swagger; DO NOT EDIT.

package clusters

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
)

// ListClustersReader is a Reader for the ListClusters structure.
type ListClustersReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *ListClustersReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewListClustersOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 401:
		result := NewListClustersUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 403:
		result := NewListClustersForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewListClustersInternalServerError()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		return nil, runtime.NewAPIError("response status code does not match any response statuses defined for this endpoint in the swagger spec", response, response.Code())
	}
}

// NewListClustersOK creates a ListClustersOK with default headers values
func NewListClustersOK() *ListClustersOK {
	return &ListClustersOK{}
}

/*
ListClustersOK describes a response with status code 200, with default header values.

List of all Kubernetes clusters.
*/
type ListClustersOK struct {
	Payload *ListClustersOKBody
}

// IsSuccess returns true when this list clusters o k response has a 2xx status code
func (o *ListClustersOK) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this list clusters o k response has a 3xx status code
func (o *ListClustersOK) IsRedirect() bool {
	return false
}

// IsClientError returns true when this list clusters o k response has a 4xx status code
func (o *ListClustersOK) IsClientError() bool {
	return false
}

// IsServerError returns true when this list clusters o k response has a 5xx status code
func (o *ListClustersOK) IsServerError() bool {
	return false
}

// IsCode returns true when this list clusters o k response a status code equal to that given
func (o *ListClustersOK) IsCode(code int) bool {
	return code == 200
}

func (o *ListClustersOK) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters][%d] listClustersOK  %+v", 200, o.Payload)
}

func (o *ListClustersOK) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters][%d] listClustersOK  %+v", 200, o.Payload)
}

func (o *ListClustersOK) GetPayload() *ListClustersOKBody {
	return o.Payload
}

func (o *ListClustersOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(ListClustersOKBody)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewListClustersUnauthorized creates a ListClustersUnauthorized with default headers values
func NewListClustersUnauthorized() *ListClustersUnauthorized {
	return &ListClustersUnauthorized{}
}

/*
ListClustersUnauthorized describes a response with status code 401, with default header values.

Unauthorized
*/
type ListClustersUnauthorized struct {
}

// IsSuccess returns true when this list clusters unauthorized response has a 2xx status code
func (o *ListClustersUnauthorized) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this list clusters unauthorized response has a 3xx status code
func (o *ListClustersUnauthorized) IsRedirect() bool {
	return false
}

// IsClientError returns true when this list clusters unauthorized response has a 4xx status code
func (o *ListClustersUnauthorized) IsClientError() bool {
	return true
}

// IsServerError returns true when this list clusters unauthorized response has a 5xx status code
func (o *ListClustersUnauthorized) IsServerError() bool {
	return false
}

// IsCode returns true when this list clusters unauthorized response a status code equal to that given
func (o *ListClustersUnauthorized) IsCode(code int) bool {
	return code == 401
}

func (o *ListClustersUnauthorized) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters][%d] listClustersUnauthorized ", 401)
}

func (o *ListClustersUnauthorized) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters][%d] listClustersUnauthorized ", 401)
}

func (o *ListClustersUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewListClustersForbidden creates a ListClustersForbidden with default headers values
func NewListClustersForbidden() *ListClustersForbidden {
	return &ListClustersForbidden{}
}

/*
ListClustersForbidden describes a response with status code 403, with default header values.

Forbidden
*/
type ListClustersForbidden struct {
}

// IsSuccess returns true when this list clusters forbidden response has a 2xx status code
func (o *ListClustersForbidden) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this list clusters forbidden response has a 3xx status code
func (o *ListClustersForbidden) IsRedirect() bool {
	return false
}

// IsClientError returns true when this list clusters forbidden response has a 4xx status code
func (o *ListClustersForbidden) IsClientError() bool {
	return true
}

// IsServerError returns true when this list clusters forbidden response has a 5xx status code
func (o *ListClustersForbidden) IsServerError() bool {
	return false
}

// IsCode returns true when this list clusters forbidden response a status code equal to that given
func (o *ListClustersForbidden) IsCode(code int) bool {
	return code == 403
}

func (o *ListClustersForbidden) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters][%d] listClustersForbidden ", 403)
}

func (o *ListClustersForbidden) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters][%d] listClustersForbidden ", 403)
}

func (o *ListClustersForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewListClustersInternalServerError creates a ListClustersInternalServerError with default headers values
func NewListClustersInternalServerError() *ListClustersInternalServerError {
	return &ListClustersInternalServerError{}
}

/*
ListClustersInternalServerError describes a response with status code 500, with default header values.

Unexpected error
*/
type ListClustersInternalServerError struct {
	Payload *models.Error
}

// IsSuccess returns true when this list clusters internal server error response has a 2xx status code
func (o *ListClustersInternalServerError) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this list clusters internal server error response has a 3xx status code
func (o *ListClustersInternalServerError) IsRedirect() bool {
	return false
}

// IsClientError returns true when this list clusters internal server error response has a 4xx status code
func (o *ListClustersInternalServerError) IsClientError() bool {
	return false
}

// IsServerError returns true when this list clusters internal server error response has a 5xx status code
func (o *ListClustersInternalServerError) IsServerError() bool {
	return true
}

// IsCode returns true when this list clusters internal server error response a status code equal to that given
func (o *ListClustersInternalServerError) IsCode(code int) bool {
	return code == 500
}

func (o *ListClustersInternalServerError) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters][%d] listClustersInternalServerError  %+v", 500, o.Payload)
}

func (o *ListClustersInternalServerError) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters][%d] listClustersInternalServerError  %+v", 500, o.Payload)
}

func (o *ListClustersInternalServerError) GetPayload() *models.Error {
	return o.Payload
}

func (o *ListClustersInternalServerError) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

/*
ListClustersOKBody list clusters o k body
swagger:model ListClustersOKBody
*/
type ListClustersOKBody struct {

	// items
	Items []*models.Cluster `json:"items"`
}

// Validate validates this list clusters o k body
func (o *ListClustersOKBody) Validate(formats strfmt.Registry) error {
	var res []error

	if err := o.validateItems(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (o *ListClustersOKBody) validateItems(formats strfmt.Registry) error {
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
					return ve.ValidateName("listClustersOK" + "." + "items" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("listClustersOK" + "." + "items" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

// ContextValidate validate this list clusters o k body based on the context it is used
func (o *ListClustersOKBody) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	var res []error

	if err := o.contextValidateItems(ctx, formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (o *ListClustersOKBody) contextValidateItems(ctx context.Context, formats strfmt.Registry) error {

	for i := 0; i < len(o.Items); i++ {

		if o.Items[i] != nil {
			if err := o.Items[i].ContextValidate(ctx, formats); err != nil {
				if ve, ok := err.(*errors.Validation); ok {
					return ve.ValidateName("listClustersOK" + "." + "items" + "." + strconv.Itoa(i))
				} else if ce, ok := err.(*errors.CompositeError); ok {
					return ce.ValidateName("listClustersOK" + "." + "items" + "." + strconv.Itoa(i))
				}
				return err
			}
		}

	}

	return nil
}

// MarshalBinary interface implementation
func (o *ListClustersOKBody) MarshalBinary() ([]byte, error) {
	if o == nil {
		return nil, nil
	}
	return swag.WriteJSON(o)
}

// UnmarshalBinary interface implementation
func (o *ListClustersOKBody) UnmarshalBinary(b []byte) error {
	var res ListClustersOKBody
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*o = res
	return nil
}
