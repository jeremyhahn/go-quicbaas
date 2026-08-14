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
// This file implements multi-tenant embedding isolation. When a tenantID is
// present in the request context, embedding cache keys are scoped to that
// tenant's namespace to prevent cross-tenant cache poisoning. When tenantID
// is empty, the system operates in single-tenant mode with no behavioral
// changes (full backward compatibility).
package service

import (
	"context"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// embeddingTenantKeyPrefix is the storage prefix for tenant-scoped embedding cache keys.
const embeddingTenantKeyPrefix = "emb-tenant/"

// embeddingTenantKeySeparator separates the tenant ID from the cache key.
const embeddingTenantKeySeparator = "/"

// EmbeddingTenantValidationError indicates a tenant ID validation failure
// in the embedding service.
type EmbeddingTenantValidationError struct {
	TenantID string
	Reason   string
}

// Error implements the error interface.
func (e *EmbeddingTenantValidationError) Error() string {
	return "embedding tenant validation failed: tenant=" + e.TenantID + " reason=" + e.Reason
}

// EmbeddingCrossTenantAccessError indicates a cross-tenant access attempt
// in the embedding service.
type EmbeddingCrossTenantAccessError struct {
	RequestingTenant string
	OwnerTenant      string
	Operation        string
}

// Error implements the error interface.
func (e *EmbeddingCrossTenantAccessError) Error() string {
	return "embedding cross-tenant access denied: requesting=" + e.RequestingTenant +
		" owner=" + e.OwnerTenant + " op=" + e.Operation
}

// embeddingTenantScopedCacheKey returns a tenant-scoped cache key for embeddings.
// For single-tenant mode (empty tenantID), the text is returned as-is.
// For multi-tenant mode, the key is prefixed: "emb-tenant/{tenantID}/{text}".
func embeddingTenantScopedCacheKey(tenantID, text string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return text
	}
	return embeddingTenantKeyPrefix + tenantID + embeddingTenantKeySeparator + text
}

// embeddingTenantStripCacheKey removes the tenant prefix from a cache key.
// If the key doesn't have a tenant prefix, it is returned as-is.
func embeddingTenantStripCacheKey(tenantID, key string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return key
	}
	prefix := embeddingTenantKeyPrefix + tenantID + embeddingTenantKeySeparator
	return strings.TrimPrefix(key, prefix)
}

// embeddingCacheKeyBelongsToTenant checks if a cache key belongs to the given tenant.
// For single-tenant mode (empty tenantID), all keys belong.
// For multi-tenant mode, the key must have the matching tenant prefix.
func embeddingCacheKeyBelongsToTenant(tenantID, key string) bool {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return true
	}
	prefix := embeddingTenantKeyPrefix + tenantID + embeddingTenantKeySeparator
	return strings.HasPrefix(key, prefix)
}

// prepareEmbeddingTenantContext extracts and validates the tenant ID from the
// request context for embedding operations. Returns the validated tenant ID
// or an error if the tenant ID is invalid.
func prepareEmbeddingTenantContext(ctx context.Context) (string, error) {
	tenantID := iam.TenantFromContext(ctx)
	if err := validateEmbeddingTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}

// validateEmbeddingTenantID validates that a tenant ID contains only safe
// characters for use in embedding cache keys.
// Empty tenant ID is valid (single-tenant mode).
func validateEmbeddingTenantID(tenantID string) error {
	if tenantID == "" {
		return nil
	}
	if len(tenantID) > 128 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "embedding.validateTenantID",
			Err: &EmbeddingTenantValidationError{
				TenantID: tenantID,
				Reason:   "tenant ID exceeds maximum length of 128 characters",
			},
		}
	}
	for _, ch := range tenantID {
		if !isValidTenantChar(ch) {
			return &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "embedding.validateTenantID",
				Err: &EmbeddingTenantValidationError{
					TenantID: tenantID,
					Reason:   "tenant ID contains invalid character; only alphanumeric, hyphens, and underscores are allowed",
				},
			}
		}
	}
	return nil
}

// enforceEmbeddingTenantAccess validates that the requesting tenant has access
// to the embedding cache key. Returns nil if access is allowed, or an error
// if the tenant doesn't own the cache entry.
func enforceEmbeddingTenantAccess(tenantID, cacheKey, operation string) error {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil // Single-tenant mode / system tenant, no access control
	}
	if !embeddingCacheKeyBelongsToTenant(tenantID, cacheKey) {
		return &errors.QBError{
			Code: errors.ErrCrossTenantAccess,
			Op:   "embedding." + operation,
			Err: &EmbeddingCrossTenantAccessError{
				RequestingTenant: tenantID,
				OwnerTenant:      "unknown",
				Operation:        operation,
			},
		}
	}
	return nil
}
