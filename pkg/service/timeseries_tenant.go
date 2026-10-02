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
// This file implements multi-tenant time series isolation. When a tenantID is
// present in the request context, all metric names are scoped to that tenant's
// namespace so tenants cannot see or modify each other's time series data.
// When tenantID is empty, the system operates in single-tenant mode with no
// behavioral changes (full backward compatibility).
package service

import (
	"context"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// tsTenantMetricPrefix is the storage prefix for tenant-scoped time series metrics.
const tsTenantMetricPrefix = "ts-tenant/"

// tsTenantMetricSeparator separates the tenant ID from the metric name.
const tsTenantMetricSeparator = "/"

// TimeSeriesTenantValidationError indicates a tenant ID validation failure
// in the time series service.
type TimeSeriesTenantValidationError struct {
	TenantID string
	Reason   string
}

// Error implements the error interface.
func (e *TimeSeriesTenantValidationError) Error() string {
	return "timeseries tenant validation failed: tenant=" + e.TenantID + " reason=" + e.Reason
}

// TimeSeriesCrossTenantAccessError indicates a cross-tenant access attempt
// in the time series service.
type TimeSeriesCrossTenantAccessError struct {
	RequestingTenant string
	OwnerTenant      string
	Metric           string
	Operation        string
}

// Error implements the error interface.
func (e *TimeSeriesCrossTenantAccessError) Error() string {
	return "timeseries cross-tenant access denied: requesting=" + e.RequestingTenant +
		" owner=" + e.OwnerTenant + " metric=" + e.Metric + " op=" + e.Operation
}

// tsTenantScopedMetric returns a tenant-scoped metric name.
// For single-tenant mode (empty tenantID), the metric is returned as-is.
// For multi-tenant mode, the metric is prefixed: "ts-tenant/{tenantID}/{metric}".
func tsTenantScopedMetric(tenantID, metric string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return metric
	}
	return tsTenantMetricPrefix + tenantID + tsTenantMetricSeparator + metric
}

// TenantScopedMetric returns the name metric is filed under for tenantID —
// the same scoping every tenant-facing time-series write applies — so a
// platform component writing a tenant's series directly to storage files it
// where the tenant's own reads look.
func TenantScopedMetric(tenantID, metric string) string {
	return tsTenantScopedMetric(tenantID, metric)
}

// SplitTenantScopedMetric is TenantScopedMetric's inverse: it returns the
// tenant a stored metric name is filed under and the name the tenant knows it
// by. A name with no tenant scope belongs to the system tenant and is returned
// whole with an empty tenant. ok is false for a malformed scoped name.
func SplitTenantScopedMetric(stored string) (tenantID, metric string, ok bool) {
	rest, scoped := strings.CutPrefix(stored, tsTenantMetricPrefix)
	if !scoped {
		return "", stored, true
	}
	tenantID, metric, found := strings.Cut(rest, tsTenantMetricSeparator)
	if !found || tenantID == "" || metric == "" {
		return "", "", false
	}
	return tenantID, metric, true
}

// tsTenantStripMetric removes the tenant prefix from a metric name.
// If the metric doesn't have a tenant prefix, it is returned as-is.
func tsTenantStripMetric(tenantID, metric string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return metric
	}
	prefix := tsTenantMetricPrefix + tenantID + tsTenantMetricSeparator
	return strings.TrimPrefix(metric, prefix)
}

// tsMetricBelongsToTenant checks if a metric name belongs to the given tenant.
// For single-tenant mode (empty tenantID), all metrics belong.
// For multi-tenant mode, the metric must have the matching tenant prefix.
func tsMetricBelongsToTenant(tenantID, metric string) bool {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return true
	}
	prefix := tsTenantMetricPrefix + tenantID + tsTenantMetricSeparator
	return strings.HasPrefix(metric, prefix)
}

// tsTenantListPrefix returns the prefix to use when listing metrics for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "ts-tenant/{tenantID}/".
func tsTenantListPrefix(tenantID string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return ""
	}
	return tsTenantMetricPrefix + tenantID + tsTenantMetricSeparator
}

// prepareTimeSeriesTenantContext extracts and validates the tenant ID from the
// request context for time series operations. Returns the validated tenant ID
// or an error if the tenant ID is invalid.
func prepareTimeSeriesTenantContext(ctx context.Context) (string, error) {
	tenantID, err := tenantctx.FromContext(ctx, "timeseries")
	if err != nil {
		return "", err
	}
	if tenantctx.IsSystem(tenantID) {
		return "", nil // the platform scope; see prepareTenantContext
	}
	if err := validateTimeSeriesTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}

// tenantScopedMetric resolves the caller's tenant from ctx and returns the
// name metric is stored under for that tenant. It is THE scoping rule of the
// time-series service: every write, read, delete and retention call files and
// looks up a metric through it, so what a tenant writes is what it reads.
func tenantScopedMetric(ctx context.Context, op, metric string) (tenantID, scoped string, err error) {
	tenantID, err = prepareTimeSeriesTenantContext(ctx)
	if err != nil {
		// Already coded: ErrForbidden for a missing tenant, ErrInvalidInput
		// for a malformed one.
		return "", "", err
	}
	return tenantID, tsTenantScopedMetric(tenantID, metric), nil
}

// validateTimeSeriesTenantID validates that a tenant ID contains only safe
// characters for use in metric names.
// Empty tenant ID is valid (single-tenant mode).
func validateTimeSeriesTenantID(tenantID string) error {
	if tenantID == "" {
		return nil
	}
	if len(tenantID) > 128 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "timeseries.validateTenantID",
			Err: &TimeSeriesTenantValidationError{
				TenantID: tenantID,
				Reason:   "tenant ID exceeds maximum length of 128 characters",
			},
		}
	}
	for _, ch := range tenantID {
		if !isValidTenantChar(ch) {
			return &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "timeseries.validateTenantID",
				Err: &TimeSeriesTenantValidationError{
					TenantID: tenantID,
					Reason:   "tenant ID contains invalid character; only alphanumeric, hyphens, and underscores are allowed",
				},
			}
		}
	}
	return nil
}

// enforceTimeSeriesTenantAccess validates that the requesting tenant has access
// to the metric. Returns nil if access is allowed, or an error if the tenant
// doesn't own the metric.
func enforceTimeSeriesTenantAccess(tenantID, metric, operation string) error {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil // Single-tenant mode / system tenant, no access control
	}
	if !tsMetricBelongsToTenant(tenantID, metric) {
		return &errors.QBError{
			Code: errors.ErrCrossTenantAccess,
			Op:   "timeseries." + operation,
			Err: &TimeSeriesCrossTenantAccessError{
				RequestingTenant: tenantID,
				OwnerTenant:      "unknown",
				Metric:           metric,
				Operation:        operation,
			},
		}
	}
	return nil
}

// tsFilterMetricsByTenant filters a list of metric names to only those belonging
// to the given tenant. For single-tenant mode (empty tenantID), all metrics are
// returned. For multi-tenant mode, only metrics with the matching tenant prefix
// are returned, with the prefix stripped from the names.
func tsFilterMetricsByTenant(tenantID string, metrics []string) []string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return metrics
	}
	prefix := tsTenantMetricPrefix + tenantID + tsTenantMetricSeparator
	filtered := make([]string, 0, len(metrics))
	for _, m := range metrics {
		if strings.HasPrefix(m, prefix) {
			filtered = append(filtered, strings.TrimPrefix(m, prefix))
		}
	}
	return filtered
}
