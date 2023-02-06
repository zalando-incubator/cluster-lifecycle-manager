// Code generated by go-swagger; DO NOT EDIT.

package node_pool_config_items

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/zalando-incubator/cluster-lifecycle-manager/pkg/cluster-registry/models"
)

// DeleteNodePoolConfigItemReader is a Reader for the DeleteNodePoolConfigItem structure.
type DeleteNodePoolConfigItemReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *DeleteNodePoolConfigItemReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 204:
		result := NewDeleteNodePoolConfigItemNoContent()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 400:
		result := NewDeleteNodePoolConfigItemBadRequest()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 401:
		result := NewDeleteNodePoolConfigItemUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 403:
		result := NewDeleteNodePoolConfigItemForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 404:
		result := NewDeleteNodePoolConfigItemNotFound()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 500:
		result := NewDeleteNodePoolConfigItemInternalServerError()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		return nil, runtime.NewAPIError("response status code does not match any response statuses defined for this endpoint in the swagger spec", response, response.Code())
	}
}

// NewDeleteNodePoolConfigItemNoContent creates a DeleteNodePoolConfigItemNoContent with default headers values
func NewDeleteNodePoolConfigItemNoContent() *DeleteNodePoolConfigItemNoContent {
	return &DeleteNodePoolConfigItemNoContent{}
}

/*
DeleteNodePoolConfigItemNoContent describes a response with status code 204, with default header values.

Config item deleted.
*/
type DeleteNodePoolConfigItemNoContent struct {
}

// IsSuccess returns true when this delete node pool config item no content response has a 2xx status code
func (o *DeleteNodePoolConfigItemNoContent) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this delete node pool config item no content response has a 3xx status code
func (o *DeleteNodePoolConfigItemNoContent) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete node pool config item no content response has a 4xx status code
func (o *DeleteNodePoolConfigItemNoContent) IsClientError() bool {
	return false
}

// IsServerError returns true when this delete node pool config item no content response has a 5xx status code
func (o *DeleteNodePoolConfigItemNoContent) IsServerError() bool {
	return false
}

// IsCode returns true when this delete node pool config item no content response a status code equal to that given
func (o *DeleteNodePoolConfigItemNoContent) IsCode(code int) bool {
	return code == 204
}

// Code gets the status code for the delete node pool config item no content response
func (o *DeleteNodePoolConfigItemNoContent) Code() int {
	return 204
}

func (o *DeleteNodePoolConfigItemNoContent) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemNoContent ", 204)
}

func (o *DeleteNodePoolConfigItemNoContent) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemNoContent ", 204)
}

func (o *DeleteNodePoolConfigItemNoContent) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteNodePoolConfigItemBadRequest creates a DeleteNodePoolConfigItemBadRequest with default headers values
func NewDeleteNodePoolConfigItemBadRequest() *DeleteNodePoolConfigItemBadRequest {
	return &DeleteNodePoolConfigItemBadRequest{}
}

/*
DeleteNodePoolConfigItemBadRequest describes a response with status code 400, with default header values.

Invalid request
*/
type DeleteNodePoolConfigItemBadRequest struct {
	Payload *models.Error
}

// IsSuccess returns true when this delete node pool config item bad request response has a 2xx status code
func (o *DeleteNodePoolConfigItemBadRequest) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete node pool config item bad request response has a 3xx status code
func (o *DeleteNodePoolConfigItemBadRequest) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete node pool config item bad request response has a 4xx status code
func (o *DeleteNodePoolConfigItemBadRequest) IsClientError() bool {
	return true
}

// IsServerError returns true when this delete node pool config item bad request response has a 5xx status code
func (o *DeleteNodePoolConfigItemBadRequest) IsServerError() bool {
	return false
}

// IsCode returns true when this delete node pool config item bad request response a status code equal to that given
func (o *DeleteNodePoolConfigItemBadRequest) IsCode(code int) bool {
	return code == 400
}

// Code gets the status code for the delete node pool config item bad request response
func (o *DeleteNodePoolConfigItemBadRequest) Code() int {
	return 400
}

func (o *DeleteNodePoolConfigItemBadRequest) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemBadRequest  %+v", 400, o.Payload)
}

func (o *DeleteNodePoolConfigItemBadRequest) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemBadRequest  %+v", 400, o.Payload)
}

func (o *DeleteNodePoolConfigItemBadRequest) GetPayload() *models.Error {
	return o.Payload
}

func (o *DeleteNodePoolConfigItemBadRequest) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewDeleteNodePoolConfigItemUnauthorized creates a DeleteNodePoolConfigItemUnauthorized with default headers values
func NewDeleteNodePoolConfigItemUnauthorized() *DeleteNodePoolConfigItemUnauthorized {
	return &DeleteNodePoolConfigItemUnauthorized{}
}

/*
DeleteNodePoolConfigItemUnauthorized describes a response with status code 401, with default header values.

Unauthorized
*/
type DeleteNodePoolConfigItemUnauthorized struct {
}

// IsSuccess returns true when this delete node pool config item unauthorized response has a 2xx status code
func (o *DeleteNodePoolConfigItemUnauthorized) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete node pool config item unauthorized response has a 3xx status code
func (o *DeleteNodePoolConfigItemUnauthorized) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete node pool config item unauthorized response has a 4xx status code
func (o *DeleteNodePoolConfigItemUnauthorized) IsClientError() bool {
	return true
}

// IsServerError returns true when this delete node pool config item unauthorized response has a 5xx status code
func (o *DeleteNodePoolConfigItemUnauthorized) IsServerError() bool {
	return false
}

// IsCode returns true when this delete node pool config item unauthorized response a status code equal to that given
func (o *DeleteNodePoolConfigItemUnauthorized) IsCode(code int) bool {
	return code == 401
}

// Code gets the status code for the delete node pool config item unauthorized response
func (o *DeleteNodePoolConfigItemUnauthorized) Code() int {
	return 401
}

func (o *DeleteNodePoolConfigItemUnauthorized) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemUnauthorized ", 401)
}

func (o *DeleteNodePoolConfigItemUnauthorized) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemUnauthorized ", 401)
}

func (o *DeleteNodePoolConfigItemUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteNodePoolConfigItemForbidden creates a DeleteNodePoolConfigItemForbidden with default headers values
func NewDeleteNodePoolConfigItemForbidden() *DeleteNodePoolConfigItemForbidden {
	return &DeleteNodePoolConfigItemForbidden{}
}

/*
DeleteNodePoolConfigItemForbidden describes a response with status code 403, with default header values.

Forbidden
*/
type DeleteNodePoolConfigItemForbidden struct {
}

// IsSuccess returns true when this delete node pool config item forbidden response has a 2xx status code
func (o *DeleteNodePoolConfigItemForbidden) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete node pool config item forbidden response has a 3xx status code
func (o *DeleteNodePoolConfigItemForbidden) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete node pool config item forbidden response has a 4xx status code
func (o *DeleteNodePoolConfigItemForbidden) IsClientError() bool {
	return true
}

// IsServerError returns true when this delete node pool config item forbidden response has a 5xx status code
func (o *DeleteNodePoolConfigItemForbidden) IsServerError() bool {
	return false
}

// IsCode returns true when this delete node pool config item forbidden response a status code equal to that given
func (o *DeleteNodePoolConfigItemForbidden) IsCode(code int) bool {
	return code == 403
}

// Code gets the status code for the delete node pool config item forbidden response
func (o *DeleteNodePoolConfigItemForbidden) Code() int {
	return 403
}

func (o *DeleteNodePoolConfigItemForbidden) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemForbidden ", 403)
}

func (o *DeleteNodePoolConfigItemForbidden) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemForbidden ", 403)
}

func (o *DeleteNodePoolConfigItemForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteNodePoolConfigItemNotFound creates a DeleteNodePoolConfigItemNotFound with default headers values
func NewDeleteNodePoolConfigItemNotFound() *DeleteNodePoolConfigItemNotFound {
	return &DeleteNodePoolConfigItemNotFound{}
}

/*
DeleteNodePoolConfigItemNotFound describes a response with status code 404, with default header values.

Config item not found
*/
type DeleteNodePoolConfigItemNotFound struct {
}

// IsSuccess returns true when this delete node pool config item not found response has a 2xx status code
func (o *DeleteNodePoolConfigItemNotFound) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete node pool config item not found response has a 3xx status code
func (o *DeleteNodePoolConfigItemNotFound) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete node pool config item not found response has a 4xx status code
func (o *DeleteNodePoolConfigItemNotFound) IsClientError() bool {
	return true
}

// IsServerError returns true when this delete node pool config item not found response has a 5xx status code
func (o *DeleteNodePoolConfigItemNotFound) IsServerError() bool {
	return false
}

// IsCode returns true when this delete node pool config item not found response a status code equal to that given
func (o *DeleteNodePoolConfigItemNotFound) IsCode(code int) bool {
	return code == 404
}

// Code gets the status code for the delete node pool config item not found response
func (o *DeleteNodePoolConfigItemNotFound) Code() int {
	return 404
}

func (o *DeleteNodePoolConfigItemNotFound) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemNotFound ", 404)
}

func (o *DeleteNodePoolConfigItemNotFound) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemNotFound ", 404)
}

func (o *DeleteNodePoolConfigItemNotFound) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewDeleteNodePoolConfigItemInternalServerError creates a DeleteNodePoolConfigItemInternalServerError with default headers values
func NewDeleteNodePoolConfigItemInternalServerError() *DeleteNodePoolConfigItemInternalServerError {
	return &DeleteNodePoolConfigItemInternalServerError{}
}

/*
DeleteNodePoolConfigItemInternalServerError describes a response with status code 500, with default header values.

Unexpected error
*/
type DeleteNodePoolConfigItemInternalServerError struct {
	Payload *models.Error
}

// IsSuccess returns true when this delete node pool config item internal server error response has a 2xx status code
func (o *DeleteNodePoolConfigItemInternalServerError) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this delete node pool config item internal server error response has a 3xx status code
func (o *DeleteNodePoolConfigItemInternalServerError) IsRedirect() bool {
	return false
}

// IsClientError returns true when this delete node pool config item internal server error response has a 4xx status code
func (o *DeleteNodePoolConfigItemInternalServerError) IsClientError() bool {
	return false
}

// IsServerError returns true when this delete node pool config item internal server error response has a 5xx status code
func (o *DeleteNodePoolConfigItemInternalServerError) IsServerError() bool {
	return true
}

// IsCode returns true when this delete node pool config item internal server error response a status code equal to that given
func (o *DeleteNodePoolConfigItemInternalServerError) IsCode(code int) bool {
	return code == 500
}

// Code gets the status code for the delete node pool config item internal server error response
func (o *DeleteNodePoolConfigItemInternalServerError) Code() int {
	return 500
}

func (o *DeleteNodePoolConfigItemInternalServerError) Error() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemInternalServerError  %+v", 500, o.Payload)
}

func (o *DeleteNodePoolConfigItemInternalServerError) String() string {
	return fmt.Sprintf("[DELETE /kubernetes-clusters/{cluster_id}/node-pools/{node_pool_name}/config-items/{config_key}][%d] deleteNodePoolConfigItemInternalServerError  %+v", 500, o.Payload)
}

func (o *DeleteNodePoolConfigItemInternalServerError) GetPayload() *models.Error {
	return o.Payload
}

func (o *DeleteNodePoolConfigItemInternalServerError) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
