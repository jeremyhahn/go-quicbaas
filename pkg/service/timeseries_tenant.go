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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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
	if tenantID == "" {
		return metric
	}
	return tsTenantMetricPrefix + tenantID + tsTenantMetricSeparator + metric
}

// tsTenantStripMetric removes the tenant prefix from a metric name.
// If the metric doesn't have a tenant prefix, it is returned as-is.
func tsTenantStripMetric(tenantID, metric string) string {
	if tenantID == "" {
		return metric
	}
	prefix := tsTenantMetricPrefix + tenantID + tsTenantMetricSeparator
	return strings.TrimPrefix(metric, prefix)
}

// tsMetricBelongsToTenant checks if a metric name belongs to the given tenant.
// For single-tenant mode (empty tenantID), all metrics belong.
// For multi-tenant mode, the metric must have the matching tenant prefix.
func tsMetricBelongsToTenant(tenantID, metric string) bool {
	if tenantID == "" {
		return true
	}
	prefix := tsTenantMetricPrefix + tenantID + tsTenantMetricSeparator
	return strings.HasPrefix(metric, prefix)
}

// tsTenantListPrefix returns the prefix to use when listing metrics for a tenant.
// For single-tenant mode, returns empty string (list all).
// For multi-tenant mode, returns "ts-tenant/{tenantID}/".
func tsTenantListPrefix(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return tsTenantMetricPrefix + tenantID + tsTenantMetricSeparator
}

// prepareTimeSeriesTenantContext extracts and validates the tenant ID from the
// request context for time series operations. Returns the validated tenant ID
// or an error if the tenant ID is invalid.
func prepareTimeSeriesTenantContext(ctx context.Context) (string, error) {
	tenantID := iam.TenantFromContext(ctx)
	if err := validateTimeSeriesTenantID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}

// validateTimeSeriesTenantID validates that a tenant ID contains only safe
// characters for use in metric names.
// Empty tenant ID is valid (single-tenant mode).
func validateTimeSeriesTenantID(tenantID string) error {
	if tenantID == "" {
		return nil
	}
	if len(tenantID) > 128 {
		return &errors.DragonError{
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
			return &errors.DragonError{
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
	if tenantID == "" {
		return nil // Single-tenant mode, no access control
	}
	if !tsMetricBelongsToTenant(tenantID, metric) {
		return &errors.DragonError{
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
	if tenantID == "" {
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
