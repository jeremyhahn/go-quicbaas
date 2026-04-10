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
// This file implements multi-tenant KMS isolation. When a tenantID is present
// in the request context, all KMS key operations are scoped to that tenant's
// namespace. When tenantID is empty, the system operates in single-tenant
// mode with no behavioral changes (full backward compatibility).
package service

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// kmsTenantKeyPrefix is the storage prefix for tenant-scoped KMS keys.
const kmsTenantKeyPrefix = "tenant/"

// kmsTenantKeySuffix follows the tenant ID in the key namespace.
const kmsTenantKeySuffix = "/"

// tenantScopedKeyID returns a tenant-scoped key ID if tenantID is non-empty.
// For single-tenant mode (empty tenantID), the key ID is returned as-is.
// Format: "tenant/{tenantID}/{keyID}".
func tenantScopedKeyID(tenantID, keyID string) string {
	if tenantID == "" {
		return keyID
	}
	return kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix + keyID
}

// tenantStripKeyID removes the tenant prefix from a key ID.
// If the key ID doesn't have a tenant prefix, it is returned as-is.
func tenantStripKeyID(tenantID, keyID string) string {
	if tenantID == "" {
		return keyID
	}
	prefix := kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix
	return strings.TrimPrefix(keyID, prefix)
}

// tenantKeyBelongsTo checks if a key ID belongs to the given tenant.
// For single-tenant mode (empty tenantID), all keys belong.
// For multi-tenant mode, the key must have the matching tenant prefix.
func tenantKeyBelongsTo(tenantID, keyID string) bool {
	if tenantID == "" {
		return true
	}
	prefix := kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix
	return strings.HasPrefix(keyID, prefix)
}

// tenantListPrefix returns the prefix to use when listing keys for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "tenant/{tenantID}/".
func tenantListPrefix(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix
}

// extractTenantID extracts the tenant ID from the context.
// Returns empty string for single-tenant mode.
func extractTenantID(ctx context.Context) string {
	return iam.TenantFromContext(ctx)
}

// validateTenantID validates that a tenant ID contains only safe characters.
// Empty tenant ID is valid (single-tenant mode).
func validateTenantID(tenantID string) error {
	if tenantID == "" {
		return nil
	}
	// Tenant IDs must be alphanumeric with hyphens and underscores, 1-128 chars
	if len(tenantID) > 128 {
		return NewError(errors.ErrInvalidInput, "kms.validateTenantID", "tenant ID exceeds maximum length of 128 characters")
	}
	for _, ch := range tenantID {
		if !isValidTenantChar(ch) {
			return NewError(errors.ErrInvalidInput, "kms.validateTenantID",
				"tenant ID contains invalid character; only alphanumeric, hyphens, and underscores are allowed")
		}
	}
	return nil
}

// isValidTenantChar returns true if the character is allowed in tenant IDs.
func isValidTenantChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '-' || ch == '_'
}

// enforceTenantAccess validates that the requesting tenant has access to the key.
// Returns nil if access is allowed, or an error if the tenant doesn't own the key.
func enforceTenantAccess(tenantID, keyID, operation string) error {
	if tenantID == "" {
		return nil // Single-tenant mode, no access control
	}
	if !tenantKeyBelongsTo(tenantID, keyID) {
		return NewError(errors.ErrCrossTenantAccess, "kms."+operation,
			"key does not belong to the requesting tenant")
	}
	return nil
}

// prepareTenantContext extracts and validates the tenant ID from context.
// Returns the validated tenant ID or an error if the tenant ID is invalid.
func prepareTenantContext(ctx context.Context) (string, error) {
	tenantID := extractTenantID(ctx)
	if err := validateTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}
