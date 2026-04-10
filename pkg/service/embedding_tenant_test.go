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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// embeddingTenantScopedCacheKey tests
// =============================================================================

func TestEmbeddingTenantScopedCacheKey_EmptyTenant(t *testing.T) {
	// Single-tenant mode: text is returned unchanged
	text := "hello world"
	result := embeddingTenantScopedCacheKey("", text)
	assert.Equal(t, text, result)
}

func TestEmbeddingTenantScopedCacheKey_WithTenant(t *testing.T) {
	// Multi-tenant mode: text is prefixed with tenant namespace
	result := embeddingTenantScopedCacheKey("acme-corp", "hello world")
	assert.Equal(t, "emb-tenant/acme-corp/hello world", result)
}

func TestEmbeddingTenantScopedCacheKey_PreservesSpecialChars(t *testing.T) {
	// Text with special characters is preserved
	result := embeddingTenantScopedCacheKey("tenant-123", "text with spaces & symbols!")
	assert.Equal(t, "emb-tenant/tenant-123/text with spaces & symbols!", result)
}

// =============================================================================
// embeddingTenantStripCacheKey tests
// =============================================================================

func TestEmbeddingTenantStripCacheKey_EmptyTenant(t *testing.T) {
	// Single-tenant mode: no stripping
	result := embeddingTenantStripCacheKey("", "hello world")
	assert.Equal(t, "hello world", result)
}

func TestEmbeddingTenantStripCacheKey_WithTenant(t *testing.T) {
	// Multi-tenant mode: tenant prefix is removed
	result := embeddingTenantStripCacheKey("acme-corp", "emb-tenant/acme-corp/hello world")
	assert.Equal(t, "hello world", result)
}

func TestEmbeddingTenantStripCacheKey_NoMatchingPrefix(t *testing.T) {
	// Key without tenant prefix is returned as-is
	result := embeddingTenantStripCacheKey("acme-corp", "plain-text")
	assert.Equal(t, "plain-text", result)
}

func TestEmbeddingTenantStripCacheKey_WrongTenant(t *testing.T) {
	// Key belonging to a different tenant is not stripped
	result := embeddingTenantStripCacheKey("acme-corp", "emb-tenant/other-corp/hello world")
	assert.Equal(t, "emb-tenant/other-corp/hello world", result)
}

// =============================================================================
// embeddingCacheKeyBelongsToTenant tests
// =============================================================================

func TestEmbeddingCacheKeyBelongsToTenant_EmptyTenant(t *testing.T) {
	// Single-tenant mode: all keys belong
	assert.True(t, embeddingCacheKeyBelongsToTenant("", "anything"))
	assert.True(t, embeddingCacheKeyBelongsToTenant("", "emb-tenant/other/key"))
}

func TestEmbeddingCacheKeyBelongsToTenant_MatchingTenant(t *testing.T) {
	// Multi-tenant mode: key with matching prefix belongs
	key := embeddingTenantScopedCacheKey("acme-corp", "hello")
	assert.True(t, embeddingCacheKeyBelongsToTenant("acme-corp", key))
}

func TestEmbeddingCacheKeyBelongsToTenant_WrongTenant(t *testing.T) {
	// Multi-tenant mode: key with different prefix does not belong
	key := embeddingTenantScopedCacheKey("other-corp", "hello")
	assert.False(t, embeddingCacheKeyBelongsToTenant("acme-corp", key))
}

func TestEmbeddingCacheKeyBelongsToTenant_UnprefixedKey(t *testing.T) {
	// Multi-tenant mode: unprefixed key does not belong
	assert.False(t, embeddingCacheKeyBelongsToTenant("acme-corp", "plain-text"))
}

// =============================================================================
// prepareEmbeddingTenantContext tests
// =============================================================================

func TestPrepareEmbeddingTenantContext_EmptyContext(t *testing.T) {
	// No tenant in context returns empty string (single-tenant mode)
	ctx := context.Background()
	tenantID, err := prepareEmbeddingTenantContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, "", tenantID)
}

func TestPrepareEmbeddingTenantContext_ValidTenant(t *testing.T) {
	// Valid tenant ID is returned
	ctx := iam.ContextWithTenant(context.Background(), "acme-corp")
	tenantID, err := prepareEmbeddingTenantContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, "acme-corp", tenantID)
}

func TestPrepareEmbeddingTenantContext_InvalidTenant(t *testing.T) {
	// Invalid tenant ID returns an error
	ctx := iam.ContextWithTenant(context.Background(), "invalid tenant!@#")
	_, err := prepareEmbeddingTenantContext(ctx)
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
}

func TestPrepareEmbeddingTenantContext_TooLongTenant(t *testing.T) {
	// Tenant ID exceeding 128 characters returns an error
	longTenant := strings.Repeat("a", 129)
	ctx := iam.ContextWithTenant(context.Background(), longTenant)
	_, err := prepareEmbeddingTenantContext(ctx)
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
}

// =============================================================================
// validateEmbeddingTenantID tests
// =============================================================================

func TestValidateEmbeddingTenantID_Empty(t *testing.T) {
	// Empty tenant ID is valid (single-tenant mode)
	err := validateEmbeddingTenantID("")
	assert.NoError(t, err)
}

func TestValidateEmbeddingTenantID_Valid(t *testing.T) {
	// Valid tenant IDs with alphanumeric, hyphens, and underscores
	validIDs := []string{"acme", "tenant-123", "my_tenant", "UPPER_case-123"}
	for _, id := range validIDs {
		err := validateEmbeddingTenantID(id)
		assert.NoError(t, err, "tenant ID %q should be valid", id)
	}
}

func TestValidateEmbeddingTenantID_InvalidChars(t *testing.T) {
	// Tenant IDs with invalid characters are rejected
	invalidIDs := []string{"tenant@corp", "ten ant", "tenant/corp", "tenant:id"}
	for _, id := range invalidIDs {
		err := validateEmbeddingTenantID(id)
		require.Error(t, err, "tenant ID %q should be invalid", id)
		var dragonErr *errors.DragonError
		require.ErrorAs(t, err, &dragonErr)
		assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
	}
}

func TestValidateEmbeddingTenantID_TooLong(t *testing.T) {
	// Tenant ID at exactly 128 characters is valid
	exact128 := strings.Repeat("a", 128)
	err := validateEmbeddingTenantID(exact128)
	assert.NoError(t, err)

	// Tenant ID at 129 characters is too long
	tooLong := strings.Repeat("a", 129)
	err = validateEmbeddingTenantID(tooLong)
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
}

// =============================================================================
// enforceEmbeddingTenantAccess tests
// =============================================================================

func TestEnforceEmbeddingTenantAccess_EmptyTenant(t *testing.T) {
	// Single-tenant mode: always allowed
	err := enforceEmbeddingTenantAccess("", "any-key", "GenerateEmbedding")
	assert.NoError(t, err)
}

func TestEnforceEmbeddingTenantAccess_MatchingTenant(t *testing.T) {
	// Multi-tenant mode: matching tenant prefix is allowed
	key := embeddingTenantScopedCacheKey("acme-corp", "hello")
	err := enforceEmbeddingTenantAccess("acme-corp", key, "GenerateEmbedding")
	assert.NoError(t, err)
}

func TestEnforceEmbeddingTenantAccess_CrossTenant(t *testing.T) {
	// Multi-tenant mode: cross-tenant access is denied
	key := embeddingTenantScopedCacheKey("other-corp", "hello")
	err := enforceEmbeddingTenantAccess("acme-corp", key, "GenerateEmbedding")
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, dragonErr.Code)
}

func TestEnforceEmbeddingTenantAccess_UnprefixedKey(t *testing.T) {
	// Multi-tenant mode: unprefixed key is denied
	err := enforceEmbeddingTenantAccess("acme-corp", "plain-text", "GenerateEmbedding")
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrCrossTenantAccess, dragonErr.Code)
}

// =============================================================================
// Error type tests
// =============================================================================

func TestEmbeddingTenantValidationError_Error(t *testing.T) {
	err := &EmbeddingTenantValidationError{
		TenantID: "bad-id",
		Reason:   "contains invalid characters",
	}
	msg := err.Error()
	assert.Contains(t, msg, "bad-id")
	assert.Contains(t, msg, "contains invalid characters")
}

func TestEmbeddingTenantValidationError_EmptyFields(t *testing.T) {
	err := &EmbeddingTenantValidationError{}
	msg := err.Error()
	assert.Contains(t, msg, "embedding tenant validation failed")
}

func TestEmbeddingCrossTenantAccessError_Error(t *testing.T) {
	err := &EmbeddingCrossTenantAccessError{
		RequestingTenant: "tenant-a",
		OwnerTenant:      "tenant-b",
		Operation:        "GenerateEmbedding",
	}
	msg := err.Error()
	assert.Contains(t, msg, "tenant-a")
	assert.Contains(t, msg, "tenant-b")
	assert.Contains(t, msg, "GenerateEmbedding")
}

func TestEmbeddingCrossTenantAccessError_EmptyFields(t *testing.T) {
	err := &EmbeddingCrossTenantAccessError{}
	msg := err.Error()
	assert.Contains(t, msg, "embedding cross-tenant access denied")
}
