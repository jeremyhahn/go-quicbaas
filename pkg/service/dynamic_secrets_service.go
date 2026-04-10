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

import "context"

// DynamicSecretsService handles dynamic credential generation with lease-based TTL management.
// Dynamic secrets are auto-generated, time-limited credentials that are automatically revoked on expiry.
type DynamicSecretsService interface {
	// CreateRole creates a new credential generation role.
	// Roles define how credentials are generated, what permissions they have, and their TTL limits.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Create role request with name, generator type, config, and TTL settings
	// Returns:
	//   - CreateRoleResponse: Contains the created role and timestamp
	//   - error: ErrInvalidInput, ErrAlreadyExists, ErrUnauthorized, ErrTimeout
	CreateRole(ctx context.Context, req *CreateRoleRequest) (*CreateRoleResponse, error)

	// GetRole retrieves a credential generation role by name.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get role request with role name
	// Returns:
	//   - GetRoleResponse: Contains the role configuration
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetRole(ctx context.Context, req *GetRoleRequest) (*GetRoleResponse, error)

	// ListRoles returns all credential generation roles.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List roles request with optional prefix filter
	// Returns:
	//   - ListRolesResponse: Contains the list of roles and count
	//   - error: ErrUnauthorized, ErrTimeout
	ListRoles(ctx context.Context, req *ListRolesRequest) (*ListRolesResponse, error)

	// DeleteRole removes a credential generation role.
	// This does not revoke existing credentials generated from this role.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete role request with role name
	// Returns:
	//   - DeleteRoleResponse: Contains deletion timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteRole(ctx context.Context, req *DeleteRoleRequest) (*DeleteRoleResponse, error)

	// GenerateCredential generates a new dynamic credential based on a role.
	// The credential is created in the backend system (e.g., database user created)
	// and a lease is issued to track its lifecycle.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Generate credential request with role name and optional TTL
	// Returns:
	//   - GenerateCredentialResponse: Contains the credential, lease, and lease ID
	//   - error: ErrNotFound (role), ErrInternal, ErrUnauthorized, ErrTimeout
	GenerateCredential(ctx context.Context, req *GenerateCredentialRequest) (*GenerateCredentialResponse, error)

	// RenewLease extends the expiration time of a credential lease.
	// The credential remains valid for the extended period.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Renew lease request with lease ID and optional increment
	// Returns:
	//   - RenewLeaseResponse: Contains the renewed lease and timestamp
	//   - error: ErrNotFound, ErrNotAllowed (if not renewable), ErrTimeout
	RenewLease(ctx context.Context, req *RenewLeaseRequest) (*RenewLeaseResponse, error)

	// RevokeLease revokes a credential lease immediately.
	// This removes the credential from the backend system (e.g., drops database user).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Revoke lease request with lease ID
	// Returns:
	//   - RevokeLeaseResponse: Contains revocation timestamp
	//   - error: ErrNotFound, ErrInternal, ErrTimeout
	RevokeLease(ctx context.Context, req *RevokeLeaseRequest) (*RevokeLeaseResponse, error)

	// ListLeases returns all active leases.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List leases request with optional path prefix filter
	// Returns:
	//   - ListLeasesResponse: Contains the list of leases and count
	//   - error: ErrUnauthorized, ErrTimeout
	ListLeases(ctx context.Context, req *ListLeasesRequest) (*ListLeasesResponse, error)
}
