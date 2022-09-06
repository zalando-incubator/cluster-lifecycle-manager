// Code generated by go-swagger; DO NOT EDIT.

package clusters

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
)

// GetClusterReader is a Reader for the GetCluster structure.
type GetClusterReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *GetClusterReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewGetClusterOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 401:
		result := NewGetClusterUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 403:
		result := NewGetClusterForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 404:
		result := NewGetClusterNotFound()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewGetClusterInternalServerError()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		return nil, runtime.NewAPIError("response status code does not match any response statuses defined for this endpoint in the swagger spec", response, response.Code())
	}
}

// NewGetClusterOK creates a GetClusterOK with default headers values
func NewGetClusterOK() *GetClusterOK {
	return &GetClusterOK{}
}

/*
GetClusterOK describes a response with status code 200, with default header values.

Cluster information.
*/
type GetClusterOK struct {
	Payload *models.Cluster
}

// IsSuccess returns true when this get cluster o k response has a 2xx status code
func (o *GetClusterOK) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this get cluster o k response has a 3xx status code
func (o *GetClusterOK) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get cluster o k response has a 4xx status code
func (o *GetClusterOK) IsClientError() bool {
	return false
}

// IsServerError returns true when this get cluster o k response has a 5xx status code
func (o *GetClusterOK) IsServerError() bool {
	return false
}

// IsCode returns true when this get cluster o k response a status code equal to that given
func (o *GetClusterOK) IsCode(code int) bool {
	return code == 200
}

func (o *GetClusterOK) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterOK  %+v", 200, o.Payload)
}

func (o *GetClusterOK) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterOK  %+v", 200, o.Payload)
}

func (o *GetClusterOK) GetPayload() *models.Cluster {
	return o.Payload
}

func (o *GetClusterOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Cluster)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewGetClusterUnauthorized creates a GetClusterUnauthorized with default headers values
func NewGetClusterUnauthorized() *GetClusterUnauthorized {
	return &GetClusterUnauthorized{}
}

/*
GetClusterUnauthorized describes a response with status code 401, with default header values.

Unauthorized
*/
type GetClusterUnauthorized struct {
}

// IsSuccess returns true when this get cluster unauthorized response has a 2xx status code
func (o *GetClusterUnauthorized) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this get cluster unauthorized response has a 3xx status code
func (o *GetClusterUnauthorized) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get cluster unauthorized response has a 4xx status code
func (o *GetClusterUnauthorized) IsClientError() bool {
	return true
}

// IsServerError returns true when this get cluster unauthorized response has a 5xx status code
func (o *GetClusterUnauthorized) IsServerError() bool {
	return false
}

// IsCode returns true when this get cluster unauthorized response a status code equal to that given
func (o *GetClusterUnauthorized) IsCode(code int) bool {
	return code == 401
}

func (o *GetClusterUnauthorized) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterUnauthorized ", 401)
}

func (o *GetClusterUnauthorized) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterUnauthorized ", 401)
}

func (o *GetClusterUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewGetClusterForbidden creates a GetClusterForbidden with default headers values
func NewGetClusterForbidden() *GetClusterForbidden {
	return &GetClusterForbidden{}
}

/*
GetClusterForbidden describes a response with status code 403, with default header values.

Forbidden
*/
type GetClusterForbidden struct {
}

// IsSuccess returns true when this get cluster forbidden response has a 2xx status code
func (o *GetClusterForbidden) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this get cluster forbidden response has a 3xx status code
func (o *GetClusterForbidden) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get cluster forbidden response has a 4xx status code
func (o *GetClusterForbidden) IsClientError() bool {
	return true
}

// IsServerError returns true when this get cluster forbidden response has a 5xx status code
func (o *GetClusterForbidden) IsServerError() bool {
	return false
}

// IsCode returns true when this get cluster forbidden response a status code equal to that given
func (o *GetClusterForbidden) IsCode(code int) bool {
	return code == 403
}

func (o *GetClusterForbidden) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterForbidden ", 403)
}

func (o *GetClusterForbidden) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterForbidden ", 403)
}

func (o *GetClusterForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewGetClusterNotFound creates a GetClusterNotFound with default headers values
func NewGetClusterNotFound() *GetClusterNotFound {
	return &GetClusterNotFound{}
}

/*
GetClusterNotFound describes a response with status code 404, with default header values.

Cluster not found
*/
type GetClusterNotFound struct {
}

// IsSuccess returns true when this get cluster not found response has a 2xx status code
func (o *GetClusterNotFound) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this get cluster not found response has a 3xx status code
func (o *GetClusterNotFound) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get cluster not found response has a 4xx status code
func (o *GetClusterNotFound) IsClientError() bool {
	return true
}

// IsServerError returns true when this get cluster not found response has a 5xx status code
func (o *GetClusterNotFound) IsServerError() bool {
	return false
}

// IsCode returns true when this get cluster not found response a status code equal to that given
func (o *GetClusterNotFound) IsCode(code int) bool {
	return code == 404
}

func (o *GetClusterNotFound) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterNotFound ", 404)
}

func (o *GetClusterNotFound) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterNotFound ", 404)
}

func (o *GetClusterNotFound) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewGetClusterInternalServerError creates a GetClusterInternalServerError with default headers values
func NewGetClusterInternalServerError() *GetClusterInternalServerError {
	return &GetClusterInternalServerError{}
}

/*
GetClusterInternalServerError describes a response with status code 500, with default header values.

Unexpected error
*/
type GetClusterInternalServerError struct {
	Payload *models.Error
}

// IsSuccess returns true when this get cluster internal server error response has a 2xx status code
func (o *GetClusterInternalServerError) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this get cluster internal server error response has a 3xx status code
func (o *GetClusterInternalServerError) IsRedirect() bool {
	return false
}

// IsClientError returns true when this get cluster internal server error response has a 4xx status code
func (o *GetClusterInternalServerError) IsClientError() bool {
	return false
}

// IsServerError returns true when this get cluster internal server error response has a 5xx status code
func (o *GetClusterInternalServerError) IsServerError() bool {
	return true
}

// IsCode returns true when this get cluster internal server error response a status code equal to that given
func (o *GetClusterInternalServerError) IsCode(code int) bool {
	return code == 500
}

func (o *GetClusterInternalServerError) Error() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterInternalServerError  %+v", 500, o.Payload)
}

func (o *GetClusterInternalServerError) String() string {
	return fmt.Sprintf("[GET /kubernetes-clusters/{cluster_id}][%d] getClusterInternalServerError  %+v", 500, o.Payload)
}

func (o *GetClusterInternalServerError) GetPayload() *models.Error {
	return o.Payload
}

func (o *GetClusterInternalServerError) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
