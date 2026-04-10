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
// This file implements multi-tenant dynamic secrets isolation. When a tenantID
// is present in the request context, all role and credential operations are
// scoped to that tenant's namespace. When tenantID is empty, the system
// operates in single-tenant mode with no behavioral changes (full backward
// compatibility).
package service

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// dsTenantPrefix is the storage prefix for tenant-scoped dynamic secrets roles.
const dsTenantPrefix = "tenant/"

// dsTenantSeparator follows the tenant ID in the dynamic secrets namespace.
const dsTenantSeparator = "/"

// dsTenantScopedRoleName returns a tenant-scoped role name if tenantID is non-empty.
// For single-tenant mode (empty tenantID), the role name is returned as-is.
// Format: "tenant/{tenantID}/{roleName}".
func dsTenantScopedRoleName(tenantID, roleName string) string {
	if tenantID == "" {
		return roleName
	}
	return dsTenantPrefix + tenantID + dsTenantSeparator + roleName
}

// dsTenantStripRoleName removes the tenant prefix from a role name.
// If the role name doesn't have a tenant prefix, it is returned as-is.
func dsTenantStripRoleName(tenantID, roleName string) string {
	if tenantID == "" {
		return roleName
	}
	prefix := dsTenantPrefix + tenantID + dsTenantSeparator
	return strings.TrimPrefix(roleName, prefix)
}

// dsTenantRoleBelongsTo checks if a role name belongs to the given tenant.
// For single-tenant mode (empty tenantID), all roles belong.
// For multi-tenant mode, the role must have the matching tenant prefix.
func dsTenantRoleBelongsTo(tenantID, roleName string) bool {
	if tenantID == "" {
		return true
	}
	prefix := dsTenantPrefix + tenantID + dsTenantSeparator
	return strings.HasPrefix(roleName, prefix)
}

// dsTenantListPrefix returns the prefix to use when listing roles for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "tenant/{tenantID}/".
func dsTenantListPrefix(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return dsTenantPrefix + tenantID + dsTenantSeparator
}

// dsTenantScopedLeasePrefix returns a tenant-scoped lease path prefix.
// For single-tenant mode, returns "credentials/".
// For multi-tenant mode, returns "credentials/tenant/{tenantID}/".
func dsTenantScopedLeasePrefix(tenantID string) string {
	if tenantID == "" {
		return "credentials/"
	}
	return "credentials/" + dsTenantPrefix + tenantID + dsTenantSeparator
}

// dsEnforceTenantAccess validates that the requesting tenant has access to the role.
// Returns nil if access is allowed, or an error if the tenant doesn't own the role.
func dsEnforceTenantAccess(tenantID, roleName, operation string) error {
	if tenantID == "" {
		return nil // Single-tenant mode, no access control
	}
	if !dsTenantRoleBelongsTo(tenantID, roleName) {
		return NewError(errors.ErrCrossTenantAccess, "dynamicSecrets."+operation,
			"role does not belong to the requesting tenant")
	}
	return nil
}

// dsPrepareTenantContext extracts and validates the tenant ID from context.
// Returns the validated tenant ID or an error if the tenant ID is invalid.
func dsPrepareTenantContext(ctx context.Context) (string, error) {
	tenantID := extractTenantID(ctx)
	if err := validateTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}
