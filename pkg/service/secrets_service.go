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

// =============================================================================
// Secrets Service - Secret Storage and Rotation
// =============================================================================

// SecretsService handles secret storage and retrieval with versioning and rotation.
// Secrets are encrypted at rest and support automatic rotation policies.
type SecretsService interface {
	// StoreSecret stores a secret with optional metadata and encryption.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Store secret request with name, data, metadata, rotation policy
	// Returns:
	//   - StoreSecretResponse: Contains secret ID, version, timestamp
	//   - error: ErrInvalidInput, ErrAlreadyExists, ErrUnauthorized, ErrTimeout
	StoreSecret(ctx context.Context, req *StoreSecretRequest) (*StoreSecretResponse, error)

	// EnsureSecret is the Vault-style generate-on-first-access
	// primitive. On the first call for a given path the server
	// generates a fresh value per the request spec and stores it,
	// returning Generated=true. On every subsequent call it returns
	// the same stable bytes with Generated=false. Concurrent callers
	// racing on the same path are serialized via a distributed lock
	// so only one generate-and-store executes.
	//
	// Spec mismatch between the request and a previously stored
	// value is a hard error. EnsureSecret never silently rotates.
	//
	// This primitive replaces the old autogen:// resolver pattern
	// that regenerated new random bytes on every boot and made it
	// impossible to bind a persistent secret to a config field.
	//
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Ensure secret request with path and spec
	// Returns:
	//   - EnsureSecretResponse: Contains data, generated flag, version, timestamp
	//   - error: ErrInvalidInput, ErrSecretSpecMismatch, ErrUnauthorized, ErrTimeout
	EnsureSecret(ctx context.Context, req *EnsureSecretRequest) (*EnsureSecretResponse, error)

	// GetSecret retrieves the latest version of a secret.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get secret request with secret name or ID
	// Returns:
	//   - GetSecretResponse: Contains secret data, version, metadata, timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetSecret(ctx context.Context, req *GetSecretRequest) (*GetSecretResponse, error)

	// ListSecrets returns all secrets accessible to the authenticated user.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List secrets request with optional filters and pagination
	// Returns:
	//   - ListSecretsResponse: Contains secret metadata list (no secret data)
	//   - error: ErrUnauthorized, ErrTimeout
	ListSecrets(ctx context.Context, req *ListSecretsRequest) (*ListSecretsResponse, error)

	// DeleteSecret permanently removes a secret and all its versions.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete secret request with secret name or ID
	// Returns:
	//   - DeleteSecretResponse: Contains deletion status and timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteSecret(ctx context.Context, req *DeleteSecretRequest) (*DeleteSecretResponse, error)

	// GetSecretVersion retrieves a specific version of a secret.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get secret version request with secret name/ID and version
	// Returns:
	//   - GetSecretVersionResponse: Contains secret data, version, metadata
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetSecretVersion(ctx context.Context, req *GetSecretVersionRequest) (*GetSecretVersionResponse, error)

	// ListSecretVersions returns all versions of a secret.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List secret versions request with secret name or ID
	// Returns:
	//   - ListSecretVersionsResponse: Contains version metadata list
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	ListSecretVersions(ctx context.Context, req *ListSecretVersionsRequest) (*ListSecretVersionsResponse, error)

	// RollbackSecret rolls back to a previous version of a secret.
	// Creates a new version with the content from the specified version.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Rollback secret request with secret name/ID and target version
	// Returns:
	//   - RollbackSecretResponse: Contains new version, timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	RollbackSecret(ctx context.Context, req *RollbackSecretRequest) (*RollbackSecretResponse, error)

	// RotateSecret manually rotates a secret to a new value.
	// Creates a new version and optionally archives old versions.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Rotate secret request with secret name/ID and new data
	// Returns:
	//   - RotateSecretResponse: Contains new version, timestamp
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	RotateSecret(ctx context.Context, req *RotateSecretRequest) (*RotateSecretResponse, error)

	// SetRotationPolicy sets or updates the automatic rotation policy.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Set rotation policy request with secret name/ID and policy
	// Returns:
	//   - SetRotationPolicyResponse: Contains updated policy and timestamp
	//   - error: ErrNotFound, ErrInvalidInput, ErrUnauthorized, ErrTimeout
	SetRotationPolicy(ctx context.Context, req *SetRotationPolicyRequest) (*SetRotationPolicyResponse, error)

	// GetRotationPolicy retrieves the current rotation policy for a secret.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get rotation policy request with secret name or ID
	// Returns:
	//   - GetRotationPolicyResponse: Contains policy details and status
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	GetRotationPolicy(ctx context.Context, req *GetRotationPolicyRequest) (*GetRotationPolicyResponse, error)

	// UpdateSecretStatus enables or disables a secret.
	// Disabled secrets cannot be accessed until re-enabled.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update status request with secret ID and new status
	// Returns:
	//   - UpdateSecretStatusResponse: Contains old and new status, timestamp
	//   - error: ErrNotFound, ErrInvalidInput, ErrUnauthorized, ErrTimeout
	UpdateSecretStatus(ctx context.Context, req *UpdateSecretStatusRequest) (*UpdateSecretStatusResponse, error)

	// GetSecretsByPrefix fetches all secrets matching a path prefix.
	// This is used for application secrets where all secrets under a path are needed at once.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Request with prefix path (e.g., "/apps/myapp/")
	// Returns:
	//   - GetSecretsByPrefixResponse: Contains all secrets with data under the prefix
	//   - error: ErrUnauthorized, ErrTimeout
	GetSecretsByPrefix(ctx context.Context, req *GetSecretsByPrefixRequest) (*GetSecretsByPrefixResponse, error)

	// WatchSecretsByPrefix streams secret changes for a prefix.
	// Used by agents and sidecars to receive real-time secret updates.
	// Parameters:
	//   - ctx: Request context (cancelled to stop watching)
	//   - req: Request with prefix path to watch
	// Returns:
	//   - Channel of SecretChange events
	//   - error: ErrUnauthorized, ErrTimeout
	WatchSecretsByPrefix(ctx context.Context, req *WatchSecretsRequest) (<-chan *SecretChange, error)
}
