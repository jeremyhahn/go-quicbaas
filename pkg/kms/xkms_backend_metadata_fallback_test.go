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

	dbxkms "github.com/jeremyhahn/go-xkms/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// GetKeyMetadata on a cold cache probes a fixed table of kid formats. A key
// stored under any other format fell through to name-only lookups that the
// software backend never answers, so the key was reported missing (#2125).
// The fallback now finds the key by its name in the backend's key list.
func TestXKMSBackend_GetKeyMetadata_NameOnlyFallbackFindsTheKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	backend, err := NewXKMSBackend(&XKMSBackendConfig{BackendType: config.BackendTypeSoftware, Logger: &mockLogger{}})
	require.NoError(t, err)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		alg  xkmssdk.SymmetricAlgorithm
		want string
	}{
		{"aes192-key", xkmssdk.SymmetricAES192GCM, AlgAES192GCM},
		{"xchacha-key", xkmssdk.SymmetricXChaCha20Poly1305, AlgXChaCha20Poly1305},
	} {
		_, err := dbxkms.GenerateSymmetricKey(ctx, "software", &xkmssdk.KeyAttributes{
			CN:                 tc.name,
			StoreType:          xkmssdk.StoreType("software"),
			KeyType:            SymmetricKeyType,
			SymmetricAlgorithm: tc.alg,
		})
		require.NoError(t, err)

		meta, err := backend.GetKeyMetadata(ctx, tc.name)
		require.NoError(t, err, "%s: a key go-xkms holds must be found by name", tc.name)
		require.Equal(t, tc.want, meta.Algorithm, tc.name)
	}

	_, err = backend.GetKeyMetadata(ctx, "no-such-key")
	require.ErrorIs(t, err, ErrKeyNotFound)
}
