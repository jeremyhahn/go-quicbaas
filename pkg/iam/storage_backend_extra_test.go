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

package iam

import (
	"context"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetSettings returns empty settings for an unset tenant and surfaces a decode
// error for corrupt data; UpdateSettings rejects an unknown tenant.
func TestBackendStorage_SettingsEdgeCases(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	// Unset settings -> empty struct, no error.
	settings, err := storage.GetSettings(ctx, "no-tenant")
	require.NoError(t, err)
	assert.NotNil(t, settings)

	// UpdateSettings on a non-existent tenant fails closed.
	err = storage.UpdateSettings(ctx, "ghost-tenant", &TenantSettings{MFARequired: true})
	assert.Error(t, err)
	assert.True(t, dberrors.IsNotFound(err))

	// Corrupt settings blob surfaces an internal decode error.
	require.NoError(t, kv.Put(ctx, tenantSettingsKey("corrupt"), []byte("{not-json")))
	_, err = storage.GetSettings(ctx, "corrupt")
	assert.Error(t, err)
}

// GetQuota returns empty quota when unset and decodes corrupt data as an error;
// IncrementQuotaUsage covers the policy and app counters.
func TestBackendStorage_QuotaEdgeCases(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	quota, err := storage.GetQuota(ctx, "no-tenant")
	require.NoError(t, err)
	assert.NotNil(t, quota)

	require.NoError(t, storage.IncrementQuotaUsage(ctx, "t", QuotaResourcePolicies, 3))
	require.NoError(t, storage.IncrementQuotaUsage(ctx, "t", QuotaResourceApps, 4))
	got, err := storage.GetQuota(ctx, "t")
	require.NoError(t, err)
	assert.Equal(t, int64(3), got.CurrentPolicies)
	assert.Equal(t, int64(4), got.CurrentApps)

	// An unknown resource is a no-op (still persists without error).
	require.NoError(t, storage.IncrementQuotaUsage(ctx, "t", "unknown-resource", 9))

	require.NoError(t, kv.Put(ctx, tenantQuotaKey("corrupt"), []byte("{bad")))
	_, err = storage.GetQuota(ctx, "corrupt")
	assert.Error(t, err)
}

// StoreToken writes the refresh-token value index and RevokeUserTokens revokes
// every token a user owns.
func TestBackendStorage_RefreshTokenIndexAndRevokeUser(t *testing.T) {
	ctx := ContextWithTenant(context.Background(), "tenant-1")
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)

	refresh := &Token{
		JTI:       "refresh-1",
		Subject:   "user-1",
		TenantID:  "tenant-1",
		TokenType: TokenTypeRefresh,
		Expires:   time.Now().Add(time.Hour),
		Issued:    time.Now(),
	}
	access := &Token{
		JTI:       "access-1",
		Subject:   "user-1",
		TenantID:  "tenant-1",
		TokenType: TokenTypeAccess,
		Expires:   time.Now().Add(time.Hour),
		Issued:    time.Now(),
	}
	require.NoError(t, storage.StoreToken(ctx, refresh))
	require.NoError(t, storage.StoreToken(ctx, access))

	byUser, err := storage.ListTokensByUser(ctx, "user-1")
	require.NoError(t, err)
	assert.Len(t, byUser, 2)

	require.NoError(t, storage.RevokeUserTokens(ctx, "user-1"))

	revoked, err := storage.IsTokenRevoked(ctx, "refresh-1")
	require.NoError(t, err)
	assert.True(t, revoked)
	revoked, err = storage.IsTokenRevoked(ctx, "access-1")
	require.NoError(t, err)
	assert.True(t, revoked)
}
