// Code generated by go-swagger; DO NOT EDIT.

package clusters

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

// UpdateClusterReader is a Reader for the UpdateCluster structure.
type UpdateClusterReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *UpdateClusterReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewUpdateClusterOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 401:
		result := NewUpdateClusterUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 403:
		result := NewUpdateClusterForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 404:
		result := NewUpdateClusterNotFound()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewUpdateClusterInternalServerError()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		return nil, runtime.NewAPIError("[PATCH /kubernetes-clusters/{cluster_id}] updateCluster", response, response.Code())
	}
}

// NewUpdateClusterOK creates a UpdateClusterOK with default headers values
func NewUpdateClusterOK() *UpdateClusterOK {
	return &UpdateClusterOK{}
}

/*
UpdateClusterOK describes a response with status code 200, with default header values.

The cluster update request is performed and the updated cluster is returned.
*/
type UpdateClusterOK struct {
	Payload *models.Cluster
}

// IsSuccess returns true when this update cluster o k response has a 2xx status code
func (o *UpdateClusterOK) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this update cluster o k response has a 3xx status code
func (o *UpdateClusterOK) IsRedirect() bool {
	return false
}

// IsClientError returns true when this update cluster o k response has a 4xx status code
func (o *UpdateClusterOK) IsClientError() bool {
	return false
}

// IsServerError returns true when this update cluster o k response has a 5xx status code
func (o *UpdateClusterOK) IsServerError() bool {
	return false
}

// IsCode returns true when this update cluster o k response a status code equal to that given
func (o *UpdateClusterOK) IsCode(code int) bool {
	return code == 200
}

// Code gets the status code for the update cluster o k response
func (o *UpdateClusterOK) Code() int {
	return 200
}

func (o *UpdateClusterOK) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterOK %s", 200, payload)
}

func (o *UpdateClusterOK) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterOK %s", 200, payload)
}

func (o *UpdateClusterOK) GetPayload() *models.Cluster {
	return o.Payload
}

func (o *UpdateClusterOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Cluster)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewUpdateClusterUnauthorized creates a UpdateClusterUnauthorized with default headers values
func NewUpdateClusterUnauthorized() *UpdateClusterUnauthorized {
	return &UpdateClusterUnauthorized{}
}

/*
UpdateClusterUnauthorized describes a response with status code 401, with default header values.

Unauthorized
*/
type UpdateClusterUnauthorized struct {
}

// IsSuccess returns true when this update cluster unauthorized response has a 2xx status code
func (o *UpdateClusterUnauthorized) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this update cluster unauthorized response has a 3xx status code
func (o *UpdateClusterUnauthorized) IsRedirect() bool {
	return false
}

// IsClientError returns true when this update cluster unauthorized response has a 4xx status code
func (o *UpdateClusterUnauthorized) IsClientError() bool {
	return true
}

// IsServerError returns true when this update cluster unauthorized response has a 5xx status code
func (o *UpdateClusterUnauthorized) IsServerError() bool {
	return false
}

// IsCode returns true when this update cluster unauthorized response a status code equal to that given
func (o *UpdateClusterUnauthorized) IsCode(code int) bool {
	return code == 401
}

// Code gets the status code for the update cluster unauthorized response
func (o *UpdateClusterUnauthorized) Code() int {
	return 401
}

func (o *UpdateClusterUnauthorized) Error() string {
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterUnauthorized", 401)
}

func (o *UpdateClusterUnauthorized) String() string {
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterUnauthorized", 401)
}

func (o *UpdateClusterUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewUpdateClusterForbidden creates a UpdateClusterForbidden with default headers values
func NewUpdateClusterForbidden() *UpdateClusterForbidden {
	return &UpdateClusterForbidden{}
}

/*
UpdateClusterForbidden describes a response with status code 403, with default header values.

Forbidden
*/
type UpdateClusterForbidden struct {
}

// IsSuccess returns true when this update cluster forbidden response has a 2xx status code
func (o *UpdateClusterForbidden) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this update cluster forbidden response has a 3xx status code
func (o *UpdateClusterForbidden) IsRedirect() bool {
	return false
}

// IsClientError returns true when this update cluster forbidden response has a 4xx status code
func (o *UpdateClusterForbidden) IsClientError() bool {
	return true
}

// IsServerError returns true when this update cluster forbidden response has a 5xx status code
func (o *UpdateClusterForbidden) IsServerError() bool {
	return false
}

// IsCode returns true when this update cluster forbidden response a status code equal to that given
func (o *UpdateClusterForbidden) IsCode(code int) bool {
	return code == 403
}

// Code gets the status code for the update cluster forbidden response
func (o *UpdateClusterForbidden) Code() int {
	return 403
}

func (o *UpdateClusterForbidden) Error() string {
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterForbidden", 403)
}

func (o *UpdateClusterForbidden) String() string {
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterForbidden", 403)
}

func (o *UpdateClusterForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewUpdateClusterNotFound creates a UpdateClusterNotFound with default headers values
func NewUpdateClusterNotFound() *UpdateClusterNotFound {
	return &UpdateClusterNotFound{}
}

/*
UpdateClusterNotFound describes a response with status code 404, with default header values.

Cluster not found
*/
type UpdateClusterNotFound struct {
}

// IsSuccess returns true when this update cluster not found response has a 2xx status code
func (o *UpdateClusterNotFound) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this update cluster not found response has a 3xx status code
func (o *UpdateClusterNotFound) IsRedirect() bool {
	return false
}

// IsClientError returns true when this update cluster not found response has a 4xx status code
func (o *UpdateClusterNotFound) IsClientError() bool {
	return true
}

// IsServerError returns true when this update cluster not found response has a 5xx status code
func (o *UpdateClusterNotFound) IsServerError() bool {
	return false
}

// IsCode returns true when this update cluster not found response a status code equal to that given
func (o *UpdateClusterNotFound) IsCode(code int) bool {
	return code == 404
}

// Code gets the status code for the update cluster not found response
func (o *UpdateClusterNotFound) Code() int {
	return 404
}

func (o *UpdateClusterNotFound) Error() string {
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterNotFound", 404)
}

func (o *UpdateClusterNotFound) String() string {
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterNotFound", 404)
}

func (o *UpdateClusterNotFound) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewUpdateClusterInternalServerError creates a UpdateClusterInternalServerError with default headers values
func NewUpdateClusterInternalServerError() *UpdateClusterInternalServerError {
	return &UpdateClusterInternalServerError{}
}

/*
UpdateClusterInternalServerError describes a response with status code 500, with default header values.

Unexpected error
*/
type UpdateClusterInternalServerError struct {
	Payload *models.Error
}

// IsSuccess returns true when this update cluster internal server error response has a 2xx status code
func (o *UpdateClusterInternalServerError) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this update cluster internal server error response has a 3xx status code
func (o *UpdateClusterInternalServerError) IsRedirect() bool {
	return false
}

// IsClientError returns true when this update cluster internal server error response has a 4xx status code
func (o *UpdateClusterInternalServerError) IsClientError() bool {
	return false
}

// IsServerError returns true when this update cluster internal server error response has a 5xx status code
func (o *UpdateClusterInternalServerError) IsServerError() bool {
	return true
}

// IsCode returns true when this update cluster internal server error response a status code equal to that given
func (o *UpdateClusterInternalServerError) IsCode(code int) bool {
	return code == 500
}

// Code gets the status code for the update cluster internal server error response
func (o *UpdateClusterInternalServerError) Code() int {
	return 500
}

func (o *UpdateClusterInternalServerError) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterInternalServerError %s", 500, payload)
}

func (o *UpdateClusterInternalServerError) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /kubernetes-clusters/{cluster_id}][%d] updateClusterInternalServerError %s", 500, payload)
}

func (o *UpdateClusterInternalServerError) GetPayload() *models.Error {
	return o.Payload
}

func (o *UpdateClusterInternalServerError) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
