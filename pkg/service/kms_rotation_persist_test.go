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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// persistRotationPolicy / loadRotationPolicy round-trip + branches.
//
// These KV-backed helpers were almost entirely uncovered (15-20%). They are
// driven directly here against the in-memory mockKVStoreForKMS used by the
// other KMS helper tests, covering: no-kvStore short-circuit, nil-policy
// delete, marshal+store, load round-trip, missing-key, and corrupt-data.
// =============================================================================

func TestPersistRotationPolicy(t *testing.T) {
	ctx := context.Background()
	policy := &RotationPolicy{
		Enabled:        true,
		AutoRotate:     true,
		Interval:       24 * time.Hour,
		RetainVersions: 3,
		NextRotation:   time.Now().Add(24 * time.Hour),
	}

	t.Run("no kvStore is a no-op", func(t *testing.T) {
		svc := &kmsServiceImpl{}
		require.NoError(t, svc.persistRotationPolicy(ctx, "k1", policy))
	})

	t.Run("nil policy deletes the key", func(t *testing.T) {
		store := &mockKVStoreForKMS{data: map[string][]byte{
			kmsRotationPolicyKey("k2"): []byte("{}"),
		}}
		svc := &kmsServiceImpl{kvStore: store}

		require.NoError(t, svc.persistRotationPolicy(ctx, "k2", nil))
		_, exists := store.data[kmsRotationPolicyKey("k2")]
		assert.False(t, exists)
	})

	t.Run("stores serialised policy", func(t *testing.T) {
		store := &mockKVStoreForKMS{data: make(map[string][]byte)}
		svc := &kmsServiceImpl{kvStore: store}

		require.NoError(t, svc.persistRotationPolicy(ctx, "k3", policy))
		data, exists := store.data[kmsRotationPolicyKey("k3")]
		require.True(t, exists)
		assert.NotEmpty(t, data)
	})
}

func TestLoadRotationPolicy(t *testing.T) {
	ctx := context.Background()
	policy := &RotationPolicy{
		Enabled:        true,
		AutoRotate:     true,
		Interval:       12 * time.Hour,
		RetainVersions: 5,
	}

	t.Run("no kvStore returns nil", func(t *testing.T) {
		svc := &kmsServiceImpl{}
		got, err := svc.loadRotationPolicy(ctx, "k1")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("round-trip", func(t *testing.T) {
		store := &mockKVStoreForKMS{data: make(map[string][]byte)}
		svc := &kmsServiceImpl{kvStore: store}

		require.NoError(t, svc.persistRotationPolicy(ctx, "k2", policy))

		got, err := svc.loadRotationPolicy(ctx, "k2")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.True(t, got.Enabled)
		assert.True(t, got.AutoRotate)
		assert.Equal(t, 12*time.Hour, got.Interval)
		assert.Equal(t, 5, got.RetainVersions)
	})

	t.Run("missing key returns nil", func(t *testing.T) {
		store := &mockKVStoreForKMS{data: make(map[string][]byte)}
		svc := &kmsServiceImpl{kvStore: store}

		got, err := svc.loadRotationPolicy(ctx, "absent")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("corrupt data returns error", func(t *testing.T) {
		store := &mockKVStoreForKMS{data: map[string][]byte{
			kmsRotationPolicyKey("bad"): []byte("not-json"),
		}}
		svc := &kmsServiceImpl{kvStore: store}

		got, err := svc.loadRotationPolicy(ctx, "bad")
		require.Error(t, err)
		assert.Nil(t, got)
	})
}

// =============================================================================
// serviceRotationToKMS / kmsRotationToService conversions.
//
// Covers the nil-input branch (previously uncovered) and value mapping.
// =============================================================================

func TestRotationPolicyConversions(t *testing.T) {
	t.Run("serviceRotationToKMS nil", func(t *testing.T) {
		assert.Nil(t, serviceRotationToKMS(nil))
	})

	t.Run("kmsRotationToService nil", func(t *testing.T) {
		assert.Nil(t, kmsRotationToService(nil))
	})

	t.Run("serviceRotationToKMS maps fields", func(t *testing.T) {
		next := time.Now().Add(time.Hour)
		out := serviceRotationToKMS(&RotationPolicy{
			Enabled:        true,
			Interval:       6 * time.Hour,
			NextRotation:   next,
			RetainVersions: 2,
			AutoRotate:     true,
		})
		require.NotNil(t, out)
		assert.True(t, out.Enabled)
		assert.True(t, out.AutoRotate)
		assert.Equal(t, 6*time.Hour, out.Interval)
		assert.Equal(t, 2, out.RetainVersions)
		assert.Equal(t, next, out.NextRotation)
	})

	t.Run("kmsRotationToService maps fields", func(t *testing.T) {
		next := time.Now().Add(2 * time.Hour)
		out := kmsRotationToService(&kms.RotationPolicy{
			Enabled:        true,
			Interval:       8 * time.Hour,
			NextRotation:   next,
			RetainVersions: 4,
			AutoRotate:     false,
		})
		require.NotNil(t, out)
		assert.True(t, out.Enabled)
		assert.False(t, out.AutoRotate)
		assert.Equal(t, 8*time.Hour, out.Interval)
		assert.Equal(t, 4, out.RetainVersions)
		assert.Equal(t, next, out.NextRotation)
	})
}
