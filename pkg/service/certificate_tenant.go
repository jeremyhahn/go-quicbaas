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
// This file implements multi-tenant certificate isolation. When a tenantID is
// present in the request context, all certificate operations are scoped to that
// tenant's namespace. When tenantID is empty, the system operates in
// single-tenant mode with no behavioral changes (full backward compatibility).
package service

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// certTenantPrefix is the storage prefix for tenant-scoped certificate serial numbers.
const certTenantPrefix = "tenant/"

// certTenantSeparator follows the tenant ID in the certificate namespace.
const certTenantSeparator = "/"

// certTenantScopedSerial returns a tenant-scoped serial number if tenantID is non-empty.
// For single-tenant mode (empty tenantID), the serial is returned as-is.
// Format: "tenant/{tenantID}/{serial}".
func certTenantScopedSerial(tenantID, serial string) string {
	if tenantID == "" {
		return serial
	}
	return certTenantPrefix + tenantID + certTenantSeparator + serial
}

// certTenantStripSerial removes the tenant prefix from a serial number.
// If the serial doesn't have a tenant prefix, it is returned as-is.
func certTenantStripSerial(tenantID, serial string) string {
	if tenantID == "" {
		return serial
	}
	prefix := certTenantPrefix + tenantID + certTenantSeparator
	return strings.TrimPrefix(serial, prefix)
}

// certTenantSerialBelongsTo checks if a serial number belongs to the given tenant.
// For single-tenant mode (empty tenantID), all serials belong.
// For multi-tenant mode, the serial must have the matching tenant prefix.
func certTenantSerialBelongsTo(tenantID, serial string) bool {
	if tenantID == "" {
		return true
	}
	prefix := certTenantPrefix + tenantID + certTenantSeparator
	return strings.HasPrefix(serial, prefix)
}

// certTenantListPrefix returns the prefix to use when listing certificates for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "tenant/{tenantID}/".
func certTenantListPrefix(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return certTenantPrefix + tenantID + certTenantSeparator
}

// certEnforceTenantAccess validates that the requesting tenant has access to the certificate.
// Returns nil if access is allowed, or an error if the tenant doesn't own the certificate.
func certEnforceTenantAccess(tenantID, serial, operation string) error {
	if tenantID == "" {
		return nil // Single-tenant mode, no access control
	}
	if !certTenantSerialBelongsTo(tenantID, serial) {
		return NewError(errors.ErrCrossTenantAccess, "certificate."+operation,
			"certificate does not belong to the requesting tenant")
	}
	return nil
}

// certPrepareTenantContext extracts and validates the tenant ID from context.
// Returns the validated tenant ID or an error if the tenant ID is invalid.
func certPrepareTenantContext(ctx context.Context) (string, error) {
	tenantID := extractTenantID(ctx)
	if err := validateTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}
