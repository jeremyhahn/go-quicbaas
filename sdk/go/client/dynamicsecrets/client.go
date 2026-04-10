// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package dynamicsecrets

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.DynamicSecrets"
)

// Client provides dynamic secrets management operations.
type Client interface {
	// CreateRole creates a new credential generation role.
	CreateRole(ctx context.Context, req *CreateRoleRequest) (*CreateRoleResponse, error)

	// GetRole retrieves a credential generation role by name.
	GetRole(ctx context.Context, req *GetRoleRequest) (*GetRoleResponse, error)

	// ListRoles returns all credential generation roles.
	ListRoles(ctx context.Context, req *ListRolesRequest) (*ListRolesResponse, error)

	// DeleteRole removes a credential generation role.
	DeleteRole(ctx context.Context, req *DeleteRoleRequest) (*DeleteRoleResponse, error)

	// GenerateCredential generates a new dynamic credential based on a role.
	GenerateCredential(ctx context.Context, req *GenerateCredentialRequest) (*GenerateCredentialResponse, error)

	// RenewLease extends the expiration time of a credential lease.
	RenewLease(ctx context.Context, req *RenewLeaseRequest) (*RenewLeaseResponse, error)

	// RevokeLease revokes a credential lease immediately.
	RevokeLease(ctx context.Context, req *RevokeLeaseRequest) (*RevokeLeaseResponse, error)

	// ListLeases returns all active leases.
	ListLeases(ctx context.Context, req *ListLeasesRequest) (*ListLeasesResponse, error)
}

// Role defines a template for generating credentials.
type Role struct {
	// Name is the unique identifier for this role.
	Name string `json:"name"`

	// GeneratorType identifies the credential generator to use.
	GeneratorType string `json:"generator_type"`

	// Config contains generator-specific configuration.
	Config map[string]string `json:"config"`

	// DefaultTTL is the default time-to-live for generated credentials.
	DefaultTTL time.Duration `json:"default_ttl"`

	// MaxTTL is the maximum allowed time-to-live for credentials.
	MaxTTL time.Duration `json:"max_ttl"`
}

// Credential represents a dynamically generated credential.
type Credential struct {
	// CredentialID is the unique identifier for this credential.
	CredentialID string `json:"credential_id"`

	// Type identifies the credential type.
	Type string `json:"type"`

	// Data contains the actual credential information.
	Data map[string]string `json:"data"`

	// LeaseID is the ID of the lease associated with this credential.
	LeaseID string `json:"lease_id"`

	// CreatedAt is the timestamp when the credential was generated.
	CreatedAt time.Time `json:"created_at"`

	// ExpiresAt is the timestamp when the credential expires.
	ExpiresAt time.Time `json:"expires_at"`
}

// Lease represents a time-limited grant for a credential.
type Lease struct {
	// LeaseID is the unique identifier for this lease.
	LeaseID string `json:"lease_id"`

	// SecretPath is the path to the secret or resource.
	SecretPath string `json:"secret_path"`

	// IssuedAt is the timestamp when the lease was created.
	IssuedAt time.Time `json:"issued_at"`

	// ExpiresAt is the timestamp when the lease will expire.
	ExpiresAt time.Time `json:"expires_at"`

	// TTL is the time-to-live duration for this lease.
	TTL time.Duration `json:"ttl"`

	// RenewCount tracks how many times this lease has been renewed.
	RenewCount int `json:"renew_count"`

	// MaxRenewals is the maximum number of times this lease can be renewed.
	MaxRenewals int `json:"max_renewals"`

	// Renewable indicates whether this lease can be renewed.
	Renewable bool `json:"renewable"`

	// RevokedAt is the timestamp when the lease was revoked.
	RevokedAt *time.Time `json:"revoked_at,omitempty"`

	// Metadata contains additional key-value data about this lease.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// CreateRoleRequest contains parameters for creating a credential role.
type CreateRoleRequest struct {
	// Name is the unique identifier for the role.
	Name string `json:"name"`

	// GeneratorType identifies the credential generator.
	GeneratorType string `json:"generator_type"`

	// Config contains generator-specific configuration.
	Config map[string]string `json:"config"`

	// DefaultTTL is the default time-to-live for generated credentials.
	DefaultTTL time.Duration `json:"default_ttl"`

	// MaxTTL is the maximum allowed time-to-live for credentials.
	MaxTTL time.Duration `json:"max_ttl"`
}

// CreateRoleResponse contains the result of creating a role.
type CreateRoleResponse struct {
	// Role is the created role.
	Role *Role `json:"role"`

	// CreatedAt is the timestamp when the role was created.
	CreatedAt time.Time `json:"created_at"`
}

// GetRoleRequest contains parameters for retrieving a role.
type GetRoleRequest struct {
	// Name is the role name to retrieve.
	Name string `json:"name"`
}

// GetRoleResponse contains the retrieved role.
type GetRoleResponse struct {
	// Role is the retrieved role.
	Role *Role `json:"role"`
}

// ListRolesRequest contains parameters for listing roles.
type ListRolesRequest struct {
	// Prefix filters roles by name prefix.
	Prefix string `json:"prefix,omitempty"`
}

// ListRolesResponse contains the list of roles.
type ListRolesResponse struct {
	// Roles is the list of roles.
	Roles []*Role `json:"roles"`

	// Count is the total number of roles.
	Count int `json:"count"`
}

// DeleteRoleRequest contains parameters for deleting a role.
type DeleteRoleRequest struct {
	// Name is the role name to delete.
	Name string `json:"name"`
}

// DeleteRoleResponse contains the result of deleting a role.
type DeleteRoleResponse struct {
	// DeletedAt is the timestamp when the role was deleted.
	DeletedAt time.Time `json:"deleted_at"`
}

// GenerateCredentialRequest contains parameters for generating a credential.
type GenerateCredentialRequest struct {
	// RoleName is the name of the role to use for generation.
	RoleName string `json:"role_name"`

	// TTL is the requested time-to-live for the credential.
	TTL time.Duration `json:"ttl,omitempty"`
}

// GenerateCredentialResponse contains the generated credential and lease.
type GenerateCredentialResponse struct {
	// Credential is the generated credential.
	Credential *Credential `json:"credential"`

	// Lease is the associated lease.
	Lease *Lease `json:"lease"`

	// LeaseID is the lease identifier.
	LeaseID string `json:"lease_id"`
}

// RenewLeaseRequest contains parameters for renewing a lease.
type RenewLeaseRequest struct {
	// LeaseID is the ID of the lease to renew.
	LeaseID string `json:"lease_id"`

	// Increment is the duration to add to the expiration time.
	Increment time.Duration `json:"increment,omitempty"`
}

// RenewLeaseResponse contains the renewed lease.
type RenewLeaseResponse struct {
	// Lease is the renewed lease.
	Lease *Lease `json:"lease"`

	// RenewedAt is the timestamp when the lease was renewed.
	RenewedAt time.Time `json:"renewed_at"`
}

// RevokeLeaseRequest contains parameters for revoking a lease.
type RevokeLeaseRequest struct {
	// LeaseID is the ID of the lease to revoke.
	LeaseID string `json:"lease_id"`
}

// RevokeLeaseResponse contains the result of revoking a lease.
type RevokeLeaseResponse struct {
	// RevokedAt is the timestamp when the lease was revoked.
	RevokedAt time.Time `json:"revoked_at"`
}

// ListLeasesRequest contains parameters for listing leases.
type ListLeasesRequest struct {
	// Prefix filters leases by path prefix.
	Prefix string `json:"prefix,omitempty"`
}

// ListLeasesResponse contains the list of leases.
type ListLeasesResponse struct {
	// Leases is the list of leases.
	Leases []*Lease `json:"leases"`

	// Count is the total number of leases.
	Count int `json:"count"`
}

// client is the concrete implementation of Client.
type client struct {
	transport transport.Transport
}

// NewClient creates a new dynamic secrets client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// CreateRole creates a new credential generation role.
func (c *client) CreateRole(ctx context.Context, req *CreateRoleRequest) (*CreateRoleResponse, error) {
	if err := validateCreateRoleRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.CreateRole", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateRole", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dynamicsecrets.CreateRole", "nil response from server")
	}

	createResp, ok := resp.(*CreateRoleResponse)
	if !ok {
		return nil, errors.NewInternal("dynamicsecrets.CreateRole", "invalid response type")
	}

	return createResp, nil
}

// GetRole retrieves a credential generation role by name.
func (c *client) GetRole(ctx context.Context, req *GetRoleRequest) (*GetRoleResponse, error) {
	if err := validateGetRoleRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.GetRole", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetRole", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("dynamicsecrets.GetRole", "role not found")
	}

	getResp, ok := resp.(*GetRoleResponse)
	if !ok {
		return nil, errors.NewInternal("dynamicsecrets.GetRole", "invalid response type")
	}

	return getResp, nil
}

// ListRoles returns all credential generation roles.
func (c *client) ListRoles(ctx context.Context, req *ListRolesRequest) (*ListRolesResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.ListRoles", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.ListRoles", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListRoles", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListRolesResponse{Roles: []*Role{}, Count: 0}, nil
	}

	listResp, ok := resp.(*ListRolesResponse)
	if !ok {
		return nil, errors.NewInternal("dynamicsecrets.ListRoles", "invalid response type")
	}

	return listResp, nil
}

// DeleteRole removes a credential generation role.
func (c *client) DeleteRole(ctx context.Context, req *DeleteRoleRequest) (*DeleteRoleResponse, error) {
	if err := validateDeleteRoleRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.DeleteRole", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteRole", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dynamicsecrets.DeleteRole", "nil response from server")
	}

	deleteResp, ok := resp.(*DeleteRoleResponse)
	if !ok {
		return nil, errors.NewInternal("dynamicsecrets.DeleteRole", "invalid response type")
	}

	return deleteResp, nil
}

// GenerateCredential generates a new dynamic credential based on a role.
func (c *client) GenerateCredential(ctx context.Context, req *GenerateCredentialRequest) (*GenerateCredentialResponse, error) {
	if err := validateGenerateCredentialRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.GenerateCredential", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GenerateCredential", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dynamicsecrets.GenerateCredential", "nil response from server")
	}

	genResp, ok := resp.(*GenerateCredentialResponse)
	if !ok {
		return nil, errors.NewInternal("dynamicsecrets.GenerateCredential", "invalid response type")
	}

	return genResp, nil
}

// RenewLease extends the expiration time of a credential lease.
func (c *client) RenewLease(ctx context.Context, req *RenewLeaseRequest) (*RenewLeaseResponse, error) {
	if err := validateRenewLeaseRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.RenewLease", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RenewLease", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dynamicsecrets.RenewLease", "nil response from server")
	}

	renewResp, ok := resp.(*RenewLeaseResponse)
	if !ok {
		return nil, errors.NewInternal("dynamicsecrets.RenewLease", "invalid response type")
	}

	return renewResp, nil
}

// RevokeLease revokes a credential lease immediately.
func (c *client) RevokeLease(ctx context.Context, req *RevokeLeaseRequest) (*RevokeLeaseResponse, error) {
	if err := validateRevokeLeaseRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.RevokeLease", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RevokeLease", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dynamicsecrets.RevokeLease", "nil response from server")
	}

	revokeResp, ok := resp.(*RevokeLeaseResponse)
	if !ok {
		return nil, errors.NewInternal("dynamicsecrets.RevokeLease", "invalid response type")
	}

	return revokeResp, nil
}

// ListLeases returns all active leases.
func (c *client) ListLeases(ctx context.Context, req *ListLeasesRequest) (*ListLeasesResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.ListLeases", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("dynamicsecrets.ListLeases", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListLeases", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListLeasesResponse{Leases: []*Lease{}, Count: 0}, nil
	}

	listResp, ok := resp.(*ListLeasesResponse)
	if !ok {
		return nil, errors.NewInternal("dynamicsecrets.ListLeases", "invalid response type")
	}

	return listResp, nil
}

// Validation functions

func validateCreateRoleRequest(req *CreateRoleRequest) error {
	if req == nil {
		return errors.NewInvalidInput("dynamicsecrets.CreateRole", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("dynamicsecrets.CreateRole", "name is required")
	}
	if req.GeneratorType == "" {
		return errors.NewInvalidInput("dynamicsecrets.CreateRole", "generator_type is required")
	}
	if req.DefaultTTL <= 0 {
		return errors.NewInvalidInput("dynamicsecrets.CreateRole", "default_ttl must be positive")
	}
	if req.MaxTTL <= 0 {
		return errors.NewInvalidInput("dynamicsecrets.CreateRole", "max_ttl must be positive")
	}
	if req.DefaultTTL > req.MaxTTL {
		return errors.NewInvalidInput("dynamicsecrets.CreateRole", "default_ttl cannot exceed max_ttl")
	}
	return nil
}

func validateGetRoleRequest(req *GetRoleRequest) error {
	if req == nil {
		return errors.NewInvalidInput("dynamicsecrets.GetRole", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("dynamicsecrets.GetRole", "name is required")
	}
	return nil
}

func validateDeleteRoleRequest(req *DeleteRoleRequest) error {
	if req == nil {
		return errors.NewInvalidInput("dynamicsecrets.DeleteRole", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("dynamicsecrets.DeleteRole", "name is required")
	}
	return nil
}

func validateGenerateCredentialRequest(req *GenerateCredentialRequest) error {
	if req == nil {
		return errors.NewInvalidInput("dynamicsecrets.GenerateCredential", "request is required")
	}
	if req.RoleName == "" {
		return errors.NewInvalidInput("dynamicsecrets.GenerateCredential", "role_name is required")
	}
	if req.TTL < 0 {
		return errors.NewInvalidInput("dynamicsecrets.GenerateCredential", "ttl cannot be negative")
	}
	return nil
}

func validateRenewLeaseRequest(req *RenewLeaseRequest) error {
	if req == nil {
		return errors.NewInvalidInput("dynamicsecrets.RenewLease", "request is required")
	}
	if req.LeaseID == "" {
		return errors.NewInvalidInput("dynamicsecrets.RenewLease", "lease_id is required")
	}
	if req.Increment < 0 {
		return errors.NewInvalidInput("dynamicsecrets.RenewLease", "increment cannot be negative")
	}
	return nil
}

func validateRevokeLeaseRequest(req *RevokeLeaseRequest) error {
	if req == nil {
		return errors.NewInvalidInput("dynamicsecrets.RevokeLease", "request is required")
	}
	if req.LeaseID == "" {
		return errors.NewInvalidInput("dynamicsecrets.RevokeLease", "lease_id is required")
	}
	return nil
}
