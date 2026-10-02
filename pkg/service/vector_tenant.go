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
// This file implements multi-tenant vector isolation. When a tenantID is
// present in the request context, all vector namespace operations are scoped
// to that tenant's namespace. When tenantID is empty, the system operates in
// single-tenant mode with no behavioral changes (full backward compatibility).
package service

import (
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// vectorTenantPrefix is the storage prefix for tenant-scoped vector namespaces.
const vectorTenantPrefix = "tenant/"

// vectorTenantSeparator follows the tenant ID in the namespace path.
const vectorTenantSeparator = "/"

// vectorTenantScopedNamespace returns a tenant-scoped namespace if tenantID is non-empty.
// For single-tenant mode (empty tenantID), the namespace is returned as-is.
// Format: "tenant/{tenantID}/{namespace}".
func vectorTenantScopedNamespace(tenantID, ns string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return ns
	}
	return vectorTenantPrefix + tenantID + vectorTenantSeparator + ns
}

// vectorTenantStripNamespace removes the tenant prefix from a namespace name.
// If the namespace doesn't have a tenant prefix, it is returned as-is.
func vectorTenantStripNamespace(tenantID, ns string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return ns
	}
	prefix := vectorTenantPrefix + tenantID + vectorTenantSeparator
	return strings.TrimPrefix(ns, prefix)
}

// vectorNamespaceBelongsToTenant checks if a namespace belongs to the given tenant.
// For single-tenant mode (empty tenantID), all namespaces belong.
// For multi-tenant mode, the namespace must have the matching tenant prefix.
func vectorNamespaceBelongsToTenant(tenantID, ns string) bool {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return true
	}
	prefix := vectorTenantPrefix + tenantID + vectorTenantSeparator
	return strings.HasPrefix(ns, prefix)
}

// vectorTenantListPrefix returns the prefix to use when filtering namespaces for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "tenant/{tenantID}/".
func vectorTenantListPrefix(tenantID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return ""
	}
	return vectorTenantPrefix + tenantID + vectorTenantSeparator
}

// vectorEnforceTenantAccess validates that the requesting tenant has access to the namespace.
// Returns nil if access is allowed, or an error if the tenant doesn't own the namespace.
func vectorEnforceTenantAccess(tenantID, ns, operation string) error {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil // Single-tenant mode / system tenant, no access control
	}
	if !vectorNamespaceBelongsToTenant(tenantID, ns) {
		return NewError(errors.ErrCrossTenantAccess, "vector."+operation,
			"namespace does not belong to the requesting tenant")
	}
	return nil
}
