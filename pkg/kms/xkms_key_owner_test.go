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

package kms

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-xkms/pkg/api/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
)

// newOwnerTestBackend boots a backend factory over store the way the
// server does and returns its software XKMSBackend.
func newOwnerTestBackend(t *testing.T, ctx context.Context, store xkmssdk.StorageBackend) *XKMSBackend {
	t.Helper()
	factory, err := NewBackendFactory(&BackendFactoryConfig{Logger: &mockLogger{}, KeyNameStore: store})
	require.NoError(t, err)
	require.NoError(t, factory.InitializeFromXKMS(ctx, nil))
	b, err := factory.Registry().Get("software")
	require.NoError(t, err)
	kb, ok := b.(*XKMSBackend)
	require.True(t, ok)
	return kb
}

func listedKeyIDs(t *testing.T, ctx context.Context, kb *XKMSBackend, prefix string) []string {
	t.Helper()
	keys, _, err := kb.ListKeys(ctx, prefix, 0)
	require.NoError(t, err)
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.KeyID)
	}
	return ids
}

// TestXKMSBackend_NativeSystemKeyNeverResolvesAsTenantKey: the node's
// native go-xkms service writes the same key store under any valid literal
// name, including one shaped like an encoded tenant key name. Such a key must
// never resolve, or list, as the tenant key it decodes to; only names this
// backend wrote (and recorded) do.
func TestXKMSBackend_NativeSystemKeyNeverResolvesAsTenantKey(t *testing.T) {
	ctx := context.Background()
	store := newKeyNameGuardXKMS(t)

	// Clean boot: the marker is written.
	kb := newOwnerTestBackend(t, ctx, store)

	// A system caller of the native service plants an encoded-shaped name
	// after boot.
	svc, err := xkmssdk.Get()
	require.NoError(t, err)
	const planted = "qbx.dGVuYW50L2FjbWUvaw"
	require.Equal(t, "tenant/acme/k", keyIDFromXKMSName(planted))
	_, err = svc.GenerateKey(ctx, &transport.GenerateKeyRequest{
		KeyID: planted, Backend: "software", KeyType: "signing", Algorithm: "ed25519",
	})
	require.NoError(t, err)

	// Tenant acme's "k" is not the native key.
	_, err = kb.GetKeyMetadata(ctx, "tenant/acme/k")
	require.ErrorIs(t, err, ErrKeyNotFound)
	_, err = kb.ExportPublicKey(ctx, "tenant/acme/k")
	require.Error(t, err)
	require.NotContains(t, listedKeyIDs(t, ctx, kb, ""), "tenant/acme/k")
	require.Empty(t, listedKeyIDs(t, ctx, kb, "tenant/"))

	// The backend's own encoded key still works, and its record persists
	// across a restart (a fresh backend with an empty cache).
	_, err = kb.CreateKey(ctx, kb.algorithmToKeyAttributes("tenant/beta/k", AlgAES256GCM))
	require.NoError(t, err)
	kb2 := newOwnerTestBackend(t, ctx, store)
	_, err = kb2.GetKeyMetadata(ctx, "tenant/beta/k")
	require.NoError(t, err)
	require.Contains(t, listedKeyIDs(t, ctx, kb2, "tenant/"), "tenant/beta/k")

	// Delete releases the name: a later native key under it is not the
	// deleted tenant key.
	require.NoError(t, kb2.DeleteKey(ctx, "tenant/beta/k"))
	_, err = svc.GenerateKey(ctx, &transport.GenerateKeyRequest{
		KeyID: xkmsKeyName("tenant/beta/k"), Backend: "software", KeyType: "signing", Algorithm: "ed25519",
	})
	require.NoError(t, err)
	_, err = kb2.GetKeyMetadata(ctx, "tenant/beta/k")
	require.ErrorIs(t, err, ErrKeyNotFound)
}
