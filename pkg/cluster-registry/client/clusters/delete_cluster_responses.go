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

// DeleteClusterReader is a Reader for the DeleteCluster structure.
type DeleteClusterReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *DeleteClusterReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 204:
		result := NewDeleteClusterNoContent()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 400:
		result := NewDeleteClusterBadRequest()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 401:
		result := NewDeleteClusterUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 403:
		result := NewDeleteClusterForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 404:
		result := NewDeleteClusterNotFound()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewDeleteClusterInternalServerError()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		return nil, runtime.NewAPIError("response status code does not match any response statuses defined for this endpoint in the swagger spec", response, response.Code())
	}
}

// NewDeleteClusterNoContent creates a DeleteClusterNoContent with default headers values
func NewDeleteClusterNoContent() *DeleteClusterNoContent {
	return &DeleteClusterNoContent{}
}

/*
DeleteClusterNoContent describes a response with status code 204, with default header values.

Cluster deleted
*/
type DeleteClusterNoContent struct {
}

// IsSuccess returns true when this delete cluster no content response has a 2xx status code
func (o *DeleteClusterNoContent) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this delete cluster no content response has a 3xx status code
func (o *DeleteClusterNoContent) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete cluster no content response has a 4xx status code
func (o *DeleteClusterNoContent) IsClientError() bool {
	return false
}

// IsServerError returns true when this delete cluster no content response has a 5xx status code
func (o *DeleteClusterNoContent) IsServerError() bool {
	return false
}

// IsCode returns true when this delete cluster no content response a status code equal to that given
func (o *DeleteClusterNoContent) IsCode(code int) bool {
	return code == 204
}

// Code gets the status code for the delete cluster no content response
func (o *DeleteClusterNoContent) Code() int {
	return 204
}

func (o *DeleteClusterNoContent) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterNoContent ", 204)
}

func (o *DeleteClusterNoContent) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterNoContent ", 204)
}

func (o *DeleteClusterNoContent) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteClusterBadRequest creates a DeleteClusterBadRequest with default headers values
func NewDeleteClusterBadRequest() *DeleteClusterBadRequest {
	return &DeleteClusterBadRequest{}
}

/*
DeleteClusterBadRequest describes a response with status code 400, with default header values.

Invalid request
*/
type DeleteClusterBadRequest struct {
	Payload *models.Error
}

// IsSuccess returns true when this delete cluster bad request response has a 2xx status code
func (o *DeleteClusterBadRequest) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete cluster bad request response has a 3xx status code
func (o *DeleteClusterBadRequest) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete cluster bad request response has a 4xx status code
func (o *DeleteClusterBadRequest) IsClientError() bool {
	return true
}

// IsServerError returns true when this delete cluster bad request response has a 5xx status code
func (o *DeleteClusterBadRequest) IsServerError() bool {
	return false
}

// IsCode returns true when this delete cluster bad request response a status code equal to that given
func (o *DeleteClusterBadRequest) IsCode(code int) bool {
	return code == 400
}

// Code gets the status code for the delete cluster bad request response
func (o *DeleteClusterBadRequest) Code() int {
	return 400
}

func (o *DeleteClusterBadRequest) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterBadRequest  %+v", 400, o.Payload)
}

func (o *DeleteClusterBadRequest) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterBadRequest  %+v", 400, o.Payload)
}

func (o *DeleteClusterBadRequest) GetPayload() *models.Error {
	return o.Payload
}

func (o *DeleteClusterBadRequest) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewDeleteClusterUnauthorized creates a DeleteClusterUnauthorized with default headers values
func NewDeleteClusterUnauthorized() *DeleteClusterUnauthorized {
	return &DeleteClusterUnauthorized{}
}

/*
DeleteClusterUnauthorized describes a response with status code 401, with default header values.

Unauthorized
*/
type DeleteClusterUnauthorized struct {
}

// IsSuccess returns true when this delete cluster unauthorized response has a 2xx status code
func (o *DeleteClusterUnauthorized) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete cluster unauthorized response has a 3xx status code
func (o *DeleteClusterUnauthorized) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete cluster unauthorized response has a 4xx status code
func (o *DeleteClusterUnauthorized) IsClientError() bool {
	return true
}

// IsServerError returns true when this delete cluster unauthorized response has a 5xx status code
func (o *DeleteClusterUnauthorized) IsServerError() bool {
	return false
}

// IsCode returns true when this delete cluster unauthorized response a status code equal to that given
func (o *DeleteClusterUnauthorized) IsCode(code int) bool {
	return code == 401
}

// Code gets the status code for the delete cluster unauthorized response
func (o *DeleteClusterUnauthorized) Code() int {
	return 401
}

func (o *DeleteClusterUnauthorized) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterUnauthorized ", 401)
}

func (o *DeleteClusterUnauthorized) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterUnauthorized ", 401)
}

func (o *DeleteClusterUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteClusterForbidden creates a DeleteClusterForbidden with default headers values
func NewDeleteClusterForbidden() *DeleteClusterForbidden {
	return &DeleteClusterForbidden{}
}

/*
DeleteClusterForbidden describes a response with status code 403, with default header values.

Forbidden
*/
type DeleteClusterForbidden struct {
	Payload *models.Error
}

// IsSuccess returns true when this delete cluster forbidden response has a 2xx status code
func (o *DeleteClusterForbidden) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete cluster forbidden response has a 3xx status code
func (o *DeleteClusterForbidden) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete cluster forbidden response has a 4xx status code
func (o *DeleteClusterForbidden) IsClientError() bool {
	return true
}

// IsServerError returns true when this delete cluster forbidden response has a 5xx status code
func (o *DeleteClusterForbidden) IsServerError() bool {
	return false
}

// IsCode returns true when this delete cluster forbidden response a status code equal to that given
func (o *DeleteClusterForbidden) IsCode(code int) bool {
	return code == 403
}

// Code gets the status code for the delete cluster forbidden response
func (o *DeleteClusterForbidden) Code() int {
	return 403
}

func (o *DeleteClusterForbidden) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterForbidden  %+v", 403, o.Payload)
}

func (o *DeleteClusterForbidden) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterForbidden  %+v", 403, o.Payload)
}

func (o *DeleteClusterForbidden) GetPayload() *models.Error {
	return o.Payload
}

func (o *DeleteClusterForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewDeleteClusterNotFound creates a DeleteClusterNotFound with default headers values
func NewDeleteClusterNotFound() *DeleteClusterNotFound {
	return &DeleteClusterNotFound{}
}

/*
DeleteClusterNotFound describes a response with status code 404, with default header values.

Cluster not found
*/
type DeleteClusterNotFound struct {
}

// IsSuccess returns true when this delete cluster not found response has a 2xx status code
func (o *DeleteClusterNotFound) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete cluster not found response has a 3xx status code
func (o *DeleteClusterNotFound) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete cluster not found response has a 4xx status code
func (o *DeleteClusterNotFound) IsClientError() bool {
	return true
}

// IsServerError returns true when this delete cluster not found response has a 5xx status code
func (o *DeleteClusterNotFound) IsServerError() bool {
	return false
}

// IsCode returns true when this delete cluster not found response a status code equal to that given
func (o *DeleteClusterNotFound) IsCode(code int) bool {
	return code == 404
}

// Code gets the status code for the delete cluster not found response
func (o *DeleteClusterNotFound) Code() int {
	return 404
}

func (o *DeleteClusterNotFound) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterNotFound ", 404)
}

func (o *DeleteClusterNotFound) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterNotFound ", 404)
}

func (o *DeleteClusterNotFound) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteClusterInternalServerError creates a DeleteClusterInternalServerError with default headers values
func NewDeleteClusterInternalServerError() *DeleteClusterInternalServerError {
	return &DeleteClusterInternalServerError{}
}

/*
DeleteClusterInternalServerError describes a response with status code 500, with default header values.

Unexpected error
*/
type DeleteClusterInternalServerError struct {
	Payload *models.Error
}

// IsSuccess returns true when this delete cluster internal server error response has a 2xx status code
func (o *DeleteClusterInternalServerError) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete cluster internal server error response has a 3xx status code
func (o *DeleteClusterInternalServerError) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete cluster internal server error response has a 4xx status code
func (o *DeleteClusterInternalServerError) IsClientError() bool {
	return false
}

// IsServerError returns true when this delete cluster internal server error response has a 5xx status code
func (o *DeleteClusterInternalServerError) IsServerError() bool {
	return true
}

// IsCode returns true when this delete cluster internal server error response a status code equal to that given
func (o *DeleteClusterInternalServerError) IsCode(code int) bool {
	return code == 500
}

// Code gets the status code for the delete cluster internal server error response
func (o *DeleteClusterInternalServerError) Code() int {
	return 500
}

func (o *DeleteClusterInternalServerError) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterInternalServerError  %+v", 500, o.Payload)
}

func (o *DeleteClusterInternalServerError) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}][%d] deleteClusterInternalServerError  %+v", 500, o.Payload)
}

func (o *DeleteClusterInternalServerError) GetPayload() *models.Error {
	return o.Payload
}

func (o *DeleteClusterInternalServerError) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
