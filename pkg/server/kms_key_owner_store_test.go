// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

package server

import (
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
)

// TestKMSKeyOwnerStore pins where each registered backend's XKMS ownership
// proofs live: on this node for backends whose key material is node-local, on
// the replicated store for backends every node shares.
func TestKMSKeyOwnerStore(t *testing.T) {
	local, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	replicated, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	for _, name := range []string{"software", "tpm2", "quantum"} {
		t.Run(name+" keeps the local store", func(t *testing.T) {
			choose := kmsKeyOwnerStores(local, func() (xkmssdk.StorageBackend, error) {
				t.Fatal("the replicated store was built for a node-local backend")
				return nil, nil
			})
			got, err := choose(name)
			require.NoError(t, err)
			require.Same(t, local, got)
		})
	}

	t.Run("shared backends use the replicated store, opened once", func(t *testing.T) {
		opens := 0
		choose := kmsKeyOwnerStores(local, func() (xkmssdk.StorageBackend, error) {
			opens++
			return replicated, nil
		})
		for _, name := range []string{"pkcs11", "awskms", "gcpkms", "azurekv", "vault"} {
			got, err := choose(name)
			require.NoError(t, err, name)
			require.Same(t, replicated, got, name)
		}
		require.Equal(t, 1, opens)
	})

	t.Run("a replicated store error is returned and not cached", func(t *testing.T) {
		boom := errors.New("shard unavailable")
		opens := 0
		choose := kmsKeyOwnerStores(local, func() (xkmssdk.StorageBackend, error) {
			opens++
			return nil, boom
		})
		got, err := choose("pkcs11")
		require.ErrorIs(t, err, boom)
		require.Nil(t, got)

		got, err = choose("pkcs11")
		require.ErrorIs(t, err, boom)
		require.Nil(t, got)
		require.Equal(t, 2, opens)
	})

	t.Run("a nil replicated store fails closed", func(t *testing.T) {
		choose := kmsKeyOwnerStores(local, func() (xkmssdk.StorageBackend, error) {
			return nil, nil
		})
		got, err := choose("pkcs11")
		require.Error(t, err)
		require.Nil(t, got)
	})

	t.Run("software after a shared backend keeps the local store", func(t *testing.T) {
		choose := kmsKeyOwnerStores(local, func() (xkmssdk.StorageBackend, error) {
			return replicated, nil
		})
		got, err := choose("pkcs11")
		require.NoError(t, err)
		require.Same(t, replicated, got)

		got, err = choose("software")
		require.NoError(t, err)
		require.Same(t, local, got)
	})
}
