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
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestInitializeFromXKMS_OwnershipStorePerBackend proves the factory asks the
// chooser for each registered backend's ownership store, so a node-local
// backend keeps its ownership records next to its own key material while a
// shared backend keeps them on the shared store.
func TestInitializeFromXKMS_OwnershipStorePerBackend(t *testing.T) {
	pkcs11 := string(config.BackendTypePKCS11)

	t.Run("each backend writes to its own store", func(t *testing.T) {
		ctx := context.Background()
		newTwoBackendXKMS(t)

		local, err := xkmssdk.NewMemoryBackend()
		require.NoError(t, err)
		shared, err := xkmssdk.NewMemoryBackend()
		require.NoError(t, err)

		asked := map[string]int{}
		factory, err := NewBackendFactory(&BackendFactoryConfig{
			Logger: &mockLogger{},
			KeyNameStoreFor: func(backendName string) (xkmssdk.StorageBackend, error) {
				asked[backendName]++
				if backendName == "software" {
					return local, nil
				}
				return shared, nil
			},
		})
		require.NoError(t, err)
		require.NoError(t, factory.InitializeFromXKMS(ctx, nil))
		require.Equal(t, map[string]int{"software": 1, pkcs11: 1}, asked)

		xkmsBackend := func(name string) *XKMSBackend {
			b, err := factory.Registry().Get(name)
			require.NoError(t, err)
			kb, ok := b.(*XKMSBackend)
			require.True(t, ok, "backend %q is %T", name, b)
			return kb
		}

		sw := xkmsBackend("software")
		swAttrs := sw.xkmsKeyAttributes("tenant/acme/k", AlgAES256GCM)
		_, err = sw.CreateKey(ctx, sw.algorithmToKeyAttributes("tenant/acme/k", AlgAES256GCM))
		require.NoError(t, err)
		swRecord := xkmsKeyOwnerRecord(sw.xkmsKeyIdentity(swAttrs))
		_, err = local.Get(ctx, swRecord)
		require.NoError(t, err, "software ownership record is on the local store")
		_, err = shared.Get(ctx, swRecord)
		require.ErrorIs(t, err, xkmssdk.ErrStorageNotFound, "software ownership record never reaches the shared store")

		hsm := xkmsBackend(pkcs11)
		hsmAttrs := hsm.xkmsKeyAttributes("tenant/acme/k", AlgAES256GCM)
		_, err = hsm.CreateKey(ctx, hsm.algorithmToKeyAttributes("tenant/acme/k", AlgAES256GCM))
		require.NoError(t, err)
		hsmRecord := xkmsKeyOwnerRecord(hsm.xkmsKeyIdentity(hsmAttrs))
		_, err = shared.Get(ctx, hsmRecord)
		require.NoError(t, err, "pkcs11 ownership record is on the shared store")
		_, err = local.Get(ctx, hsmRecord)
		require.ErrorIs(t, err, xkmssdk.ErrStorageNotFound, "pkcs11 ownership record never reaches the local store")

		_, err = local.Get(ctx, xkmsKeyNameMappingMarker)
		require.NoError(t, err, "the legacy-name guard marks the local store")
		_, err = shared.Get(ctx, xkmsKeyNameMappingMarker)
		require.NoError(t, err, "the legacy-name guard marks the shared store")
	})

	t.Run("a chooser error fails the start", func(t *testing.T) {
		ctx := context.Background()
		newTwoBackendXKMS(t)

		local, err := xkmssdk.NewMemoryBackend()
		require.NoError(t, err)
		errOpen := errors.New("replicated store unavailable")

		factory, err := NewBackendFactory(&BackendFactoryConfig{
			Logger: &mockLogger{},
			KeyNameStoreFor: func(backendName string) (xkmssdk.StorageBackend, error) {
				if backendName == pkcs11 {
					return nil, errOpen
				}
				return local, nil
			},
		})
		require.NoError(t, err)
		require.ErrorIs(t, factory.InitializeFromXKMS(ctx, nil), errOpen)

		_, err = local.Get(ctx, xkmsKeyNameMappingMarker)
		require.ErrorIs(t, err, xkmssdk.ErrStorageNotFound, "no marker is written when the start fails")
	})
}
