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
// This file implements multi-tenant secrets isolation. When a tenantID is present
// in the request context, all secrets operations are scoped to that tenant's
// namespace. When tenantID is empty, the system operates in single-tenant
// mode with no behavioral changes (full backward compatibility).
package service

import (
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// secretsTenantPrefix is the storage prefix for tenant-scoped secrets.
const secretsTenantPrefix = "tenant/"

// secretsTenantSeparator follows the tenant ID in the secrets namespace.
const secretsTenantSeparator = "/"

// secretsTenantScopedID returns a tenant-scoped secret ID if tenantID is non-empty.
// For single-tenant mode (empty tenantID), the secret ID is returned as-is.
// Format: "tenant/{tenantID}/{secretID}".
func secretsTenantScopedID(tenantID, secretID string) string {
	if tenantID == "" {
		return secretID
	}
	return secretsTenantPrefix + tenantID + secretsTenantSeparator + secretID
}

// secretsTenantStripID removes the tenant prefix from a secret ID.
// If the secret ID doesn't have a tenant prefix, it is returned as-is.
func secretsTenantStripID(tenantID, secretID string) string {
	if tenantID == "" {
		return secretID
	}
	prefix := secretsTenantPrefix + tenantID + secretsTenantSeparator
	return strings.TrimPrefix(secretID, prefix)
}

// secretsTenantBelongsTo checks if a secret ID belongs to the given tenant.
// For single-tenant mode (empty tenantID), all secrets belong.
// For multi-tenant mode, the secret must have the matching tenant prefix.
func secretsTenantBelongsTo(tenantID, secretID string) bool {
	if tenantID == "" {
		return true
	}
	prefix := secretsTenantPrefix + tenantID + secretsTenantSeparator
	return strings.HasPrefix(secretID, prefix)
}

// secretsTenantListPrefix returns the prefix to use when listing secrets for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "tenant/{tenantID}/".
func secretsTenantListPrefix(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return secretsTenantPrefix + tenantID + secretsTenantSeparator
}

// enforceSecretsTenantAccess validates that the requesting tenant has access to the secret.
// Returns nil if access is allowed, or an error if the tenant doesn't own the secret.
func enforceSecretsTenantAccess(tenantID, secretID, operation string) error {
	if tenantID == "" {
		return nil // Single-tenant mode, no access control
	}
	if !secretsTenantBelongsTo(tenantID, secretID) {
		return NewError(errors.ErrCrossTenantAccess, "secrets."+operation,
			"secret does not belong to the requesting tenant")
	}
	return nil
}
