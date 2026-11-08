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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// kmsTenantKeyPrefix is the storage prefix for tenant-scoped KMS keys.
const kmsTenantKeyPrefix = "tenant/"

// kmsTenantKeySuffix follows the tenant ID in the key namespace.
const kmsTenantKeySuffix = "/"

// tenantScopedKeyID returns a tenant-scoped key ID if tenantID is non-empty.
// For single-tenant mode (empty tenantID) or the system tenant, the key ID is
// returned as-is: a system-tenant caller is platform-scoped, not isolated
// into a namespace literally named "system".
// Format: "tenant/{tenantID}/{keyID}".
func tenantScopedKeyID(tenantID, keyID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return keyID
	}
	return kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix + keyID
}

// tenantStripKeyID removes the tenant prefix from a key ID.
// If the key ID doesn't have a tenant prefix, it is returned as-is.
func tenantStripKeyID(tenantID, keyID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return keyID
	}
	prefix := kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix
	return strings.TrimPrefix(keyID, prefix)
}

// tenantKeyBelongsTo checks if a key ID belongs to the given tenant.
// For single-tenant mode (empty tenantID) or the system tenant, all keys
// belong (no isolation applies to the platform scope).
// For multi-tenant mode, the key must have the matching tenant prefix.
func tenantKeyBelongsTo(tenantID, keyID string) bool {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return true
	}
	prefix := kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix
	return strings.HasPrefix(keyID, prefix)
}

// safetyResultsForTenant keeps the shared safety tracker's results that
// belong to tenantID, each ID returned in the tenant's own namespace. The
// system caller keeps every tenant's results.
func safetyResultsForTenant(tenantID string, results []SafetyCheckResult) []SafetyCheckResult {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return results
	}
	owned := make([]SafetyCheckResult, 0, len(results))
	for _, r := range results {
		if !tenantKeyBelongsTo(tenantID, r.ID) {
			continue
		}
		r.ID = tenantStripKeyID(tenantID, r.ID)
		owned = append(owned, r)
	}
	return owned
}

// tenantListPrefix returns the prefix to use when listing keys for a tenant.
// For single-tenant mode or the system tenant, returns empty string (list all).
// For multi-tenant mode, returns "tenant/{tenantID}/".
func tenantListPrefix(tenantID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return ""
	}
	return kmsTenantKeyPrefix + tenantID + kmsTenantKeySuffix
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
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil // Single-tenant mode / system tenant, no access control
	}
	if !tenantKeyBelongsTo(tenantID, keyID) {
		return NewError(errors.ErrCrossTenantAccess, "kms."+operation,
			"key does not belong to the requesting tenant")
	}
	return nil
}

// prepareTenantContext reads the request's tenant (tenantctx.FromContext,
// which refuses a context with no tenant bound) and validates it.
//
// It returns the tenant in the services' own convention, where "" is the
// platform scope: an explicit system binding maps to "", which is what the
// tenant-scoping helpers and the downstream stores (an xkms SignCSR with no
// tenant SAN, an unprefixed key or secret) have always taken for the system.
// What changed in go-quicbaas#1150 is only that the empty result now has to
// be asked for by binding the system tenant, never produced by the absence
// of a binding.
func prepareTenantContext(ctx context.Context) (string, error) {
	tenantID, err := tenantctx.FromContext(ctx, "service.tenant")
	if err != nil {
		return "", err
	}
	if tenantctx.IsSystem(tenantID) {
		return "", nil
	}
	if err := validateTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}
