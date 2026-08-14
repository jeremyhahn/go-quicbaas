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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// tsTenantScopedMetric tests
// =============================================================================

func TestTsTenantScopedMetric_EmptyTenant(t *testing.T) {
	// Single-tenant mode: metric is returned unchanged
	metric := "cpu.usage"
	result := tsTenantScopedMetric("", metric)
	assert.Equal(t, metric, result)
}

func TestTsTenantScopedMetric_WithTenant(t *testing.T) {
	// Multi-tenant mode: metric is prefixed with tenant namespace
	result := tsTenantScopedMetric("acme-corp", "cpu.usage")
	assert.Equal(t, "ts-tenant/acme-corp/cpu.usage", result)
}

func TestTsTenantScopedMetric_PreservesMetricChars(t *testing.T) {
	// Metric with dots and other characters is preserved
	result := tsTenantScopedMetric("tenant-123", "system.disk.io.bytes_read")
	assert.Equal(t, "ts-tenant/tenant-123/system.disk.io.bytes_read", result)
}

// =============================================================================
// tsTenantStripMetric tests
// =============================================================================

func TestTsTenantStripMetric_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := tsTenantStripMetric("", "cpu.usage")
	assert.Equal(t, "cpu.usage", result)
}

func TestTsTenantStripMetric_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := tsTenantStripMetric("acme-corp", "ts-tenant/acme-corp/cpu.usage")
	assert.Equal(t, "cpu.usage", result)
}

func TestTsTenantStripMetric_NoMatchingPrefix(t *testing.T) {
	// Metric without tenant prefix is returned as-is
	result := tsTenantStripMetric("acme-corp", "plain-metric")
	assert.Equal(t, "plain-metric", result)
}

func TestTsTenantStripMetric_WrongTenant(t *testing.T) {
	// Metric belonging to a different tenant is not stripped
	result := tsTenantStripMetric("acme-corp", "ts-tenant/other-corp/cpu.usage")
	assert.Equal(t, "ts-tenant/other-corp/cpu.usage", result)
}

// =============================================================================
// tsMetricBelongsToTenant tests
// =============================================================================

func TestTsMetricBelongsToTenant_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all metrics belong
	assert.True(t, tsMetricBelongsToTenant("", "anything"))
	assert.True(t, tsMetricBelongsToTenant("", "ts-tenant/other/metric"))
}

func TestTsMetricBelongsToTenant_MatchingTenant(t *testing.T) {
	// Multi-tenant mode: metric with matching prefix belongs
	metric := tsTenantScopedMetric("acme-corp", "cpu.usage")
	assert.True(t, tsMetricBelongsToTenant("acme-corp", metric))
}

func TestTsMetricBelongsToTenant_WrongTenant(t *testing.T) {
	// Multi-tenant mode: metric with different prefix does not belong
	metric := tsTenantScopedMetric("other-corp", "cpu.usage")
	assert.False(t, tsMetricBelongsToTenant("acme-corp", metric))
}

func TestTsMetricBelongsToTenant_UnprefixedMetric(t *testing.T) {
	// Multi-tenant mode: unprefixed metric does not belong
	assert.False(t, tsMetricBelongsToTenant("acme-corp", "plain-metric"))
}

// =============================================================================
// tsTenantListPrefix tests
// =============================================================================

func TestTsTenantListPrefix_EmptyTenant(t *testing.T) {
	// Single-tenant mode: returns empty string
	result := tsTenantListPrefix("")
	assert.Equal(t, "", result)
}

func TestTsTenantListPrefix_WithTenant(t *testing.T) {
	// Multi-tenant mode: returns the tenant prefix
	result := tsTenantListPrefix("acme-corp")
	assert.Equal(t, "ts-tenant/acme-corp/", result)
}

// =============================================================================
// prepareTimeSeriesTenantContext tests
// =============================================================================

func TestPrepareTimeSeriesTenantContext_EmptyContext(t *testing.T) {
	// No tenant in context returns empty string (single-tenant mode)
	ctx := context.Background()
	tenantID, err := prepareTimeSeriesTenantContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, "", tenantID)
}

func TestPrepareTimeSeriesTenantContext_ValidTenant(t *testing.T) {
	// Valid tenant ID is returned
	ctx := iam.ContextWithTenant(context.Background(), "acme-corp")
	tenantID, err := prepareTimeSeriesTenantContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestPrepareTimeSeriesTenantContext_InvalidTenant(t *testing.T) {
	// Invalid tenant ID returns an error
	ctx := iam.ContextWithTenant(context.Background(), "invalid tenant!@#")
	_, err := prepareTimeSeriesTenantContext(ctx)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestPrepareTimeSeriesTenantContext_TooLongTenant(t *testing.T) {
	// Tenant ID exceeding 128 characters returns an error
	longTenant := strings.Repeat("a", 129)
	ctx := iam.ContextWithTenant(context.Background(), longTenant)
	_, err := prepareTimeSeriesTenantContext(ctx)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

// =============================================================================
// validateTimeSeriesTenantID tests
// =============================================================================

func TestValidateTimeSeriesTenantID_Empty(t *testing.T) {
	// Empty tenant ID is valid (single-tenant mode)
	err := validateTimeSeriesTenantID("")
	assert.NoError(t, err)
}

func TestValidateTimeSeriesTenantID_Valid(t *testing.T) {
	// Valid tenant IDs with alphanumeric, hyphens, and underscores
	validIDs := []string{"acme", "tenant-123", "my_tenant", "UPPER_case-123"}
	for _, id := range validIDs {
		err := validateTimeSeriesTenantID(id)
		assert.NoError(t, err, "tenant ID %q should be valid", id)
	}
}

func TestValidateTimeSeriesTenantID_InvalidChars(t *testing.T) {
	// Tenant IDs with invalid characters are rejected
	invalidIDs := []string{"tenant@corp", "ten ant", "tenant/corp", "tenant:id"}
	for _, id := range invalidIDs {
		err := validateTimeSeriesTenantID(id)
		require.Error(t, err, "tenant ID %q should be invalid", id)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	}
}

func TestValidateTimeSeriesTenantID_TooLong(t *testing.T) {
	// Tenant ID at exactly 128 characters is valid
	exact128 := strings.Repeat("a", 128)
	err := validateTimeSeriesTenantID(exact128)
	assert.NoError(t, err)

	// Tenant ID at 129 characters is too long
	tooLong := strings.Repeat("a", 129)
	err = validateTimeSeriesTenantID(tooLong)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

// =============================================================================
// enforceTimeSeriesTenantAccess tests
// =============================================================================

func TestEnforceTimeSeriesTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	err := enforceTimeSeriesTenantAccess("", "any-metric", "WritePoint")
	assert.NoError(t, err)
}

func TestEnforceTimeSeriesTenantAccess_MatchingTenant(t *testing.T) {
	// Multi-tenant mode: matching tenant prefix is allowed
	metric := tsTenantScopedMetric("acme-corp", "cpu.usage")
	err := enforceTimeSeriesTenantAccess("acme-corp", metric, "WritePoint")
	assert.NoError(t, err)
}

func TestEnforceTimeSeriesTenantAccess_CrossTenant(t *testing.T) {
	// Multi-tenant mode: cross-tenant access is denied
	metric := tsTenantScopedMetric("other-corp", "cpu.usage")
	err := enforceTimeSeriesTenantAccess("acme-corp", metric, "WritePoint")
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, qbErr.Code)
}

func TestEnforceTimeSeriesTenantAccess_UnprefixedMetric(t *testing.T) {
	// Multi-tenant mode: unprefixed metric is denied
	err := enforceTimeSeriesTenantAccess("acme-corp", "plain-metric", "TSQuery")
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, qbErr.Code)
}

// =============================================================================
// tsFilterMetricsByTenant tests
// =============================================================================

func TestTsFilterMetricsByTenant_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all metrics returned unchanged
	metrics := []string{"cpu.usage", "mem.usage", "disk.io"}
	result := tsFilterMetricsByTenant("", metrics)
	assert.Equal(t, metrics, result)
}

func TestTsFilterMetricsByTenant_FiltersCorrectly(t *testing.T) {
	// Multi-tenant mode: only matching tenant metrics are returned, stripped
	metrics := []string{
		"ts-tenant/acme-corp/cpu.usage",
		"ts-tenant/other-corp/cpu.usage",
		"ts-tenant/acme-corp/mem.usage",
		"plain-metric",
	}
	result := tsFilterMetricsByTenant("acme-corp", metrics)
	assert.Equal(t, []string{"cpu.usage", "mem.usage"}, result)
}

func TestTsFilterMetricsByTenant_NoMatches(t *testing.T) {
	// Multi-tenant mode: no matching metrics returns empty slice
	metrics := []string{
		"ts-tenant/other-corp/cpu.usage",
		"plain-metric",
	}
	result := tsFilterMetricsByTenant("acme-corp", metrics)
	assert.Empty(t, result)
}

func TestTsFilterMetricsByTenant_EmptyInput(t *testing.T) {
	// Empty input returns empty slice
	result := tsFilterMetricsByTenant("acme-corp", []string{})
	assert.Empty(t, result)
}

func TestTsFilterMetricsByTenant_NilInput(t *testing.T) {
	// Nil input returns nil in single-tenant mode
	result := tsFilterMetricsByTenant("", nil)
	assert.Nil(t, result)

	// Nil input returns empty slice in multi-tenant mode
	result = tsFilterMetricsByTenant("acme-corp", nil)
	assert.Empty(t, result)
}

// =============================================================================
// Error type tests
// =============================================================================

func TestTimeSeriesTenantValidationError_Error(t *testing.T) {
	err := &TimeSeriesTenantValidationError{
		TenantID: "bad-id",
		Reason:   "contains invalid characters",
	}
	msg := err.Error()
	assert.Contains(t, msg, "bad-id")
	assert.Contains(t, msg, "contains invalid characters")
}

func TestTimeSeriesTenantValidationError_EmptyFields(t *testing.T) {
	err := &TimeSeriesTenantValidationError{}
	msg := err.Error()
	assert.Contains(t, msg, "timeseries tenant validation failed")
}

func TestTimeSeriesCrossTenantAccessError_Error(t *testing.T) {
	err := &TimeSeriesCrossTenantAccessError{
		RequestingTenant: "tenant-a",
		OwnerTenant:      "tenant-b",
		Metric:           "cpu.usage",
		Operation:        "WritePoint",
	}
	msg := err.Error()
	assert.Contains(t, msg, "tenant-a")
	assert.Contains(t, msg, "tenant-b")
	assert.Contains(t, msg, "cpu.usage")
	assert.Contains(t, msg, "WritePoint")
}

func TestTimeSeriesCrossTenantAccessError_EmptyFields(t *testing.T) {
	err := &TimeSeriesCrossTenantAccessError{}
	msg := err.Error()
	assert.Contains(t, msg, "timeseries cross-tenant access denied")
}

// =============================================================================
// System-tenant scoping tests
// =============================================================================

// TestTimeSeriesTenant_SystemTenantIsPlatformScoped proves a caller carrying
// the literal system tenant is treated identically to single-tenant mode
// rather than being isolated into a tenant namespace named "system".
func TestTimeSeriesTenant_SystemTenantIsPlatformScoped(t *testing.T) {
	for _, tenantID := range []string{"", qrdbsdk.SystemTenantID} {
		t.Run("tenantID="+tenantID, func(t *testing.T) {
			assert.Equal(t, "cpu.load", tsTenantScopedMetric(tenantID, "cpu.load"))
			assert.True(t, tsMetricBelongsToTenant(tenantID, "any.metric"))
			assert.NoError(t, enforceTimeSeriesTenantAccess(tenantID, "any.metric", "Write"))
		})
	}
}

// TestTimeSeriesTenant_OrdinaryTenantStaysIsolated proves the system-tenant
// carve-out does not widen ordinary tenants' reach: a lookalike ID that
// merely shares the "system" prefix is still fully isolated.
func TestTimeSeriesTenant_OrdinaryTenantStaysIsolated(t *testing.T) {
	scoped := tsTenantScopedMetric("system-2", "cpu.load")
	assert.Equal(t, "ts-tenant/system-2/cpu.load", scoped)
	assert.False(t, tsMetricBelongsToTenant("system-2", "cpu.load"))
	err := enforceTimeSeriesTenantAccess("system-2", "cpu.load", "Write")
	require.Error(t, err)
}
