// Copyright (c) 2025 Jeremy Hahn

package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// requireVerifiesUnder signs a token digest with signer and requires m to
// verify it under kid.
func requireVerifiesUnder(t *testing.T, m *iam.SigningKeyManager, kid string, signer crypto.Signer) {
	t.Helper()
	digest := sha256.Sum256([]byte("token signed under " + kid))
	sig, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	require.NoError(t, err)
	pub, err := m.PublicForKID(context.Background(), kid)
	require.NoError(t, err, kid)
	ecPub, ok := pub.(*ecdsa.PublicKey)
	require.True(t, ok)
	require.True(t, ecdsa.VerifyASN1(ecPub, digest[:], sig), kid)
	require.Len(t, m.JWKS().Key(kid), 1, kid)
}

// A node upgrades its signing key L from the default backend to R1 on the
// replicated store, rotates to R2 and restarts. The adopted key L is kept
// for verification beside the rotation overlap, so on the restarted node
// and on a peer sharing the store an R1 token verifies and L still
// verifies; for the global key and for a dedicated tenant key.
func TestOIDCSigningStore_AdoptedKeyOutlivesRotationAndRestart(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	for name, tenantID := range map[string]string{"global key": "", "dedicated tenant key": "tenant-dedicated"} {
		t.Run(name, func(t *testing.T) {
			initOIDCSigningDefault(t)
			legacy, err := iam.NewSigningKeyManagerFromXKMS(iam.OIDCSigningKeyCN, tenantID)
			require.NoError(t, err)
			lKid, lSigner, _ := legacy.Current()

			registerFreshOIDCSigningBackend(t)
			node := func(t *testing.T) *iam.SigningKeyManager {
				t.Helper()
				global, err := newOIDCSigningKeyManager(oidcSigningBackendName)
				require.NoError(t, err)
				if tenantID == "" {
					return global
				}
				resolver, err := iam.NewTenantSigningKeyResolver(global, dedicatedOIDCSettings{tenantID: true})
				require.NoError(t, err)
				m, err := resolver.ManagerFor(context.Background(), tenantID)
				require.NoError(t, err)
				return m
			}
			a, peer := node(t), node(t)
			r1Kid, r1Signer, _ := a.Current()
			require.NotEqual(t, lKid, r1Kid)

			require.NoError(t, a.Rotate())
			r2Kid, _, _ := a.Current()
			require.NotEqual(t, r1Kid, r2Kid)

			restarted := node(t)
			require.NoError(t, peer.Refresh())
			for side, m := range map[string]*iam.SigningKeyManager{"restarted": restarted, "peer": peer} {
				t.Run(side, func(t *testing.T) {
					kid, _, _ := m.Current()
					require.Equal(t, r2Kid, kid)
					requireVerifiesUnder(t, m, r1Kid, r1Signer)
					requireVerifiesUnder(t, m, lKid, lSigner)
					require.NoError(t, m.Refresh())
					requireVerifiesUnder(t, m, r1Kid, r1Signer)
					requireVerifiesUnder(t, m, lKid, lSigner)
				})
			}
		})
	}
}
