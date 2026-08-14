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

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// certTenantScopedSerial tests
// =============================================================================

func TestCertTenantScopedSerial_EmptyTenant(t *testing.T) {
	// Single-tenant mode: serial is returned unchanged
	serial := "ABC123"
	result := certTenantScopedSerial("", serial)
	assert.Equal(t, serial, result)
}

func TestCertTenantScopedSerial_WithTenant(t *testing.T) {
	// Multi-tenant mode: serial is prefixed with tenant namespace
	result := certTenantScopedSerial("acme-corp", "ABC123")
	assert.Equal(t, "tenant/acme-corp/ABC123", result)
}

func TestCertTenantScopedSerial_PreservesComplexSerial(t *testing.T) {
	// Serials with special characters are preserved
	result := certTenantScopedSerial("tenant-123", "serial/with/slashes")
	assert.Equal(t, "tenant/tenant-123/serial/with/slashes", result)
}

// =============================================================================
// certTenantStripSerial tests
// =============================================================================

func TestCertTenantStripSerial_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := certTenantStripSerial("", "ABC123")
	assert.Equal(t, "ABC123", result)
}

func TestCertTenantStripSerial_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := certTenantStripSerial("acme-corp", "tenant/acme-corp/ABC123")
	assert.Equal(t, "ABC123", result)
}

func TestCertTenantStripSerial_NoMatchingPrefix(t *testing.T) {
	// If the serial doesn't have the tenant prefix, it's returned as-is
	result := certTenantStripSerial("acme-corp", "other-serial")
	assert.Equal(t, "other-serial", result)
}

func TestCertTenantStripSerial_WrongTenant(t *testing.T) {
	// If the serial belongs to a different tenant, the prefix is not stripped
	result := certTenantStripSerial("acme-corp", "tenant/other-corp/ABC123")
	assert.Equal(t, "tenant/other-corp/ABC123", result)
}

// =============================================================================
// certTenantSerialBelongsTo tests
// =============================================================================

func TestCertTenantSerialBelongsTo_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all serials belong
	assert.True(t, certTenantSerialBelongsTo("", "any-serial"))
	assert.True(t, certTenantSerialBelongsTo("", "tenant/acme-corp/serial"))
}

func TestCertTenantSerialBelongsTo_MatchingTenant(t *testing.T) {
	assert.True(t, certTenantSerialBelongsTo("acme-corp", "tenant/acme-corp/ABC123"))
}

func TestCertTenantSerialBelongsTo_NonMatchingTenant(t *testing.T) {
	assert.False(t, certTenantSerialBelongsTo("acme-corp", "tenant/other-corp/ABC123"))
}

func TestCertTenantSerialBelongsTo_UnscopedSerial(t *testing.T) {
	// A serial without tenant prefix does not belong to a multi-tenant request
	assert.False(t, certTenantSerialBelongsTo("acme-corp", "ABC123"))
}

// =============================================================================
// certTenantListPrefix tests
// =============================================================================

func TestCertTenantListPrefix_EmptyTenant(t *testing.T) {
	assert.Equal(t, "", certTenantListPrefix(""))
}

func TestCertTenantListPrefix_WithTenant(t *testing.T) {
	assert.Equal(t, "tenant/acme-corp/", certTenantListPrefix("acme-corp"))
}

// =============================================================================
// certEnforceTenantAccess tests
// =============================================================================

func TestCertEnforceTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, certEnforceTenantAccess("", "any-serial", "RevokeCertificate"))
}

func TestCertEnforceTenantAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, certEnforceTenantAccess("acme-corp", "tenant/acme-corp/ABC123", "RevokeCertificate"))
}

func TestCertEnforceTenantAccess_CrossTenantDenied(t *testing.T) {
	err := certEnforceTenantAccess("acme-corp", "tenant/other-corp/ABC123", "RevokeCertificate")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestCertEnforceTenantAccess_UnscopedSerialDenied(t *testing.T) {
	// A multi-tenant request cannot access an unscoped serial
	err := certEnforceTenantAccess("acme-corp", "unscoped-serial", "IsRevoked")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

// =============================================================================
// certPrepareTenantContext tests
// =============================================================================

func TestCertPrepareTenantContext_NoTenant(t *testing.T) {
	ctx := context.Background()
	tenantID, err := certPrepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "", tenantID)
}

func TestCertPrepareTenantContext_ValidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "acme-corp")
	tenantID, err := certPrepareTenantContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestCertPrepareTenantContext_InvalidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "invalid/tenant")
	_, err := certPrepareTenantContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestCertTenantSerial_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		serial   string
	}{
		{"empty-tenant", "", "ABC123"},
		{"with-tenant", "acme-corp", "DEF456"},
		{"complex-serial", "tenant-123", "serial/with/slashes"},
		{"underscore-tenant", "my_tenant", "GHI789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := certTenantScopedSerial(tt.tenantID, tt.serial)
			stripped := certTenantStripSerial(tt.tenantID, scoped)
			assert.Equal(t, tt.serial, stripped, "roundtrip should return original serial")
		})
	}
}

func TestCertTenantSerial_ScopeAndBelongsTo(t *testing.T) {
	// A scoped serial should belong to the tenant that scoped it
	tenantID := "acme-corp"
	serial := "ABC123"
	scoped := certTenantScopedSerial(tenantID, serial)
	assert.True(t, certTenantSerialBelongsTo(tenantID, scoped))

	// But not to a different tenant
	assert.False(t, certTenantSerialBelongsTo("other-corp", scoped))
}

func TestCertTenantSerial_ListPrefixMatchesScopedSerials(t *testing.T) {
	tenantID := "acme-corp"
	serial := "ABC123"
	scoped := certTenantScopedSerial(tenantID, serial)
	prefix := certTenantListPrefix(tenantID)
	assert.True(t, strings.HasPrefix(scoped, prefix),
		"scoped serial should start with tenant list prefix")
}

// =============================================================================
// System-tenant scoping tests
// =============================================================================

// TestCertificateTenant_SystemTenantIsPlatformScoped proves a caller carrying
// the literal system tenant is treated identically to single-tenant mode
// rather than being isolated into a tenant namespace named "system".
func TestCertificateTenant_SystemTenantIsPlatformScoped(t *testing.T) {
	for _, tenantID := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("tenantID="+tenantID, func(t *testing.T) {
			assert.Equal(t, "01AB", certTenantScopedSerial(tenantID, "01AB"))
			assert.True(t, certTenantSerialBelongsTo(tenantID, "any-serial"))
			assert.NoError(t, certEnforceTenantAccess(tenantID, "any-serial", "GetCertificate"))
		})
	}
}

// TestCertificateTenant_OrdinaryTenantStaysIsolated proves the system-tenant
// carve-out does not widen ordinary tenants' reach: a lookalike ID that
// merely shares the "system" prefix is still fully isolated.
func TestCertificateTenant_OrdinaryTenantStaysIsolated(t *testing.T) {
	scoped := certTenantScopedSerial("system-2", "01AB")
	assert.Equal(t, "tenant/system-2/01AB", scoped)
	assert.False(t, certTenantSerialBelongsTo("system-2", "01AB"))
	err := certEnforceTenantAccess("system-2", "01AB", "GetCertificate")
	require.Error(t, err)
}
