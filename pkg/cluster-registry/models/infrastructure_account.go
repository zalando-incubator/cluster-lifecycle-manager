// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/go-openapi/validate"
)

// InfrastructureAccount infrastructure account
//
// swagger:model InfrastructureAccount
type InfrastructureAccount struct {

	// Cost center of the Owner/infrastructure account
	// Example: 0000001234
	// Required: true
	CostCenter *string `json:"cost_center"`

	// Level of criticality as defined by tech controlling. 1 is non critical, 2 is standard production, 3 is PCI
	// Example: 2
	// Required: true
	CriticalityLevel *int32 `json:"criticality_level"`

	// Environment. possible values are "production" or "test".
	// Example: production
	// Required: true
	Environment *string `json:"environment"`

	// The external identifier of the account (i.e. AWS account ID)
	// Example: 123456789012
	// Required: true
	ExternalID *string `json:"external_id"`

	// Globally unique ID of the infrastructure account.
	// Example: aws:123456789012
	// Required: true
	ID *string `json:"id"`

	// Lifecycle Status is used to describe the current status of the account.
	// Required: true
	// Enum: ["requested","creating","ready","decommissioned"]
	LifecycleStatus *string `json:"lifecycle_status"`

	// Name of the infrastructure account
	// Example: foo
	// Required: true
	Name *string `json:"name"`

	// Owner of the infrastructure account (references an object in the organization service)
	// Example: team/bar
	// Required: true
	Owner *string `json:"owner"`

	// Type of the infrastructure account. Possible types are "aws", "gcp", "dc".
	// Example: aws
	// Required: true
	Type *string `json:"type"`
}

// Validate validates this infrastructure account
func (m *InfrastructureAccount) Validate(formats strfmt.Registry) error {
	var res []error

	if err := m.validateCostCenter(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateCriticalityLevel(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateEnvironment(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateExternalID(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateID(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateLifecycleStatus(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateName(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateOwner(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateType(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *InfrastructureAccount) validateCostCenter(formats strfmt.Registry) error {

	if err := validate.Required("cost_center", "body", m.CostCenter); err != nil {
		return err
	}

	return nil
}

func (m *InfrastructureAccount) validateCriticalityLevel(formats strfmt.Registry) error {

	if err := validate.Required("criticality_level", "body", m.CriticalityLevel); err != nil {
		return err
	}

	return nil
}

func (m *InfrastructureAccount) validateEnvironment(formats strfmt.Registry) error {

	if err := validate.Required("environment", "body", m.Environment); err != nil {
		return err
	}

	return nil
}

func (m *InfrastructureAccount) validateExternalID(formats strfmt.Registry) error {

	if err := validate.Required("external_id", "body", m.ExternalID); err != nil {
		return err
	}

	return nil
}

func (m *InfrastructureAccount) validateID(formats strfmt.Registry) error {

	if err := validate.Required("id", "body", m.ID); err != nil {
		return err
	}

	return nil
}

var infrastructureAccountTypeLifecycleStatusPropEnum []interface{}

func init() {
	var res []string
	if err := json.Unmarshal([]byte(`["requested","creating","ready","decommissioned"]`), &res); err != nil {
		panic(err)
	}
	for _, v := range res {
		infrastructureAccountTypeLifecycleStatusPropEnum = append(infrastructureAccountTypeLifecycleStatusPropEnum, v)
	}
}

const (

	// InfrastructureAccountLifecycleStatusRequested captures enum value "requested"
	InfrastructureAccountLifecycleStatusRequested string = "requested"

	// InfrastructureAccountLifecycleStatusCreating captures enum value "creating"
	InfrastructureAccountLifecycleStatusCreating string = "creating"

	// InfrastructureAccountLifecycleStatusReady captures enum value "ready"
	InfrastructureAccountLifecycleStatusReady string = "ready"

	// InfrastructureAccountLifecycleStatusDecommissioned captures enum value "decommissioned"
	InfrastructureAccountLifecycleStatusDecommissioned string = "decommissioned"
)

// prop value enum
func (m *InfrastructureAccount) validateLifecycleStatusEnum(path, location string, value string) error {
	if err := validate.EnumCase(path, location, value, infrastructureAccountTypeLifecycleStatusPropEnum, true); err != nil {
		return err
	}
	return nil
}

func (m *InfrastructureAccount) validateLifecycleStatus(formats strfmt.Registry) error {

	if err := validate.Required("lifecycle_status", "body", m.LifecycleStatus); err != nil {
		return err
	}

	// value enum
	if err := m.validateLifecycleStatusEnum("lifecycle_status", "body", *m.LifecycleStatus); err != nil {
		return err
	}

	return nil
}

func (m *InfrastructureAccount) validateName(formats strfmt.Registry) error {

	if err := validate.Required("name", "body", m.Name); err != nil {
		return err
	}

	return nil
}

func (m *InfrastructureAccount) validateOwner(formats strfmt.Registry) error {

	if err := validate.Required("owner", "body", m.Owner); err != nil {
		return err
	}

	return nil
}

func (m *InfrastructureAccount) validateType(formats strfmt.Registry) error {

	if err := validate.Required("type", "body", m.Type); err != nil {
		return err
	}

	return nil
}

// ContextValidate validates this infrastructure account based on context it is used
func (m *InfrastructureAccount) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	return nil
}

// MarshalBinary interface implementation
func (m *InfrastructureAccount) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *InfrastructureAccount) UnmarshalBinary(b []byte) error {
	var res InfrastructureAccount
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
