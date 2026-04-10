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
// This file implements multi-tenant object storage admin isolation. When a
// tenantID is present in the request context, all object storage admin operations
// are scoped to that tenant's namespace. When tenantID is empty, the system
// operates in single-tenant mode with no behavioral changes.
package service

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

const osAdminTenantPrefix = "tenant/"
const osAdminTenantSeparator = "/"

// osAdminTenantScopedBackend returns a tenant-scoped backend name.
// For single-tenant mode (empty tenantID), the name is returned as-is.
// Format: "tenant/{tenantID}/{backend}".
func osAdminTenantScopedBackend(tenantID, backend string) string {
	if tenantID == "" {
		return backend
	}
	return osAdminTenantPrefix + tenantID + osAdminTenantSeparator + backend
}

// osAdminTenantStripBackend removes the tenant prefix from a backend name.
func osAdminTenantStripBackend(tenantID, backend string) string {
	if tenantID == "" {
		return backend
	}
	prefix := osAdminTenantPrefix + tenantID + osAdminTenantSeparator
	return strings.TrimPrefix(backend, prefix)
}

// osAdminBackendBelongsToTenant checks if a backend name belongs to the given tenant.
func osAdminBackendBelongsToTenant(tenantID, backend string) bool {
	if tenantID == "" {
		return true
	}
	prefix := osAdminTenantPrefix + tenantID + osAdminTenantSeparator
	return strings.HasPrefix(backend, prefix)
}

// osAdminTenantListPrefix returns the prefix for listing backends for a tenant.
func osAdminTenantListPrefix(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return osAdminTenantPrefix + tenantID + osAdminTenantSeparator
}

// osAdminEnforceTenantAccess validates tenant access to a backend.
func osAdminEnforceTenantAccess(tenantID, backend, operation string) error {
	if tenantID == "" {
		return nil
	}
	if !osAdminBackendBelongsToTenant(tenantID, backend) {
		return NewError(errors.ErrCrossTenantAccess, "objectstorage_admin."+operation,
			"backend does not belong to the requesting tenant")
	}
	return nil
}

// osAdminPrepareTenantContext extracts and validates the tenant ID from context.
func osAdminPrepareTenantContext(ctx context.Context) (string, error) {
	tenantID := extractTenantID(ctx)
	if err := validateTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}
