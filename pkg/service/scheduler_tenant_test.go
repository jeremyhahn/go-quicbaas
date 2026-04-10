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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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
// schedulerTenantExtractID tests
// =============================================================================

func TestSchedulerTenantExtractID_NoTenant(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", schedulerTenantExtractID(ctx))
}

func TestSchedulerTenantExtractID_WithTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "my-tenant")
	assert.Equal(t, "my-tenant", schedulerTenantExtractID(ctx))
}

func TestSchedulerTenantExtractID_EmptyTenantValue(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "")
	assert.Equal(t, "", schedulerTenantExtractID(ctx))
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
// schedulerTenantPrepareContext tests
// =============================================================================

func TestSchedulerTenantPrepareContext_NoTenant(t *testing.T) {
	ctx := context.Background()
	tenantID, err := schedulerTenantPrepareContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "", tenantID)
}

func TestSchedulerTenantPrepareContext_ValidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "acme-corp")
	tenantID, err := schedulerTenantPrepareContext(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestSchedulerTenantPrepareContext_InvalidTenant(t *testing.T) {
	ctx := iam.ContextWithTenant(context.Background(), "invalid/tenant")
	_, err := schedulerTenantPrepareContext(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

// =============================================================================
// schedulerTenantResolveID tests
// =============================================================================

func TestSchedulerTenantResolveID_ContextTenantPreferred(t *testing.T) {
	// When IAM context has a tenant, it takes precedence over the request tenant
	ctx := iam.ContextWithTenant(context.Background(), "context-tenant")
	tenantID, err := schedulerTenantResolveID(ctx, "request-tenant")
	assert.NoError(t, err)
	assert.Equal(t, "context-tenant", tenantID)
}

func TestSchedulerTenantResolveID_FallbackToRequest(t *testing.T) {
	// When IAM context has no tenant, the request tenant is used as fallback
	ctx := context.Background()
	tenantID, err := schedulerTenantResolveID(ctx, "request-tenant")
	assert.NoError(t, err)
	assert.Equal(t, "request-tenant", tenantID)
}

func TestSchedulerTenantResolveID_BothEmpty(t *testing.T) {
	// When neither context nor request has a tenant, empty is returned (single-tenant mode)
	ctx := context.Background()
	tenantID, err := schedulerTenantResolveID(ctx, "")
	assert.NoError(t, err)
	assert.Equal(t, "", tenantID)
}

func TestSchedulerTenantResolveID_EmptyContextWithEmptyString(t *testing.T) {
	// An empty string in context is treated as no tenant
	ctx := iam.ContextWithTenant(context.Background(), "")
	tenantID, err := schedulerTenantResolveID(ctx, "fallback")
	assert.NoError(t, err)
	assert.Equal(t, "fallback", tenantID)
}

func TestSchedulerTenantResolveID_InvalidContextTenant(t *testing.T) {
	// Invalid tenant in context should fail validation
	ctx := iam.ContextWithTenant(context.Background(), "bad/tenant")
	_, err := schedulerTenantResolveID(ctx, "valid-tenant")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestSchedulerTenantResolveID_InvalidRequestTenant(t *testing.T) {
	// Invalid tenant in request (used as fallback) should fail validation
	ctx := context.Background()
	_, err := schedulerTenantResolveID(ctx, "bad@tenant")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestSchedulerTenantResolveID_ValidRequestTenantFormats(t *testing.T) {
	// Various valid tenant ID formats should be accepted when falling back to request
	ctx := context.Background()

	tests := []struct {
		name     string
		tenantID string
	}{
		{"alphanumeric", "default"},
		{"with-hyphens", "acme-corp"},
		{"with-underscores", "my_tenant"},
		{"mixed", "Tenant-123_abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenantID, err := schedulerTenantResolveID(ctx, tt.tenantID)
			assert.NoError(t, err)
			assert.Equal(t, tt.tenantID, tenantID)
		})
	}
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
