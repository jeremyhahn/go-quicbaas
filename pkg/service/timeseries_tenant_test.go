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
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Metric scoping helper tests
// =============================================================================

func TestTsTenantScopedMetric(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		metric   string
		want     string
	}{
		{name: "empty tenant leaves the metric unchanged", tenantID: "", metric: "cpu.usage", want: "cpu.usage"},
		{name: "tenant prefixes the metric", tenantID: "acme-corp", metric: "cpu.usage", want: "ts-tenant/acme-corp/cpu.usage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tsTenantScopedMetric(tt.tenantID, tt.metric))
		})
	}
}

func TestTsTenantStripMetric(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		metric   string
		want     string
	}{
		{name: "empty tenant strips nothing", tenantID: "", metric: "cpu.usage", want: "cpu.usage"},
		{name: "own prefix is removed", tenantID: "acme-corp", metric: "ts-tenant/acme-corp/cpu.usage", want: "cpu.usage"},
		{name: "unprefixed metric is unchanged", tenantID: "acme-corp", metric: "plain-metric", want: "plain-metric"},
		{name: "other tenant's prefix is kept", tenantID: "acme-corp", metric: "ts-tenant/other-corp/cpu.usage", want: "ts-tenant/other-corp/cpu.usage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tsTenantStripMetric(tt.tenantID, tt.metric))
		})
	}
}

func TestTsMetricBelongsToTenant(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		metric   string
		want     bool
	}{
		{name: "empty tenant owns any metric", tenantID: "", metric: "ts-tenant/other/metric", want: true},
		{name: "own prefix belongs", tenantID: "acme-corp", metric: "ts-tenant/acme-corp/cpu.usage", want: true},
		{name: "other tenant's prefix does not belong", tenantID: "acme-corp", metric: "ts-tenant/other-corp/cpu.usage", want: false},
		{name: "unprefixed metric does not belong", tenantID: "acme-corp", metric: "plain-metric", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tsMetricBelongsToTenant(tt.tenantID, tt.metric))
		})
	}
}

func TestTsTenantListPrefix(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		want     string
	}{
		{name: "empty tenant lists everything", tenantID: "", want: ""},
		{name: "tenant lists its own prefix", tenantID: "acme-corp", want: "ts-tenant/acme-corp/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tsTenantListPrefix(tt.tenantID))
		})
	}
}

// =============================================================================
// prepareTimeSeriesTenantContext tests
// =============================================================================

func TestPrepareTimeSeriesTenantContext(t *testing.T) {
	// An unbound context is refused, not read as single-tenant / system
	// reach (go-quicbaas#1150).
	t.Run("unbound context is refused", func(t *testing.T) {
		_, err := prepareTimeSeriesTenantContext(context.Background())
		var missing *tenantctx.MissingTenantError
		require.ErrorAs(t, err, &missing)
	})

	t.Run("valid tenant is returned", func(t *testing.T) {
		ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
		tenantID, err := prepareTimeSeriesTenantContext(ctx)
		require.NoError(t, err)
		assert.Equal(t, "acme-corp", tenantID)
	})

	t.Run("invalid tenant is rejected", func(t *testing.T) {
		ctx := tenantctx.WithTenant(context.Background(), "invalid tenant!@#")
		_, err := prepareTimeSeriesTenantContext(ctx)
		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// =============================================================================
// validateTimeSeriesTenantID tests
// =============================================================================

func TestValidateTimeSeriesTenantID(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		wantErr  bool
	}{
		{name: "empty is single-tenant mode", tenantID: ""},
		{name: "lowercase", tenantID: "acme"},
		{name: "hyphen and digits", tenantID: "tenant-123"},
		{name: "underscore", tenantID: "my_tenant"},
		{name: "mixed case", tenantID: "UPPER_case-123"},
		{name: "exactly 128 characters", tenantID: strings.Repeat("a", 128)},
		{name: "129 characters", tenantID: strings.Repeat("a", 129), wantErr: true},
		{name: "at sign", tenantID: "tenant@corp", wantErr: true},
		{name: "space", tenantID: "ten ant", wantErr: true},
		{name: "slash", tenantID: "tenant/corp", wantErr: true},
		{name: "colon", tenantID: "tenant:id", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTimeSeriesTenantID(tt.tenantID)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
		})
	}
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

// TestTenantScopedMetric_MatchesTheInternalScoping pins the exported form to
// the scoping every tenant-facing write applies, so a platform component that
// writes a tenant's series directly files it where the tenant's reads look.
func TestTenantScopedMetric_MatchesTheInternalScoping(t *testing.T) {
	for _, tenant := range []string{"", "acme"} {
		if got, want := TenantScopedMetric(tenant, "fleet.device.power_watts"), tsTenantScopedMetric(tenant, "fleet.device.power_watts"); got != want {
			t.Fatalf("TenantScopedMetric(%q) = %q, want %q", tenant, got, want)
		}
	}
	if got := TenantScopedMetric("acme", "m"); got != "ts-tenant/acme/m" {
		t.Fatalf("TenantScopedMetric = %q", got)
	}
}

func TestSplitTenantScopedMetric(t *testing.T) {
	tests := []struct {
		name       string
		stored     string
		wantTenant string
		wantMetric string
		wantOK     bool
	}{
		{"a tenant-scoped name", "ts-tenant/acme/fleet.device.power_watts", "acme", "fleet.device.power_watts", true},
		{"a nested metric keeps its slashes", "ts-tenant/acme/app/requests", "acme", "app/requests", true},
		{"an unscoped name is the system tenant's", "cpu.usage", "", "cpu.usage", true},
		{"a scope with no metric", "ts-tenant/acme/", "", "", false},
		{"a scope with no tenant", "ts-tenant//cpu", "", "", false},
		{"a scope with no separator", "ts-tenant/acme", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tenant, metric, ok := SplitTenantScopedMetric(tc.stored)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantTenant, tenant)
			assert.Equal(t, tc.wantMetric, metric)
		})
	}
}

func TestSplitTenantScopedMetric_InvertsTenantScopedMetric(t *testing.T) {
	tenant, metric, ok := SplitTenantScopedMetric(TenantScopedMetric("acme", "fleet.device.utilization"))
	require.True(t, ok)
	assert.Equal(t, "acme", tenant)
	assert.Equal(t, "fleet.device.utilization", metric)
}
