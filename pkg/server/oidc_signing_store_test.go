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

package server

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// bootOIDCSigningNode simulates one node's boot: a fresh process-global xkms
// with its own node-local (memory) default backend, the OIDC signing backend
// registered over kv, and the signing key manager built on it.
func bootOIDCSigningNode(t *testing.T, kv xkmssdk.StorageBackend) *iam.SigningKeyManager {
	t.Helper()
	dbxkms.Reset()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	local, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: xkmssdk.NewStorageMemory(),
	})
	require.NoError(t, err)
	def, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     local,
		CertStorage: xkmssdk.NewStorageMemory(),
	})
	require.NoError(t, err)
	require.NoError(t, dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: string(xkmssdk.StoreSoftware),
		Backends:       map[string]xkmssdk.ServiceBackend{string(xkmssdk.StoreSoftware): def},
		Logger:         logger.NewAdapter(log),
	}))

	backend, err := newOIDCSigningServiceBackend(kv)
	require.NoError(t, err)
	require.NoError(t, dbxkms.RegisterBackend(oidcSigningBackendName, backend))

	m, err := iam.NewSigningKeyManagerFromXKMSBackend(iam.OIDCSigningKeyCN, "", oidcSigningBackendName)
	require.NoError(t, err)
	return m
}

// TestOIDCSigningStore_PeerNodesShareOneKey proves a platform token signed on
// one node verifies on a peer: both nodes load the one key from the shared
// (replicated) store, so they agree on the kid. A peer on a different store
// gets a different kid, which shows the shared store is what makes it pass.
func TestOIDCSigningStore_PeerNodesShareOneKey(t *testing.T) {
	t.Cleanup(dbxkms.Reset)

	cases := []struct {
		name     string
		shared   bool
		sameKeys bool
	}{
		{name: "peer on the shared store", shared: true, sameKeys: true},
		{name: "peer on its own store", shared: false, sameKeys: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv := xkmssdk.NewStorageMemory()
			a := bootOIDCSigningNode(t, kv)
			kidA, signerA, _ := a.Current()

			kvB := kv
			if !tc.shared {
				kvB = xkmssdk.NewStorageMemory()
			}
			b := bootOIDCSigningNode(t, kvB)
			kidB, _, _ := b.Current()

			if !tc.sameKeys {
				require.NotEqual(t, kidA, kidB)
				require.Empty(t, b.JWKS().Key(kidA))
				return
			}
			require.Equal(t, kidA, kidB)
			require.Len(t, b.JWKS().Key(kidA), 1)

			digest := sha256.Sum256([]byte("platform-token-payload"))
			sig, err := signerA.Sign(rand.Reader, digest[:], crypto.SHA256)
			require.NoError(t, err)
			pub, err := b.PublicForKID(kidA)
			require.NoError(t, err)
			ecPub, ok := pub.(*ecdsa.PublicKey)
			require.True(t, ok)
			require.True(t, ecdsa.VerifyASN1(ecPub, digest[:], sig))
		})
	}
}
