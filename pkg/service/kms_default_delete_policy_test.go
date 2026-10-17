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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// SetDefaultKey + DeleteKeyPolicy.
//
// SetDefaultKey was entirely uncovered; DeleteKeyPolicy lacked the success and
// not-found paths. Both run against the in-memory mock manager / KVStore.
// =============================================================================

func TestKMSService_SetDefaultKey(t *testing.T) {
	ctx := systemTenantCtx()

	t.Run("nil request", func(t *testing.T) {
		svc := newKMSService(newMockKMSManager(), newMockLogger())
		_, err := svc.SetDefaultKey(ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty name", func(t *testing.T) {
		svc := newKMSService(newMockKMSManager(), newMockLogger())
		_, err := svc.SetDefaultKey(ctx, &SetDefaultKeyRequest{})
		require.Error(t, err)
	})

	t.Run("key not found", func(t *testing.T) {
		svc := newKMSService(newMockKMSManager(), newMockLogger())
		_, err := svc.SetDefaultKey(ctx, &SetDefaultKeyRequest{Name: "ghost"})
		require.Error(t, err)
	})

	t.Run("success and previous-default tracking", func(t *testing.T) {
		svc := newKMSService(newMockKMSManager(), newMockLogger())
		svc.keyNames.Store("key-a", "key-a")
		svc.keyNames.Store("key-b", "key-b")

		resp, err := svc.SetDefaultKey(ctx, &SetDefaultKeyRequest{Name: "key-a"})
		require.NoError(t, err)
		assert.True(t, resp.IsDefault)
		assert.Empty(t, resp.PreviousDefault)

		resp2, err := svc.SetDefaultKey(ctx, &SetDefaultKeyRequest{Name: "key-b"})
		require.NoError(t, err)
		assert.Equal(t, "key-a", resp2.PreviousDefault)
	})
}

func TestKMSService_DeleteKeyPolicy(t *testing.T) {
	ctx := systemTenantCtx()

	t.Run("nil request", func(t *testing.T) {
		svc := newKMSService(newMockKMSManager(), newMockLogger())
		_, err := svc.DeleteKeyPolicy(ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty key id", func(t *testing.T) {
		svc := newKMSService(newMockKMSManager(), newMockLogger())
		_, err := svc.DeleteKeyPolicy(ctx, &DeleteKeyPolicyRequest{})
		require.Error(t, err)
	})

	t.Run("key not found", func(t *testing.T) {
		svc := newKMSService(newMockKMSManager(), newMockLogger())
		_, err := svc.DeleteKeyPolicy(ctx, &DeleteKeyPolicyRequest{KeyID: "missing"})
		require.Error(t, err)
	})

	t.Run("success with kvStore", func(t *testing.T) {
		mockMgr := newMockKMSManager()
		mockMgr.keys["k1"] = &mockKeyData{keyID: "k1", algorithm: "AES-256-GCM", version: 1, status: "enabled", createdAt: time.Now()}
		svc := newKMSService(mockMgr, newMockLogger())
		svc.SetKVStore(&mockKVStoreForKMS{data: make(map[string][]byte)})

		resp, err := svc.DeleteKeyPolicy(ctx, &DeleteKeyPolicyRequest{KeyID: "k1"})
		require.NoError(t, err)
		assert.Equal(t, "k1", resp.KeyID)
	})
}
