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

package security

import (
	"context"
	"time"
)

// TenantDEKManager manages per-tenant Data Encryption Keys (DEKs).
// Each tenant receives an isolated DEK derived from the cluster-level
// master encryption key, providing cryptographic isolation between tenants.
type TenantDEKManager interface {
	// GetOrCreateTenantDEK returns the current DEK for the tenant,
	// creating one if it does not yet exist.
	GetOrCreateTenantDEK(ctx context.Context, tenantID string) ([]byte, error)

	// GetTenantDEK returns the current DEK for the tenant.
	// Returns an error if the tenant DEK does not exist.
	GetTenantDEK(ctx context.Context, tenantID string) ([]byte, error)

	// RotateTenantDEK generates a new DEK for the specified tenant,
	// replacing the current active key.
	RotateTenantDEK(ctx context.Context, tenantID string) error

	// DeleteTenantDEK removes the DEK for the specified tenant.
	DeleteTenantDEK(ctx context.Context, tenantID string) error

	// GetTenantDEKStatus returns metadata about a tenant's DEK.
	GetTenantDEKStatus(ctx context.Context, tenantID string) (*TenantDEKStatus, error)

	// ListTenantDEKs returns the tenant IDs that have active DEKs.
	ListTenantDEKs(ctx context.Context) ([]string, error)

	// Close releases resources held by the manager.
	Close() error
}

// TenantDEKStatus contains metadata about a tenant's DEK.
type TenantDEKStatus struct {
	// TenantID is the unique identifier of the tenant.
	TenantID string

	// Version is the current DEK version (incremented on rotation).
	Version uint64

	// Algorithm is the encryption algorithm used (e.g. "AES-256-GCM").
	Algorithm string

	// KeyBytes is the key length in bytes.
	KeyBytes int

	// CreatedAt is the time the DEK was first created.
	CreatedAt time.Time

	// RotatedAt is the time the DEK was last rotated.
	RotatedAt time.Time
}
