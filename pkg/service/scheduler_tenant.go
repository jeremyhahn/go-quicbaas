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
// This file implements multi-tenant scheduler isolation. When a tenantID is
// present in the request context, all scheduler job operations are scoped to
// that tenant's namespace. When tenantID is empty, the system operates in
// single-tenant mode with no behavioral changes (full backward compatibility).
package service

import (
	"context"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// schedulerTenantJobPrefix is the storage prefix for tenant-scoped scheduler job IDs.
const schedulerTenantJobPrefix = "tenant/"

// schedulerTenantJobSuffix follows the tenant ID in the job namespace.
const schedulerTenantJobSuffix = "/"

// schedulerTenantScopedJobName returns a tenant-scoped job name if tenantID is non-empty.
// For single-tenant mode (empty tenantID), the job name is returned as-is.
// Format: "tenant/{tenantID}/{jobName}".
func schedulerTenantScopedJobName(tenantID, jobName string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return jobName
	}
	return schedulerTenantJobPrefix + tenantID + schedulerTenantJobSuffix + jobName
}

// schedulerTenantStripJobName removes the tenant prefix from a job name.
// If the job name doesn't have a tenant prefix, it is returned as-is.
func schedulerTenantStripJobName(tenantID, jobName string) string {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return jobName
	}
	prefix := schedulerTenantJobPrefix + tenantID + schedulerTenantJobSuffix
	return strings.TrimPrefix(jobName, prefix)
}

// schedulerTenantJobBelongsTo checks if a job's TargetTenantID matches the given tenant.
// For single-tenant mode (empty tenantID), all jobs belong.
// For multi-tenant mode, the job must have a matching TargetTenantID.
func schedulerTenantJobBelongsTo(tenantID, jobTargetTenantID string) bool {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return true
	}
	return jobTargetTenantID == tenantID
}

// schedulerTenantExtractID extracts the tenant ID from the context.
// Returns empty string for single-tenant mode.
func schedulerTenantExtractID(ctx context.Context) string {
	return iam.TenantFromContext(ctx)
}

// schedulerTenantValidateID validates that a tenant ID contains only safe characters.
// Empty tenant ID is valid (single-tenant mode).
func schedulerTenantValidateID(tenantID string) error {
	if tenantID == "" {
		return nil
	}
	if len(tenantID) > 128 {
		return NewError(errors.ErrInvalidInput, "scheduler.validateTenantID",
			"tenant ID exceeds maximum length of 128 characters")
	}
	for _, ch := range tenantID {
		if !isValidTenantChar(ch) {
			return NewError(errors.ErrInvalidInput, "scheduler.validateTenantID",
				"tenant ID contains invalid character; only alphanumeric, hyphens, and underscores are allowed")
		}
	}
	return nil
}

// schedulerTenantEnforceAccess validates that the requesting tenant has access to the job.
// Returns nil if access is allowed, or an error if the tenant doesn't own the job.
func schedulerTenantEnforceAccess(tenantID, jobTargetTenantID, operation string) error {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return nil // Single-tenant mode / system tenant, no access control
	}
	if !schedulerTenantJobBelongsTo(tenantID, jobTargetTenantID) {
		return NewError(errors.ErrCrossTenantAccess, "scheduler."+operation,
			"job does not belong to the requesting tenant")
	}
	return nil
}

// schedulerTenantPrepareContext extracts and validates the tenant ID from context.
// Returns the validated tenant ID or an error if the tenant ID is invalid.
func schedulerTenantPrepareContext(ctx context.Context) (string, error) {
	tenantID := schedulerTenantExtractID(ctx)
	if err := schedulerTenantValidateID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}

// schedulerTenantResolveID resolves the effective tenant ID by preferring the
// IAM context tenant and falling back to the request-provided tenant ID.
// This ensures that storage key construction uses the same tenant namespace
// regardless of whether the tenant was injected into the Go context by a
// transport-level interceptor or passed explicitly in the request payload.
func schedulerTenantResolveID(ctx context.Context, requestTenantID string) (string, error) {
	tenantID := schedulerTenantExtractID(ctx)
	if tenantID == "" {
		tenantID = requestTenantID
	}
	if err := schedulerTenantValidateID(tenantID); err != nil {
		return "", err
	}
	return tenantID, nil
}
