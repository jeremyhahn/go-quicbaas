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

//go:build integration

package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestKeyspaceResolver_Integration_TenantIsolation(t *testing.T) {
	resolver := service.NewKeyspaceResolver()

	t.Run("different tenants get different key prefixes", func(t *testing.T) {
		ctx1 := iam.ContextWithTenant(context.Background(), "tenant-a")
		ctx2 := iam.ContextWithTenant(context.Background(), "tenant-b")

		key1, err := resolver.BuildKey(ctx1, "User", 123)
		require.NoError(t, err)

		key2, err := resolver.BuildKey(ctx2, "User", 123)
		require.NoError(t, err)

		// Same entity type and ID but different tenants = different keys
		assert.NotEqual(t, key1, key2)
		assert.Contains(t, string(key1), "tenant-a")
		assert.Contains(t, string(key2), "tenant-b")
	})

	t.Run("tenant cannot access other tenant keys", func(t *testing.T) {
		ctxA := iam.ContextWithTenant(context.Background(), "tenant-a")

		// Build a key for tenant-b
		ctxB := iam.ContextWithTenant(context.Background(), "tenant-b")
		keyB, err := resolver.BuildKey(ctxB, "User", 123)
		require.NoError(t, err)

		// Tenant-a should not have access to tenant-b's key
		err = resolver.ValidateAccess(ctxA, keyB)
		assert.Error(t, err)
	})

	t.Run("system keyspace blocked from tenant access", func(t *testing.T) {
		ctx := iam.ContextWithTenant(context.Background(), "tenant-a")

		systemKey := []byte("__sys:cluster/config")
		assert.True(t, resolver.IsSystemKey(systemKey))

		err := resolver.ValidateAccess(ctx, systemKey)
		assert.Error(t, err)
	})

	t.Run("legacy mode works without tenant context", func(t *testing.T) {
		ctx := context.Background() // No tenant

		key, err := resolver.BuildKey(ctx, "User", 456)
		require.NoError(t, err)

		// Legacy key format: User:456
		assert.Equal(t, "User:456", string(key))
	})
}

func TestKeyspaceResolver_Integration_KeyParsing(t *testing.T) {
	resolver := service.NewKeyspaceResolver()

	t.Run("parse tenant key", func(t *testing.T) {
		ctx := iam.ContextWithTenant(context.Background(), "my-tenant")
		key, err := resolver.BuildKey(ctx, "Organization", 999)
		require.NoError(t, err)

		tenantID, entityType, id, err := resolver.ParseKey(key)
		require.NoError(t, err)
		assert.Equal(t, "my-tenant", tenantID)
		assert.Equal(t, "Organization", entityType)
		assert.Equal(t, uint64(999), id)
	})

	t.Run("parse legacy key", func(t *testing.T) {
		key := []byte("Product:42")

		tenantID, entityType, id, err := resolver.ParseKey(key)
		require.NoError(t, err)
		assert.Empty(t, tenantID)
		assert.Equal(t, "Product", entityType)
		assert.Equal(t, uint64(42), id)
	})
}
