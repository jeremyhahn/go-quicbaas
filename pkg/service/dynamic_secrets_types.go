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

// Package service provides the unified service facade for QuicBaaS.
package service

import (
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
)

// CredentialRoleCreateRequest contains parameters for creating a credential role.
type CredentialRoleCreateRequest struct {
	// Name is the unique identifier for the role.
	Name string `json:"name"`

	// GeneratorType identifies the credential generator (e.g., "postgres", "mysql").
	GeneratorType string `json:"generator_type"`

	// Config contains generator-specific configuration.
	Config map[string]string `json:"config"`

	// DefaultTTL is the default time-to-live for generated credentials.
	DefaultTTL time.Duration `json:"default_ttl"`

	// MaxTTL is the maximum allowed time-to-live for credentials.
	MaxTTL time.Duration `json:"max_ttl"`
}

// CreateCredentialRoleRequest is an alias for CredentialRoleCreateRequest.
type CreateCredentialRoleRequest = CredentialRoleCreateRequest

// CreateRoleRequest is an alias for DynamicSecretsService interface.
type CreateRoleRequest = CredentialRoleCreateRequest

// CredentialRoleCreateResponse contains the result of creating a role.
type CredentialRoleCreateResponse struct {
	// Role is the created role.
	Role *credentials.Role `json:"role"`

	// CreatedAt is the timestamp when the role was created.
	CreatedAt time.Time `json:"created_at"`
}

// CreateCredentialRoleResponse is an alias for CredentialRoleCreateResponse.
type CreateCredentialRoleResponse = CredentialRoleCreateResponse

// CreateRoleResponse is an alias for backwards compatibility with DynamicSecretsService interface.
type CreateRoleResponse = CredentialRoleCreateResponse

// CredentialRoleGetRequest contains parameters for retrieving a role.
type CredentialRoleGetRequest struct {
	// Name is the role name to retrieve.
	Name string `json:"name"`
}

// GetCredentialRoleRequest is an alias for CredentialRoleGetRequest.
type GetCredentialRoleRequest = CredentialRoleGetRequest

// GetRoleRequest is an alias for backwards compatibility with DynamicSecretsService interface.
type GetRoleRequest = CredentialRoleGetRequest

// CredentialRoleGetResponse contains the retrieved role.
type CredentialRoleGetResponse struct {
	// Role is the retrieved role.
	Role *credentials.Role `json:"role"`
}

// GetCredentialRoleResponse is an alias for CredentialRoleGetResponse.
type GetCredentialRoleResponse = CredentialRoleGetResponse

// GetRoleResponse is an alias for backwards compatibility with DynamicSecretsService interface.
type GetRoleResponse = CredentialRoleGetResponse

// CredentialRolesListRequest contains parameters for listing roles.
type CredentialRolesListRequest struct {
	// Prefix filters roles by name prefix (optional).
	Prefix string `json:"prefix,omitempty"`
}

// ListCredentialRolesRequest is an alias for CredentialRolesListRequest.
type ListCredentialRolesRequest = CredentialRolesListRequest

// ListRolesRequest is an alias for backwards compatibility with DynamicSecretsService interface.
type ListRolesRequest = CredentialRolesListRequest

// CredentialRolesListResponse contains the list of roles.
type CredentialRolesListResponse struct {
	// Roles is the list of roles.
	Roles []*credentials.Role `json:"roles"`

	// Count is the total number of roles.
	Count int `json:"count"`
}

// ListCredentialRolesResponse is an alias for CredentialRolesListResponse.
type ListCredentialRolesResponse = CredentialRolesListResponse

// ListRolesResponse is an alias for backwards compatibility with DynamicSecretsService interface.
type ListRolesResponse = CredentialRolesListResponse

// CredentialRoleDeleteRequest contains parameters for deleting a role.
type CredentialRoleDeleteRequest struct {
	// Name is the role name to delete.
	Name string `json:"name"`
}

// DeleteCredentialRoleRequest is an alias for CredentialRoleDeleteRequest.
type DeleteCredentialRoleRequest = CredentialRoleDeleteRequest

// DeleteRoleRequest is an alias for backwards compatibility with DynamicSecretsService interface.
type DeleteRoleRequest = CredentialRoleDeleteRequest

// CredentialRoleDeleteResponse contains the result of deleting a role.
type CredentialRoleDeleteResponse struct {
	// DeletedAt is the timestamp when the role was deleted.
	DeletedAt time.Time `json:"deleted_at"`
}

// DeleteCredentialRoleResponse is an alias for CredentialRoleDeleteResponse.
type DeleteCredentialRoleResponse = CredentialRoleDeleteResponse

// DeleteRoleResponse is an alias for backwards compatibility with DynamicSecretsService interface.
type DeleteRoleResponse = CredentialRoleDeleteResponse

// GenerateCredentialRequest contains parameters for generating a credential.
type GenerateCredentialRequest struct {
	// RoleName is the name of the role to use for generation.
	RoleName string `json:"role_name"`

	// TTL is the requested time-to-live for the credential.
	// If not specified, the role's DefaultTTL is used.
	TTL time.Duration `json:"ttl,omitempty"`
}

// GenerateCredentialResponse contains the generated credential and lease.
type GenerateCredentialResponse struct {
	// Credential is the generated credential.
	Credential *credentials.Credential `json:"credential"`

	// Lease is the associated lease.
	Lease *lease.Lease `json:"lease"`

	// LeaseID is the lease identifier.
	LeaseID string `json:"lease_id"`
}

// RenewLeaseRequest contains parameters for renewing a lease.
type RenewLeaseRequest struct {
	// LeaseID is the ID of the lease to renew.
	LeaseID string `json:"lease_id"`

	// Increment is the duration to add to the expiration time.
	// If not specified, the original TTL is used.
	Increment time.Duration `json:"increment,omitempty"`
}

// RenewLeaseResponse contains the renewed lease.
type RenewLeaseResponse struct {
	// Lease is the renewed lease.
	Lease *lease.Lease `json:"lease"`

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
	// Prefix filters leases by path prefix (optional).
	Prefix string `json:"prefix,omitempty"`
}

// ListLeasesResponse contains the list of active leases.
type ListLeasesResponse struct {
	// Leases is the list of active leases: expired and revoked leases are excluded.
	Leases []*lease.Lease `json:"leases"`

	// Count is the number of active leases.
	Count int `json:"count"`
}
