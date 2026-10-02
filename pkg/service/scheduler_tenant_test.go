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

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// schedulerTenantScopedJobName tests
// =============================================================================

func TestSchedulerTenantScopedJobName_EmptyTenant(t *testing.T) {
	// Single-tenant mode: job name is returned unchanged
	jobName := "backup-job"
	result := schedulerTenantScopedJobName("", jobName)
	assert.Equal(t, jobName, result)
}

func TestSchedulerTenantScopedJobName_WithTenant(t *testing.T) {
	// Multi-tenant mode: job name is prefixed with tenant namespace
	result := schedulerTenantScopedJobName("acme-corp", "backup-job")
	assert.Equal(t, "tenant/acme-corp/backup-job", result)
}

func TestSchedulerTenantScopedJobName_PreservesComplexJobName(t *testing.T) {
	// Job names with special characters are preserved
	result := schedulerTenantScopedJobName("tenant-123", "job/with/slashes")
	assert.Equal(t, "tenant/tenant-123/job/with/slashes", result)
}

// =============================================================================
// schedulerTenantStripJobName tests
// =============================================================================

func TestSchedulerTenantStripJobName_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := schedulerTenantStripJobName("", "backup-job")
	assert.Equal(t, "backup-job", result)
}

func TestSchedulerTenantStripJobName_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := schedulerTenantStripJobName("acme-corp", "tenant/acme-corp/backup-job")
	assert.Equal(t, "backup-job", result)
}

func TestSchedulerTenantStripJobName_NoMatchingPrefix(t *testing.T) {
	// If the job name doesn't have the tenant prefix, it's returned as-is
	result := schedulerTenantStripJobName("acme-corp", "other-job")
	assert.Equal(t, "other-job", result)
}

func TestSchedulerTenantStripJobName_WrongTenant(t *testing.T) {
	// If the job name belongs to a different tenant, the prefix is not stripped
	result := schedulerTenantStripJobName("acme-corp", "tenant/other-corp/backup-job")
	assert.Equal(t, "tenant/other-corp/backup-job", result)
}

// =============================================================================
// schedulerTenantJobBelongsTo tests
// =============================================================================

func TestSchedulerTenantJobBelongsTo_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all jobs belong
	assert.True(t, schedulerTenantJobBelongsTo("", "any-tenant"))
	assert.True(t, schedulerTenantJobBelongsTo("", ""))
}

func TestSchedulerTenantJobBelongsTo_MatchingTenant(t *testing.T) {
	assert.True(t, schedulerTenantJobBelongsTo("acme-corp", "acme-corp"))
}

func TestSchedulerTenantJobBelongsTo_NonMatchingTenant(t *testing.T) {
	assert.False(t, schedulerTenantJobBelongsTo("acme-corp", "other-corp"))
}

func TestSchedulerTenantJobBelongsTo_EmptyTargetTenant(t *testing.T) {
	// A job without a target tenant does not belong to a specific tenant
	assert.False(t, schedulerTenantJobBelongsTo("acme-corp", ""))
}

// =============================================================================
// schedulerTenantValidateID tests
// =============================================================================

func TestSchedulerTenantValidateID_Empty(t *testing.T) {
	// Empty is valid (single-tenant mode)
	assert.NoError(t, schedulerTenantValidateID(""))
}

func TestSchedulerTenantValidateID_ValidAlphanumeric(t *testing.T) {
	assert.NoError(t, schedulerTenantValidateID("acme-corp"))
	assert.NoError(t, schedulerTenantValidateID("tenant_123"))
	assert.NoError(t, schedulerTenantValidateID("MyTenant"))
	assert.NoError(t, schedulerTenantValidateID("a"))
}

func TestSchedulerTenantValidateID_InvalidCharacters(t *testing.T) {
	err := schedulerTenantValidateID("tenant/bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")

	err = schedulerTenantValidateID("tenant@bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")

	err = schedulerTenantValidateID("tenant bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestSchedulerTenantValidateID_TooLong(t *testing.T) {
	// Maximum 128 characters
	longID := strings.Repeat("a", 129)
	err := schedulerTenantValidateID(longID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum length")

	// Exactly 128 should pass
	maxID := strings.Repeat("a", 128)
	assert.NoError(t, schedulerTenantValidateID(maxID))
}

// =============================================================================
// schedulerTenantEnforceAccess tests
// =============================================================================

func TestSchedulerTenantEnforceAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	assert.NoError(t, schedulerTenantEnforceAccess("", "any-tenant", "GetJob"))
}

func TestSchedulerTenantEnforceAccess_MatchingTenant(t *testing.T) {
	assert.NoError(t, schedulerTenantEnforceAccess("acme-corp", "acme-corp", "GetJob"))
}

func TestSchedulerTenantEnforceAccess_CrossTenantDenied(t *testing.T) {
	err := schedulerTenantEnforceAccess("acme-corp", "other-corp", "GetJob")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

func TestSchedulerTenantEnforceAccess_EmptyTargetTenantDenied(t *testing.T) {
	// A multi-tenant request cannot access a job with no target tenant
	err := schedulerTenantEnforceAccess("acme-corp", "", "DeleteJob")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to the requesting tenant")
}

// =============================================================================
// schedulerTenantResolveID tests (go-quicbaas#1150)
// =============================================================================

// TestSchedulerTenantResolveID_RefusesUnboundContext pins the fail-closed half
// of #1150: a context with no tenant bound is not the system tenant, and the
// tenant the request names is not trusted in its place.
func TestSchedulerTenantResolveID_RefusesUnboundContext(t *testing.T) {
	for _, requested := range []string{"", "request-tenant"} {
		_, err := schedulerTenantResolveID(context.Background(), "GetJob", requested)
		var missing *tenantctx.MissingTenantError
		require.ErrorAs(t, err, &missing, "requested %q", requested)
	}
}

// TestSchedulerTenantResolveID_TenantCallerNamesOnlyItself proves a tenant
// caller resolves to its own tenant and is refused for any other, the system
// tenant included.
func TestSchedulerTenantResolveID_TenantCallerNamesOnlyItself(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	for _, requested := range []string{"", "acme"} {
		tenantID, err := schedulerTenantResolveID(ctx, "GetJob", requested)
		require.NoError(t, err)
		assert.Equal(t, "acme", tenantID)
	}
	for _, requested := range []string{"globex", tenantctx.SystemTenantID} {
		_, err := schedulerTenantResolveID(ctx, "GetJob", requested)
		var cross *tenantctx.CrossTenantError
		require.ErrorAs(t, err, &cross, "requested %q", requested)
	}
}

// TestSchedulerTenantResolveID_SystemCallerNamesOnlyItself proves the
// system tenant resolves to the store's system namespace ("") and is refused,
// like any caller, when it names another tenant (no cross-tenant lift).
func TestSchedulerTenantResolveID_SystemCallerNamesOnlyItself(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())

	tenantID, err := schedulerTenantResolveID(ctx, "GetJob", "")
	require.NoError(t, err)
	assert.Equal(t, "", tenantID, "the system tenant is the store's unprefixed namespace")

	_, err = schedulerTenantResolveID(ctx, "GetJob", "globex")
	var cross *tenantctx.CrossTenantError
	require.ErrorAs(t, err, &cross)
}

// TestSchedulerTenantResolveID_ValidatesTheResolvedTenant proves a malformed
// tenant is refused whichever side supplied it.
func TestSchedulerTenantResolveID_ValidatesTheResolvedTenant(t *testing.T) {
	_, err := schedulerTenantResolveID(tenantctx.WithTenant(context.Background(), "bad/tenant"), "GetJob", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

// =============================================================================
// Roundtrip tests: scope then strip
// =============================================================================

func TestSchedulerTenantJobName_Roundtrip(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		jobName  string
	}{
		{"empty-tenant", "", "backup-job"},
		{"with-tenant", "acme-corp", "backup-job"},
		{"complex-job-name", "tenant-123", "sub/path/job"},
		{"underscore-tenant", "my_tenant", "cleanup-job"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := schedulerTenantScopedJobName(tt.tenantID, tt.jobName)
			stripped := schedulerTenantStripJobName(tt.tenantID, scoped)
			assert.Equal(t, tt.jobName, stripped, "roundtrip should return original job name")
		})
	}
}

func TestSchedulerTenantJobName_ScopeAndBelongsTo(t *testing.T) {
	// A scoped job with matching TargetTenantID should belong to that tenant
	tenantID := "acme-corp"
	assert.True(t, schedulerTenantJobBelongsTo(tenantID, tenantID))

	// But not to a different tenant
	assert.False(t, schedulerTenantJobBelongsTo("other-corp", tenantID))
}

// =============================================================================
// System-tenant scoping tests
// =============================================================================

// TestSchedulerTenant_SystemTenantIsPlatformScoped proves a caller carrying
// the literal system tenant is treated identically to single-tenant mode
// rather than being isolated into a tenant namespace named "system".
func TestSchedulerTenant_SystemTenantIsPlatformScoped(t *testing.T) {
	for _, tenantID := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("tenantID="+tenantID, func(t *testing.T) {
			assert.Equal(t, "nightly-backup", schedulerTenantScopedJobName(tenantID, "nightly-backup"))
			assert.True(t, schedulerTenantJobBelongsTo(tenantID, "any-other-tenant"))
			assert.NoError(t, schedulerTenantEnforceAccess(tenantID, "any-other-tenant", "RunJob"))
		})
	}
}

// TestSchedulerTenant_OrdinaryTenantStaysIsolated proves the system-tenant
// carve-out does not widen ordinary tenants' reach: a lookalike ID that
// merely shares the "system" prefix is still fully isolated.
func TestSchedulerTenant_OrdinaryTenantStaysIsolated(t *testing.T) {
	scoped := schedulerTenantScopedJobName("system-2", "nightly-backup")
	assert.Equal(t, "tenant/system-2/nightly-backup", scoped)
	assert.False(t, schedulerTenantJobBelongsTo("system-2", "other-tenant"))
	err := schedulerTenantEnforceAccess("system-2", "other-tenant", "RunJob")
	require.Error(t, err)
}
