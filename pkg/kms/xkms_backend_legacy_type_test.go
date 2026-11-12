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

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestXKMSBackend_LegacyEncryptionTypedKeyStaysReachable seeds a symmetric
// key under the pre-#1768 encryption type and checks that a ciphertext made
// under it still decrypts, and that the key can be used, read and deleted.
func TestXKMSBackend_LegacyEncryptionTypedKeyStaysReachable(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	ctx := context.Background()
	newBackend := func() *XKMSBackend {
		b, err := NewXKMSBackend(&XKMSBackendConfig{
			BackendType: config.BackendTypeSoftware,
			Logger:      &mockLogger{},
		})
		require.NoError(t, err)
		return b
	}
	backend := newBackend()

	_, err := dbxkms.GenerateSymmetricKey(ctx, "software", &xkmssdk.KeyAttributes{
		CN:                 "legacy-kek",
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	payload := []byte("payload")
	encrypted, err := dbxkms.Encrypt(ctx, "software:encryption:aes256-gcm:legacy-kek", payload, &xkmssdk.EncryptOptions{})
	require.NoError(t, err)
	ct, err := backend.serializeEncryptedData(encrypted)
	require.NoError(t, err)

	got, err := backend.Decrypt(ctx, "legacy-kek", ct, nil)
	require.NoError(t, err)
	require.Equal(t, payload, got)

	meta, err := backend.GetKeyMetadata(ctx, "legacy-kek")
	require.NoError(t, err)
	require.Equal(t, "AES-256-GCM", meta.Algorithm)

	ct2, err := backend.Encrypt(ctx, "legacy-kek", payload, nil)
	require.NoError(t, err)
	got2, err := backend.Decrypt(ctx, "legacy-kek", ct2, nil)
	require.NoError(t, err)
	require.Equal(t, payload, got2)

	require.NoError(t, backend.DeleteKey(ctx, "legacy-kek"))

	_, err = newBackend().GetKeyMetadata(ctx, "legacy-kek")
	require.ErrorIs(t, err, ErrKeyNotFound)
}
