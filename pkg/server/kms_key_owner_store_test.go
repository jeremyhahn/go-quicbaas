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

// TestKMSKeyOwnerStore pins where the XKMS ownership proofs live: on this node
// for backends whose key material is node-local, on the replicated store for
// backends every node shares.
func TestKMSKeyOwnerStore(t *testing.T) {
	local, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	replicated, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	for _, name := range []string{"software", "tpm2", "quantum"} {
		t.Run(name+" keeps the local store", func(t *testing.T) {
			got, err := kmsKeyOwnerStore(name, local, func() (xkmssdk.StorageBackend, error) {
				t.Fatal("the replicated store was built for a node-local backend")
				return nil, nil
			})
			require.NoError(t, err)
			require.Same(t, local, got)
		})
	}

	for _, name := range []string{"pkcs11", "awskms", "gcpkms", "azurekv", "vault"} {
		t.Run(name+" uses the replicated store", func(t *testing.T) {
			got, err := kmsKeyOwnerStore(name, local, func() (xkmssdk.StorageBackend, error) {
				return replicated, nil
			})
			require.NoError(t, err)
			require.Same(t, replicated, got)
		})
	}

	t.Run("a replicated store error is returned", func(t *testing.T) {
		boom := errors.New("shard unavailable")
		got, err := kmsKeyOwnerStore("pkcs11", local, func() (xkmssdk.StorageBackend, error) {
			return nil, boom
		})
		require.ErrorIs(t, err, boom)
		require.Nil(t, got)
	})
}
