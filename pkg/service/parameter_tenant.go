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
// This file implements multi-tenant parameter store isolation. When a tenantID
// is present in the request context, all parameter operations are scoped to
// that tenant's namespace. When tenantID is empty, the system operates in
// single-tenant mode with no behavioral changes (full backward compatibility).
package service

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// paramTenantPrefix is the storage prefix for tenant-scoped parameters.
const paramTenantPrefix = "tenant/"

// paramTenantSeparator follows the tenant ID in the parameter namespace.
const paramTenantSeparator = "/"

// paramTenantScopedPath returns a tenant-scoped parameter path.
// For single-tenant mode (empty tenantID), the path is returned as-is.
// Format: "tenant/{tenantID}/{path}".
func paramTenantScopedPath(tenantID, path string) string {
	if tenantID == "" {
		return path
	}
	return paramTenantPrefix + tenantID + paramTenantSeparator + path
}

// paramTenantStripPath removes the tenant prefix from a parameter path.
// If the path doesn't have a tenant prefix, it is returned as-is.
func paramTenantStripPath(tenantID, path string) string {
	if tenantID == "" {
		return path
	}
	prefix := paramTenantPrefix + tenantID + paramTenantSeparator
	return strings.TrimPrefix(path, prefix)
}

// paramPathBelongsToTenant checks if a path belongs to the given tenant.
// For single-tenant mode (empty tenantID), all paths belong.
// For multi-tenant mode, the path must have the matching tenant prefix.
func paramPathBelongsToTenant(tenantID, path string) bool {
	if tenantID == "" {
		return true
	}
	prefix := paramTenantPrefix + tenantID + paramTenantSeparator
	return strings.HasPrefix(path, prefix)
}

// paramTenantListPrefix returns the prefix to use when listing parameters for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "tenant/{tenantID}/".
func paramTenantListPrefix(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return paramTenantPrefix + tenantID + paramTenantSeparator
}

// paramEnforceTenantAccess validates that the requesting tenant has access to the parameter path.
// Returns nil if access is allowed, or an error if the tenant doesn't own the path.
func paramEnforceTenantAccess(tenantID, path, operation string) error {
	if tenantID == "" {
		return nil // Single-tenant mode, no access control
	}
	if !paramPathBelongsToTenant(tenantID, path) {
		return NewError(errors.ErrCrossTenantAccess, "parameterstore."+operation,
			"parameter does not belong to the requesting tenant")
	}
	return nil
}

// paramPrepareTenantContext extracts and validates the tenant ID from context.
// Returns the validated tenant ID or an error if the tenant ID is invalid.
func paramPrepareTenantContext(ctx context.Context) (string, error) {
	tenantID := extractTenantID(ctx)
	if err := validateTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}
